package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/observability"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func TestPrepareAICallDisclosesAIAndIssuesOneUseTicket(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-runtime-key", []byte("pty_ai"))
	if err != nil {
		t.Fatal(err)
	}
	speech := &fakeSpeech{}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	metrics := observability.New()
	server := &Server{
		Source: fakePartySource{
			party: model.Party{ID: "pty_ai", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
			services: model.PartyServices{
				PartyID: "pty_ai", AIEnabled: true, UpdatedAt: now,
			},
		},
		Cipher: cipher, Speech: speech, AIAdultAccess: fakeAIAdultAccess(true), AudioDir: t.TempDir(), PlaybackDir: "/voice",
		Now: func() time.Time { return now }, AIMaxConcurrent: 1, Metrics: metrics,
		AIAdultOnlyEnabled: true,
	}
	callID := uuid.NewString()
	path, canonicalID, err := server.prepareAICall(t.Context(), "pty_ai", callID, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/voice/ai-adult-disclosure-pty_ai" || canonicalID != callID {
		t.Fatalf("unexpected AI authorization: path=%q call=%q", path, canonicalID)
	}
	if speech.key != "party-runtime-key" || speech.input != aiDisclosurePhrase || !strings.Contains(speech.input, "AI-generated voice") {
		t.Fatalf("AI disclosure used unexpected key or phrase: key=%q phrase=%q", speech.key, speech.input)
	}
	if _, _, err := server.prepareAICall(t.Context(), "pty_ai", uuid.NewString(), "234567"); err == nil {
		t.Fatal("concurrency reservation allowed a second AI call")
	}
	ticket, ok := server.claimAITicket(callID)
	if !ok || ticket.PartyID != "pty_ai" || !strings.HasPrefix(ticket.SafetyID, "rr_") || strings.Contains(ticket.SafetyID, "123456") {
		t.Fatalf("unexpected privacy-preserving ticket: %#v ok=%v", ticket, ok)
	}
	if _, ok := server.claimAITicket(callID); ok {
		t.Fatal("AI ticket could be claimed twice")
	}
	active := httptest.NewRecorder()
	metrics.Handler(nil).ServeHTTP(active, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(active.Body.String(), "ringring_ai_calls_active 1") {
		t.Fatalf("active AI bridge was not observed:\n%s", active.Body.String())
	}
	server.releaseAICall()
	released := httptest.NewRecorder()
	metrics.Handler(nil).ServeHTTP(released, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(released.Body.String(), "ringring_ai_calls_active 0") {
		t.Fatalf("released AI bridge remained active:\n%s", released.Body.String())
	}
}

func TestPrepareAICallGivesFriendlyDenialToNonAdultExtension(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-runtime-key", []byte("pty_ai"))
	if err != nil {
		t.Fatal(err)
	}
	speech := &fakeSpeech{}
	server := &Server{
		Source: fakePartySource{
			party:    model.Party{ID: "pty_ai", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
			services: model.PartyServices{PartyID: "pty_ai", AIEnabled: true},
		},
		AIAdultAccess: fakeAIAdultAccess(false), Cipher: cipher, Speech: speech,
		AudioDir: t.TempDir(), PlaybackDir: "/voice", AIAdultOnlyEnabled: true,
	}
	callID := uuid.NewString()
	path, canonicalID, err := server.prepareAICall(t.Context(), "pty_ai", callID, "rrd_legacy-device")
	if !errors.Is(err, errAIAdultAccess) {
		t.Fatalf("non-adult extension error = %v", err)
	}
	if path != "/voice/ai-adult-only-pty_ai" || canonicalID != "" || speech.input != aiAdultOnlyPhrase {
		t.Fatalf("unexpected friendly denial: path=%q call=%q phrase=%q", path, canonicalID, speech.input)
	}
	if _, ok := server.claimAITicket(callID); ok {
		t.Fatal("non-adult extension received an AI bridge ticket")
	}
}

func TestAIAdultOnlyGateStopsAuthorizationAndRealtime(t *testing.T) {
	server := &Server{}
	if _, _, err := server.prepareAICall(t.Context(), "pty_ai", uuid.NewString(), "123456"); err == nil || !strings.Contains(err.Error(), "adult-only gate") {
		t.Fatalf("closed gate allowed AI authorization: %v", err)
	}
	if _, _, _, err := server.partyAIKey(t.Context(), "pty_ai"); err == nil || !strings.Contains(err.Error(), "adult-only gate") {
		t.Fatalf("closed gate allowed party key access: %v", err)
	}
	appSide, phoneSide := net.Pipe()
	defer appSide.Close()
	defer phoneSide.Close()
	if err := server.bridgeRealtime(t.Context(), appSide, "party-key", "rr_safe_test"); err == nil || !strings.Contains(err.Error(), "adult-only gate") {
		t.Fatalf("closed gate allowed a Realtime bridge: %v", err)
	}
}

func TestSpendLimitReconciliationDoesNotUseCachedAIDisclosure(t *testing.T) {
	temporary := t.TempDir()
	if err := os.WriteFile(filepath.Join(temporary, "ai-disclosure-pty_ai.wav"), []byte("cached"), 0o640); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Source: fakePartySource{
			party: model.Party{
				ID: "pty_ai", OpenAIStatus: "ready", OpenAIKeyCiphertext: "unused",
				OpenAISpendLimitStatus: "update-error", OpenAISpendPendingCents: 725,
			},
			services: model.PartyServices{PartyID: "pty_ai", AIEnabled: true},
		},
		Cipher: &fakeDecryptor{}, Speech: &fakeSpeech{}, AIAdultAccess: fakeAIAdultAccess(true), AudioDir: temporary, PlaybackDir: "/voice",
		AIAdultOnlyEnabled: true,
	}
	if _, _, err := server.prepareAICall(t.Context(), "pty_ai", uuid.NewString(), "123456"); err == nil {
		t.Fatal("spend-limit reconciliation served a cached AI disclosure")
	}
}

type fakeDecryptor struct{}

func (*fakeDecryptor) Decrypt(string, []byte) (string, error) { return "party-key", nil }

func TestRealtimeBridgeTranscodesAudioAndUsesPrivacyControls(t *testing.T) {
	type observed struct {
		Authorization string
		SafetyID      string
		Session       map[string]any
		InputAudio    []byte
	}
	observedCh := make(chan observed, 1)
	serverErrors := make(chan error, 1)
	outputPCMU := []byte{encodeMuLaw(-1200), encodeMuLaw(0), encodeMuLaw(1200)}
	websocketServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer connection.CloseNow()
		ctx := context.Background()
		_, sessionPayload, err := connection.Read(ctx)
		if err != nil {
			serverErrors <- err
			return
		}
		var session map[string]any
		if err := json.Unmarshal(sessionPayload, &session); err != nil {
			serverErrors <- err
			return
		}
		if _, _, err := connection.Read(ctx); err != nil { // initial response.create
			serverErrors <- err
			return
		}
		_, inputPayload, err := connection.Read(ctx)
		if err != nil {
			serverErrors <- err
			return
		}
		var input struct {
			Audio string `json:"audio"`
		}
		if err := json.Unmarshal(inputPayload, &input); err != nil {
			serverErrors <- err
			return
		}
		inputAudio, err := base64.StdEncoding.DecodeString(input.Audio)
		if err != nil {
			serverErrors <- err
			return
		}
		observedCh <- observed{
			Authorization: r.Header.Get("Authorization"), SafetyID: r.Header.Get("OpenAI-Safety-Identifier"),
			Session: session, InputAudio: inputAudio,
		}
		for _, event := range []map[string]any{
			{"type": "response.created"},
			{"type": "response.output_audio_transcript.delta", "delta": "private words RingRing must ignore"},
			{"type": "response.output_audio.delta", "item_id": "item_1", "delta": base64.StdEncoding.EncodeToString(outputPCMU)},
		} {
			payload, _ := json.Marshal(event)
			if err := connection.Write(ctx, websocket.MessageText, payload); err != nil {
				serverErrors <- err
				return
			}
		}
		_, _, _ = connection.Read(ctx)
	}))
	defer websocketServer.Close()

	appSide, asteriskSide := net.Pipe()
	bridge := &Server{AIRealtimeURL: websocketServer.URL, AIModel: "gpt-realtime-2.1", AIAdultOnlyEnabled: true}
	bridgeResult := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() { bridgeResult <- bridge.bridgeRealtime(ctx, appSide, "party-key", "rr_safe_test") }()

	inputPCM := make([]byte, 6)
	for i, sample := range []int16{-800, 0, 800} {
		binary.LittleEndian.PutUint16(inputPCM[i*2:i*2+2], uint16(sample))
	}
	if err := writeAudioSocketFrame(asteriskSide, 0x10, inputPCM); err != nil {
		t.Fatal(err)
	}
	frameType, outputPCM, err := readAudioSocketFrame(asteriskSide)
	if err != nil {
		t.Fatal(err)
	}
	if frameType != 0x10 || !bytes.Equal(outputPCM, muLawToLinearPCM(outputPCMU)) {
		t.Fatalf("unexpected transcoded output: type=%x pcm=%v", frameType, outputPCM)
	}
	if err := writeAudioSocketFrame(asteriskSide, 0x00, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-bridgeResult; err != nil {
		t.Fatal(err)
	}
	observation := <-observedCh
	if observation.Authorization != "Bearer party-key" || observation.SafetyID != "rr_safe_test" {
		t.Fatalf("missing backend authentication/privacy headers: %#v", observation)
	}
	wantInput, err := linearPCMToMuLaw(inputPCM)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(observation.InputAudio, wantInput) {
		t.Fatalf("input was not transcoded to PCMU: got=%v want=%v", observation.InputAudio, wantInput)
	}
	sessionJSON, _ := json.Marshal(observation.Session)
	sessionText := string(sessionJSON)
	for _, required := range []string{`"audio/pcmu"`, `"tracing":null`, `"tools":[]`, "adult extension for a caller age 18 or older", `"max_output_tokens":256`} {
		if !strings.Contains(sessionText, required) {
			t.Fatalf("Realtime privacy/safety setting %q missing from %s", required, sessionText)
		}
	}
	if strings.Contains(sessionText, "input_audio_transcription") {
		t.Fatalf("input transcription was enabled: %s", sessionText)
	}
	select {
	case err := <-serverErrors:
		t.Fatal(err)
	default:
	}
}

func TestAudioSocketFramingAndMuLawRoundTrip(t *testing.T) {
	var framed bytes.Buffer
	payload := []byte{1, 2, 3, 4}
	if err := writeAudioSocketFrame(&framed, 0x10, payload); err != nil {
		t.Fatal(err)
	}
	frameType, got, err := readAudioSocketFrame(&framed)
	if err != nil {
		t.Fatal(err)
	}
	if frameType != 0x10 || !bytes.Equal(got, payload) {
		t.Fatalf("unexpected AudioSocket frame: type=%x payload=%v", frameType, got)
	}
	for _, sample := range []int16{-30000, -1000, 0, 1000, 30000} {
		decoded := decodeMuLaw(encodeMuLaw(sample))
		if sample == 0 && decoded != 0 {
			t.Fatalf("zero decoded as %d", decoded)
		}
		if sample < 0 && decoded >= 0 || sample > 0 && decoded <= 0 {
			t.Fatalf("sample %d changed sign to %d", sample, decoded)
		}
		difference := int(decoded) - int(sample)
		if difference < 0 {
			difference = -difference
		}
		if difference > 1200 {
			t.Fatalf("sample %d decoded too far away as %d", sample, decoded)
		}
	}
}

func TestRealtimeOutputIsPacedInTwentyMillisecondFrames(t *testing.T) {
	encoded := bytes.Repeat([]byte{encodeMuLaw(600)}, 320)
	var output bytes.Buffer
	var nextFrame time.Time
	started := time.Now()
	written, err := writePacedPCMU(t.Context(), &output, encoded, &nextFrame)
	if err != nil {
		t.Fatal(err)
	}
	if written != len(encoded) {
		t.Fatalf("wrote %d PCMU bytes, want %d", written, len(encoded))
	}
	if elapsed := time.Since(started); elapsed < 10*time.Millisecond {
		t.Fatalf("two audio frames were sent as a burst in %s", elapsed)
	}
	for frame := 0; frame < 2; frame++ {
		frameType, payload, err := readAudioSocketFrame(&output)
		if err != nil {
			t.Fatal(err)
		}
		if frameType != 0x10 || len(payload) != 320 {
			t.Fatalf("frame %d had type=%x bytes=%d", frame, frameType, len(payload))
		}
	}
}
