package voice

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/secure"
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

func TestDisabledWeatherDoesNotUseCachedAudio(t *testing.T) {
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
			party:    model.Party{ID: "pty_voice", OpenAIStatus: "ready", OpenAIKeyCiphertext: "unused"},
			services: model.PartyServices{PartyID: "pty_voice", WeatherEnabled: false},
		},
		Cipher: cipher, Weather: fakeWeather{}, Speech: &fakeSpeech{},
		AudioDir: temporary, PlaybackDir: "/voice",
	}
	if _, err := server.weatherAudio(t.Context(), "pty_voice"); err == nil {
		t.Fatal("disabled weather line served cached audio")
	}
}
