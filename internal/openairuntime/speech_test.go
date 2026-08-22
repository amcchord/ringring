package openairuntime

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpeechRequestAndPCMConversion(t *testing.T) {
	pcm := make([]byte, 12)
	for i, sample := range []int16{300, 600, 900, -300, -600, -900} {
		binary.LittleEndian.PutUint16(pcm[i*2:i*2+2], uint16(sample))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer party-key" {
			t.Fatal("party API key was not sent")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "gpt-4o-mini-tts" || body["response_format"] != "pcm" {
			t.Fatalf("unexpected speech request: %#v", body)
		}
		_, _ = w.Write(pcm)
	}))
	defer server.Close()
	client := New(server.Client())
	client.speechURL = server.URL
	gotPCM, err := client.SpeechPCM(t.Context(), "party-key", "A weather report")
	if err != nil {
		t.Fatal(err)
	}
	wav, err := PCM24kToWAV8k(gotPCM)
	if err != nil {
		t.Fatal(err)
	}
	if string(wav[:4]) != "RIFF" || binary.LittleEndian.Uint32(wav[24:28]) != 8000 || len(wav) != 48 {
		t.Fatalf("invalid WAV header: %q %d %d", wav[:4], binary.LittleEndian.Uint32(wav[24:28]), len(wav))
	}
	if sample := int16(binary.LittleEndian.Uint16(wav[44:46])); sample != 600 {
		t.Fatalf("first downsampled value = %d", sample)
	}
	if sample := int16(binary.LittleEndian.Uint16(wav[46:48])); sample != -600 {
		t.Fatalf("second downsampled value = %d", sample)
	}
}
