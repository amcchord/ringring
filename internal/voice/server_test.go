package voice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/weather"
)

type fakePartySource struct {
	party    model.Party
	services model.PartyServices
}

func (f fakePartySource) PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error) {
	return f.party, f.services, nil
}

type fakeWeather struct{ conditions weather.Conditions }

func (f fakeWeather) Current(context.Context, float64, float64) (weather.Conditions, error) {
	return f.conditions, nil
}

type fakeSpeech struct {
	key   string
	input string
}

type extensionChange struct {
	partyID   string
	endpoint  string
	extension string
}

type fakeExtensionManager struct {
	calls  []extensionChange
	errors map[string]error
}

func (f *fakeExtensionManager) ChangeMemberExtensionByDevice(_ context.Context, partyID, endpoint, extension string) error {
	f.calls = append(f.calls, extensionChange{partyID: partyID, endpoint: endpoint, extension: extension})
	return f.errors[extension]
}

func scriptedAGI(results ...string) *bufio.Reader {
	var responses strings.Builder
	for _, result := range results {
		fmt.Fprintf(&responses, "200 result=%s\n", result)
	}
	return bufio.NewReader(strings.NewReader(responses.String()))
}

func (f *fakeSpeech) SpeechPCM(_ context.Context, key, input string) ([]byte, error) {
	f.key = key
	f.input = input
	pcm := make([]byte, 600)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:i+2], uint16(int16(500)))
	}
	return pcm, nil
}

func TestWeatherAudioUsesDecryptedPartyKeyAndDisclosesAI(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-runtime-key", []byte("pty_voice"))
	if err != nil {
		t.Fatal(err)
	}
	speech := &fakeSpeech{}
	temporary := t.TempDir()
	server := &Server{
		Source: fakePartySource{
			party:    model.Party{ID: "pty_voice", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
			services: model.PartyServices{PartyID: "pty_voice", WeatherEnabled: true, WeatherLabel: "Portland, Maine", WeatherLatitude: 43.66, WeatherLongitude: -70.25},
		},
		Cipher:  cipher,
		Weather: fakeWeather{weather.Conditions{Temperature: 72, ApparentTemperature: 70, High: 77, Low: 59, PrecipitationChance: 20, WeatherCode: 2}},
		Speech:  speech, AudioDir: temporary, PlaybackDir: "/voice", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) },
	}
	path, err := server.weatherAudio(t.Context(), "pty_voice")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/voice/weather-pty_voice" || speech.key != "party-runtime-key" {
		t.Fatalf("unexpected output path or key: %q %q", path, speech.key)
	}
	if !strings.Contains(speech.input, "AI-generated voice") || !strings.Contains(speech.input, "Open-Meteo") {
		t.Fatalf("weather phrase lacked disclosures: %q", speech.input)
	}
}

func TestSpendLimitReconciliationDoesNotUseCachedWeatherAudio(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	if err := os.WriteFile(temporary+"/weather-pty_voice.wav", []byte("cached"), 0o640); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Source: fakePartySource{
			party: model.Party{
				ID: "pty_voice", OpenAIStatus: "ready", OpenAIKeyCiphertext: "unused",
				OpenAISpendLimitStatus: "updating", OpenAISpendPendingCents: 725,
			},
			services: model.PartyServices{PartyID: "pty_voice", WeatherEnabled: true, WeatherLabel: "Portland, Maine"},
		},
		Cipher: cipher, Weather: fakeWeather{}, Speech: &fakeSpeech{},
		AudioDir: temporary, PlaybackDir: "/voice",
	}
	if _, err := server.weatherAudio(t.Context(), "pty_voice"); err == nil {
		t.Fatal("spend-limit reconciliation served cached weather audio")
	}
}

func TestVoiceExtensionSelectionUsesAuthenticatedEndpointAndConfirmsDigits(t *testing.T) {
	manager := &fakeExtensionManager{errors: map[string]error{}}
	reconciles := 0
	server := &Server{
		Extensions: manager,
		Reconcile: func(context.Context) error {
			reconciles++
			return nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	reader := scriptedAGI("103", "0", "0", "0", "0", "49", "0", "0", "0")
	var commands bytes.Buffer
	server.handleChooseExtension(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_voice", "agi_arg_2": "rrd_authenticated",
	})

	if len(manager.calls) != 1 || manager.calls[0] != (extensionChange{partyID: "pty_voice", endpoint: "rrd_authenticated", extension: "103"}) {
		t.Fatalf("unexpected extension changes: %#v", manager.calls)
	}
	if reconciles != 1 {
		t.Fatalf("reconcile count = %d", reconciles)
	}
	want := strings.Join([]string{
		"GET DATA agent-newlocation 5000 5",
		`STREAM FILE "you-entered" ""`,
		`SAY DIGITS 103 ""`,
		`STREAM FILE "if-correct-press" ""`,
		`SAY DIGITS 1 ""`,
		"WAIT FOR DIGIT 5000",
		"EXEC Playback auth-thankyou",
		`STREAM FILE "vm-extension" ""`,
		`SAY DIGITS 103 ""`,
	}, "\n") + "\n"
	if commands.String() != want {
		t.Fatalf("unexpected FastAGI exchange:\n%s", commands.String())
	}
}

func TestVoiceExtensionSelectionRetriesInvalidTakenAndUnconfirmedNumbers(t *testing.T) {
	manager := &fakeExtensionManager{errors: map[string]error{"102": store.ErrExtensionTaken}}
	server := &Server{Extensions: manager, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	reader := scriptedAGI(
		"1", "0", // invalid one-digit entry and its error prompt
		"104", "0", "0", "0", "0", "50", "0", // DTMF 2 declines, then retry prompt
		"102", "0", "0", "0", "0", "49", "0", "0", // occupied number, its error prompt, then goodbye
	)
	var commands bytes.Buffer
	server.handleChooseExtension(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_voice", "agi_arg_2": "rrd_authenticated",
	})

	if len(manager.calls) != 1 || manager.calls[0].extension != "102" {
		t.Fatalf("unexpected extension changes: %#v", manager.calls)
	}
	output := commands.String()
	if strings.Count(output, "GET DATA agent-newlocation 5000 5\n") != 3 ||
		strings.Count(output, "EXEC Playback invalid\n") != 2 ||
		!strings.Contains(output, "EXEC Playback please-try-again\n") ||
		!strings.HasSuffix(output, "EXEC Playback goodbye\n") {
		t.Fatalf("unexpected retry exchange:\n%s", output)
	}
}

func TestVoiceExtensionSelectionRejectsUntrustedIdentityAndStoreFailure(t *testing.T) {
	manager := &fakeExtensionManager{errors: map[string]error{"105": store.ErrNotFound}}
	server := &Server{Extensions: manager, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	reader := scriptedAGI("0")
	var commands bytes.Buffer
	server.handleChooseExtension(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_voice", "agi_arg_2": "rrd_bad,endpoint",
	})
	if commands.String() != "EXEC Playback ss-noservice\n" || len(manager.calls) != 0 {
		t.Fatalf("unsafe endpoint reached extension manager: %q %#v", commands.String(), manager.calls)
	}

	reader = scriptedAGI("105", "0", "0", "0", "0", "49", "0")
	commands.Reset()
	server.handleChooseExtension(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_voice", "agi_arg_2": "rrd_missing",
	})
	if len(manager.calls) != 1 || !strings.HasSuffix(commands.String(), "EXEC Playback ss-noservice\n") {
		t.Fatalf("missing endpoint was not rejected generically: %q %#v", commands.String(), manager.calls)
	}
}

func TestVoiceExtensionSelectionKeepsSavedExtensionWhenReloadNeedsRepair(t *testing.T) {
	manager := &fakeExtensionManager{errors: map[string]error{}}
	server := &Server{
		Extensions: manager,
		Reconcile:  func(context.Context) error { return errors.New("private reload unavailable") },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	reader := scriptedAGI("106", "0", "0", "0", "0", "49", "0", "0", "0")
	var commands bytes.Buffer
	server.handleChooseExtension(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_voice", "agi_arg_2": "rrd_authenticated",
	})
	if len(manager.calls) != 1 || manager.calls[0].extension != "106" || !strings.Contains(commands.String(), "EXEC Playback auth-thankyou\n") {
		t.Fatalf("saved extension was not kept authoritative after reload failure: %q %#v", commands.String(), manager.calls)
	}
}
