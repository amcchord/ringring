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
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/weather"
)

type fakePartySource struct {
	party    model.Party
	services model.PartyServices
}

type failingPartySource struct{ err error }

type fakeAIAdultAccess bool

func (f fakeAIAdultAccess) AIAdultAccessForDevice(context.Context, string, string) (bool, error) {
	return bool(f), nil
}

type fakeOperatorDisclosure struct {
	disclosed bool
	checks    int
	marks     int
	partyID   string
	endpoint  string
}

func (f *fakeOperatorDisclosure) OperatorDisclosureForDevice(_ context.Context, partyID, endpoint string) (bool, error) {
	f.checks++
	f.partyID = partyID
	f.endpoint = endpoint
	return f.disclosed, nil
}

func (f *fakeOperatorDisclosure) MarkOperatorDisclosureForDevice(_ context.Context, partyID, endpoint string, _ time.Time) error {
	f.marks++
	f.partyID = partyID
	f.endpoint = endpoint
	f.disclosed = true
	return nil
}

func (f failingPartySource) PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error) {
	return model.Party{}, model.PartyServices{}, f.err
}

func (f fakePartySource) PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error) {
	return f.party, f.services, nil
}

type fakeWeather struct{ conditions weather.Conditions }

func (f fakeWeather) Geocode(_ context.Context, query string) (weather.Location, error) {
	return weather.Location{Query: query, Label: "Cambridge, Massachusetts", Latitude: 42.37, Longitude: -71.11}, nil
}

func (f fakeWeather) Current(context.Context, float64, float64) (weather.Conditions, error) {
	return f.conditions, nil
}

type fakeSpeech struct {
	key    string
	input  string
	inputs []string
	calls  int
}

type failingSpeech struct{}

func (failingSpeech) SpeechPCM(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("speech unavailable")
}

type fakeJoinMembers struct {
	member   model.Member
	err      error
	partyID  string
	endpoint string
}

func (f *fakeJoinMembers) PartyMemberForDevice(_ context.Context, partyID, endpoint string) (model.Member, error) {
	f.partyID = partyID
	f.endpoint = endpoint
	return f.member, f.err
}

type fakeConferenceAnnouncer struct {
	conference string
	playback   string
	err        error
}

func (f *fakeConferenceAnnouncer) AnnounceJoin(_ context.Context, conference, playback string) error {
	f.conference = conference
	f.playback = playback
	return f.err
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
	f.inputs = append(f.inputs, input)
	f.calls++
	pcm := make([]byte, 600)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:i+2], uint16(int16(500)))
	}
	return pcm, nil
}

func TestJoinPartyUsesAuthenticatedMemberNameAndEphemeralPartyVoice(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-join-key", []byte("pty_join"))
	if err != nil {
		t.Fatal(err)
	}
	audioDir := t.TempDir()
	speech := &fakeSpeech{}
	members := &fakeJoinMembers{member: model.Member{PartyID: "pty_join", DisplayName: "Austin"}}
	announcer := &fakeConferenceAnnouncer{}
	server := &Server{
		Source: fakePartySource{party: model.Party{
			ID: "pty_join", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext,
		}},
		Cipher: cipher, Speech: speech, AudioDir: audioDir, PlaybackDir: "/voice",
		JoinMembers: members, ConferenceAnnounce: announcer, JoinAudioTTL: 15 * time.Millisecond,
	}
	commands := &bytes.Buffer{}
	server.handleJoinParty(scriptedAGI("0", "0"), bufio.NewWriter(commands), map[string]string{
		"agi_arg_1": "pty_join", "agi_arg_2": "123456", "agi_arg_3": "rrc-pty_join-102",
	})
	if commands.String() != "SET VARIABLE RINGRING_JOIN_READY 0\nSET VARIABLE RINGRING_JOIN_READY 1\n" {
		t.Fatalf("unexpected conference join FastAGI exchange:\n%s", commands.String())
	}
	if members.partyID != "pty_join" || members.endpoint != "123456" {
		t.Fatalf("join did not use the authenticated endpoint boundary: %#v", members)
	}
	if speech.key != "party-join-key" || speech.input != "Ring ring! Austin is joining the party." {
		t.Fatalf("unexpected party join voice request: key=%q input=%q", speech.key, speech.input)
	}
	if announcer.conference != "rrc-pty_join-102" || !strings.HasPrefix(announcer.playback, "/voice/join-v1-") {
		t.Fatalf("unexpected announcement target: %#v", announcer)
	}
	localPath := filepath.Join(audioDir, filepath.Base(announcer.playback)+".wav")
	if info, err := os.Stat(localPath); err != nil || info.Size() <= 44 {
		t.Fatalf("ephemeral announcement was not written: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(localPath); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("ephemeral announcement was not removed after its TTL")
}

func TestRemoveJoinAnnouncementAudioClearsOnlyEphemeralNameAudio(t *testing.T) {
	audioDir := t.TempDir()
	stale := filepath.Join(audioDir, "join-v1-interrupted.wav")
	shared := filepath.Join(audioDir, "voice-v2-operator.wav")
	for _, path := range []string{stale, shared} {
		if err := os.WriteFile(path, []byte("test audio"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&Server{AudioDir: audioDir}).removeJoinAnnouncementAudio(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale join announcement still exists: %v", err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("shared voice asset was removed: %v", err)
	}
}

func TestJoinPartyFallsBackToBeepWithoutSendingCallAudio(t *testing.T) {
	members := &fakeJoinMembers{member: model.Member{PartyID: "pty_join", DisplayName: "Austin"}}
	announcer := &fakeConferenceAnnouncer{}
	server := &Server{
		JoinMembers: members, ConferenceAnnounce: announcer,
		Source: fakePartySource{}, Speech: failingSpeech{}, AudioDir: t.TempDir(), PlaybackDir: "/voice",
	}
	commands := &bytes.Buffer{}
	server.handleJoinParty(scriptedAGI("0", "0"), bufio.NewWriter(commands), map[string]string{
		"agi_arg_1": "pty_join", "agi_arg_2": "123456", "agi_arg_3": "rrc-pty_join-102",
	})
	if announcer.playback != "beep" || !strings.Contains(commands.String(), "RINGRING_JOIN_READY 1") {
		t.Fatalf("voice outage did not preserve conference joining with a safe fallback: %#v\n%s", announcer, commands.String())
	}
}

func TestJoinPartyRejectsCrossPartyConferenceBeforeMemberLookup(t *testing.T) {
	members := &fakeJoinMembers{member: model.Member{DisplayName: "Austin"}}
	announcer := &fakeConferenceAnnouncer{}
	server := &Server{JoinMembers: members, ConferenceAnnounce: announcer}
	commands := &bytes.Buffer{}
	server.handleJoinParty(scriptedAGI("0"), bufio.NewWriter(commands), map[string]string{
		"agi_arg_1": "pty_join", "agi_arg_2": "123456", "agi_arg_3": "rrc-pty_other-102",
	})
	if members.partyID != "" || announcer.conference != "" || strings.Contains(commands.String(), "RINGRING_JOIN_READY 1") {
		t.Fatalf("cross-party conference reached the join path: %#v %#v\n%s", members, announcer, commands.String())
	}
}

type mutablePartySource struct {
	party    model.Party
	services model.PartyServices
}

func (f *mutablePartySource) PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error) {
	return f.party, f.services, nil
}

type weatherLocationCall struct {
	partyID  string
	endpoint string
	input    store.WeatherLocationInput
}

type fakeWeatherLocationManager struct {
	source *mutablePartySource
	calls  []weatherLocationCall
	err    error
}

func (f *fakeWeatherLocationManager) SetWeatherLocationByDevice(_ context.Context, partyID, endpoint string, input store.WeatherLocationInput) (model.PartyServices, bool, error) {
	f.calls = append(f.calls, weatherLocationCall{partyID: partyID, endpoint: endpoint, input: input})
	if f.err != nil {
		return model.PartyServices{}, false, f.err
	}
	f.source.services.WeatherEnabled = true
	f.source.services.WeatherQuery = input.Query
	f.source.services.WeatherLabel = input.Label
	f.source.services.WeatherLatitude = input.Latitude
	f.source.services.WeatherLongitude = input.Longitude
	f.source.services.UpdatedAt = input.UpdatedAt
	return f.source.services, true, nil
}

func TestOperatorAudioUsesPartyKeyDisclosesAIAndCaches(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-operator-key", []byte("pty_operator"))
	if err != nil {
		t.Fatal(err)
	}
	speech := &fakeSpeech{}
	server := &Server{
		Source: fakePartySource{
			party: model.Party{ID: "pty_operator", Name: "Private Family Name", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
			services: model.PartyServices{
				PartyID: "pty_operator", TimeEnabled: true, WeatherEnabled: true, RadioEnabled: true, AIEnabled: true,
			},
		},
		Cipher: cipher, Speech: speech, AudioDir: t.TempDir(), PlaybackDir: "/voice",
	}

	path, err := server.operatorAudio(t.Context(), "pty_operator", operatorReasonHelp, true)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/voice/operator-v3-help-first-pty_operator" || speech.key != "party-operator-key" || speech.calls != 1 {
		t.Fatalf("unexpected operator output: path=%q key=%q calls=%d", path, speech.key, speech.calls)
	}
	for _, phrase := range []string{"RingRing operator", "AI-generated voice", "not a person", "star one zero", "star one one", "star one two", "star one three", "star one five", "regular or emergency"} {
		if !strings.Contains(speech.input, phrase) {
			t.Fatalf("operator tour omitted %q: %q", phrase, speech.input)
		}
	}
	for _, privateOrAdultOnly := range []string{"Private Family Name", "star one four"} {
		if strings.Contains(speech.input, privateOrAdultOnly) {
			t.Fatalf("operator tour exposed or advertised %q: %q", privateOrAdultOnly, speech.input)
		}
	}

	if _, err := server.operatorAudio(t.Context(), "pty_operator", operatorReasonHelp, true); err != nil {
		t.Fatal(err)
	}
	if speech.calls != 1 {
		t.Fatalf("operator audio was regenerated instead of using its cache: %d calls", speech.calls)
	}
}

func TestOperatorPromptMentionsOnlyEnabledFamilySafeLines(t *testing.T) {
	_, prompt, err := operatorPrompt(operatorReasonHelp, model.PartyServices{}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "AI-generated") || strings.Contains(prompt, "not a person") {
		t.Fatalf("repeat operator prompt retained its AI disclosure: %q", prompt)
	}
	for _, disabled := range []string{"star one one", "star one two", "star one three", "star one four"} {
		if strings.Contains(prompt, disabled) {
			t.Fatalf("disabled or adult-only line %q appeared in operator tour: %q", disabled, prompt)
		}
	}
	_, firstPrompt, err := operatorPrompt(operatorReasonHelp, model.PartyServices{}, true, false)
	if err != nil || !strings.Contains(firstPrompt, "AI-generated voice, not a person") {
		t.Fatalf("first operator prompt lacked its disclosure: %q, %v", firstPrompt, err)
	}
	_, setupPrompt, err := operatorPrompt(operatorReasonHelp, model.PartyServices{WeatherSetupAllowed: true}, false, true)
	if err != nil || !strings.Contains(setupPrompt, "set up local weather") || !strings.Contains(setupPrompt, "five digit ZIP code") {
		t.Fatalf("operator omitted weather setup route: %q, %v", setupPrompt, err)
	}
	if _, _, err := operatorPrompt("caller-supplied-value", model.PartyServices{}, true, false); err == nil {
		t.Fatal("unrecognized operator reason was accepted")
	}
}

func TestOperatorFastAGISetsReadyOnlyAfterPlayback(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-operator-key", []byte("pty_operator"))
	if err != nil {
		t.Fatal(err)
	}
	disclosures := &fakeOperatorDisclosure{}
	server := &Server{
		Source: fakePartySource{
			party:    model.Party{ID: "pty_operator", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
			services: model.PartyServices{PartyID: "pty_operator"},
		},
		OperatorDisclosure: disclosures,
		Cipher:             cipher, Speech: &fakeSpeech{}, AudioDir: t.TempDir(), PlaybackDir: "/voice",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: observability.New(),
	}
	reader := scriptedAGI("0", "0", "0")
	var commands bytes.Buffer
	server.handleOperator(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_operator", "agi_arg_2": operatorReasonMisdial, "agi_arg_3": "123456",
	})
	want := "SET VARIABLE RINGRING_OPERATOR_READY 0\n" +
		`STREAM FILE "/voice/operator-v3-misdial-first-pty_operator" ""` + "\n" +
		"SET VARIABLE RINGRING_OPERATOR_READY 1\n"
	if commands.String() != want {
		t.Fatalf("unexpected operator FastAGI exchange:\n%s", commands.String())
	}
	if disclosures.checks != 1 || disclosures.marks != 1 || disclosures.partyID != "pty_operator" || disclosures.endpoint != "123456" {
		t.Fatalf("unexpected first disclosure state: %#v", disclosures)
	}

	reader = scriptedAGI("0", "0", "0")
	commands.Reset()
	server.handleOperator(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_operator", "agi_arg_2": operatorReasonMisdial, "agi_arg_3": "123456",
	})
	want = "SET VARIABLE RINGRING_OPERATOR_READY 0\n" +
		`STREAM FILE "/voice/operator-v3-misdial-repeat-pty_operator" ""` + "\n" +
		"SET VARIABLE RINGRING_OPERATOR_READY 1\n"
	if commands.String() != want || disclosures.checks != 2 || disclosures.marks != 1 {
		t.Fatalf("repeat operator exchange or disclosure state was wrong:\n%s\n%#v", commands.String(), disclosures)
	}
}

func TestOperatorDoesNotMarkDisclosureWhenPlaybackFails(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-operator-key", []byte("pty_operator"))
	if err != nil {
		t.Fatal(err)
	}
	disclosures := &fakeOperatorDisclosure{}
	server := &Server{
		Source: fakePartySource{
			party:    model.Party{ID: "pty_operator", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
			services: model.PartyServices{PartyID: "pty_operator"},
		},
		OperatorDisclosure: disclosures,
		Cipher:             cipher,
		Speech:             &fakeSpeech{},
		AudioDir:           t.TempDir(),
		PlaybackDir:        "/voice",
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics:            observability.New(),
	}
	reader := scriptedAGI("0", "-1")
	var commands bytes.Buffer
	server.handleOperator(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_operator", "agi_arg_2": operatorReasonHelp, "agi_arg_3": "123456",
	})
	if disclosures.checks != 1 || disclosures.marks != 0 || disclosures.disclosed {
		t.Fatalf("failed playback changed disclosure state: %#v", disclosures)
	}
	if strings.Contains(commands.String(), "SET VARIABLE RINGRING_OPERATOR_READY 1") {
		t.Fatalf("failed playback marked the operator ready:\n%s", commands.String())
	}
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
		Weather: fakeWeather{conditions: weather.Conditions{Temperature: 72, ApparentTemperature: 70, High: 77, Low: 59, PrecipitationChance: 20, WeatherCode: 2}},
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

func TestWeatherFastAGICollectsAndSavesFirstZIPThenReadsForecast(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-weather-key", []byte("pty_weather"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)
	source := &mutablePartySource{
		party:    model.Party{ID: "pty_weather", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext},
		services: model.PartyServices{PartyID: "pty_weather", TimeEnabled: true},
	}
	locations := &fakeWeatherLocationManager{source: source}
	speech := &fakeSpeech{}
	reconciles := 0
	server := &Server{
		Source: source, WeatherLocations: locations, Reconcile: func(context.Context) error { reconciles++; return nil },
		Cipher: cipher, Weather: fakeWeather{conditions: weather.Conditions{
			Temperature: 75, ApparentTemperature: 76, High: 81, Low: 64, PrecipitationChance: 30, WeatherCode: 1,
		}}, Speech: speech, AudioDir: t.TempDir(), PlaybackDir: "/voice",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: observability.New(), Now: func() time.Time { return now },
	}
	reader := scriptedAGI("02138", "0")
	var commands bytes.Buffer
	server.handleWeather(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_weather", "agi_arg_2": "123456",
	})
	want := "GET DATA \"/voice/weather-setup-v1-initial-pty_weather\" 12000 5\n" +
		"STREAM FILE \"/voice/weather-pty_weather\" \"\"\n"
	if commands.String() != want {
		t.Fatalf("unexpected weather setup exchange:\n%s", commands.String())
	}
	if len(locations.calls) != 1 || locations.calls[0].partyID != "pty_weather" || locations.calls[0].endpoint != "123456" || locations.calls[0].input.Query != "02138" {
		t.Fatalf("unexpected saved weather location: %#v", locations.calls)
	}
	if reconciles != 1 || !source.services.WeatherEnabled || source.services.WeatherLabel != "Cambridge, Massachusetts" {
		t.Fatalf("weather location was not enabled and reconciled: reconciles=%d services=%#v", reconciles, source.services)
	}
	if len(speech.inputs) != 2 || !strings.Contains(speech.inputs[0], "AI-generated voice") || !strings.Contains(speech.inputs[0], "five digit U.S. ZIP code") ||
		!strings.Contains(speech.inputs[1], "Cambridge, Massachusetts") || !strings.Contains(speech.inputs[1], "Open-Meteo") {
		t.Fatalf("unexpected weather setup and forecast prompts: %#v", speech.inputs)
	}
}

func TestWeatherFastAGIRetriesInvalidZIPWithoutRecordingSpeech(t *testing.T) {
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt("party-weather-key", []byte("pty_weather"))
	if err != nil {
		t.Fatal(err)
	}
	source := &mutablePartySource{
		party: model.Party{ID: "pty_weather", OpenAIStatus: "ready", OpenAIKeyCiphertext: ciphertext}, services: model.PartyServices{PartyID: "pty_weather"},
	}
	locations := &fakeWeatherLocationManager{source: source}
	server := &Server{
		Source: source, WeatherLocations: locations, Cipher: cipher,
		Weather: fakeWeather{conditions: weather.Conditions{}}, Speech: &fakeSpeech{}, AudioDir: t.TempDir(), PlaybackDir: "/voice",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: observability.New(), Now: time.Now,
	}
	reader := scriptedAGI("12", "02138", "0")
	var commands bytes.Buffer
	server.handleWeather(reader, bufio.NewWriter(&commands), map[string]string{"agi_arg_1": "pty_weather", "agi_arg_2": "123456"})
	output := commands.String()
	if strings.Count(output, "GET DATA ") != 2 || !strings.Contains(output, "weather-setup-v1-initial") || !strings.Contains(output, "weather-setup-v1-retry") || len(locations.calls) != 1 {
		t.Fatalf("invalid ZIP did not retry safely:\n%s\n%#v", output, locations.calls)
	}
	for _, forbidden := range []string{"RECORD FILE", "speech-to-text", "transcript"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("weather setup recorded or transcribed caller input %q:\n%s", forbidden, output)
		}
	}
}

func TestVoiceObservabilityKeepsRecordValuesOutOfLogsAndMetrics(t *testing.T) {
	metrics := observability.New()
	var logs bytes.Buffer
	server := &Server{
		Source: failingPartySource{err: errors.New("private-party-value from provider")},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)), Metrics: metrics,
	}
	reader := scriptedAGI("0", "0")
	var commands bytes.Buffer
	server.handleWeather(reader, bufio.NewWriter(&commands), map[string]string{"agi_arg_1": "private-party-value", "agi_arg_2": "123456"})

	response := httptest.NewRecorder()
	metrics.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	combined := logs.String() + response.Body.String()
	if strings.Contains(combined, "private-party-value") {
		t.Fatalf("voice observability exposed a party value:\n%s", combined)
	}
	if !strings.Contains(logs.String(), "error_class=internal") ||
		!strings.Contains(response.Body.String(), "ringring_voice_service_requests_total{service=\"weather\",result=\"error\"} 1") {
		t.Fatalf("voice failure was not safely observable:\n%s", combined)
	}
}

func TestOperatorFailureKeepsPartyAndReasonOutOfObservability(t *testing.T) {
	metrics := observability.New()
	var logs bytes.Buffer
	server := &Server{
		Source:  failingPartySource{err: errors.New("private-party-value from provider")},
		Logger:  slog.New(slog.NewTextHandler(&logs, nil)),
		Metrics: metrics,
	}
	reader := scriptedAGI("0")
	var commands bytes.Buffer
	server.handleOperator(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "private-party-value", "agi_arg_2": "private-reason-value",
	})
	response := httptest.NewRecorder()
	metrics.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	combined := logs.String() + response.Body.String()
	for _, privateValue := range []string{"private-party-value", "private-reason-value"} {
		if strings.Contains(combined, privateValue) {
			t.Fatalf("operator observability exposed %q:\n%s", privateValue, combined)
		}
	}
	if !strings.Contains(logs.String(), "error_class=internal") ||
		!strings.Contains(response.Body.String(), "ringring_voice_service_requests_total{service=\"operator\",result=\"error\"} 1") {
		t.Fatalf("operator failure was not safely observable:\n%s", combined)
	}
}

func TestVoiceExtensionSelectionUsesAuthenticatedEndpointAndConfirmsDigits(t *testing.T) {
	manager := &fakeExtensionManager{errors: map[string]error{}}
	reconciles := 0
	metrics := observability.New()
	server := &Server{
		Extensions: manager,
		Reconcile: func(context.Context) error {
			reconciles++
			return nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: metrics,
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
	response := httptest.NewRecorder()
	metrics.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(response.Body.String(), "ringring_voice_service_requests_total{service=\"extension\",result=\"changed\"} 1") {
		t.Fatalf("successful extension change was not observed:\n%s", response.Body.String())
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

func TestVoiceExtensionSelectionRejectsReservedNumberBeforeConfirmation(t *testing.T) {
	manager := &fakeExtensionManager{errors: map[string]error{}}
	server := &Server{Extensions: manager, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	reader := scriptedAGI("911", "0", "-1")
	var commands bytes.Buffer
	server.handleChooseExtension(reader, bufio.NewWriter(&commands), map[string]string{
		"agi_arg_1": "pty_voice", "agi_arg_2": "rrd_authenticated",
	})
	if len(manager.calls) != 0 || !strings.Contains(commands.String(), "EXEC Playback invalid\n") || strings.Contains(commands.String(), "SAY DIGITS 911") {
		t.Fatalf("reserved extension reached confirmation or storage: %q %#v", commands.String(), manager.calls)
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
