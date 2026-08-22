package openairuntime

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultSpeechURL = "https://api.openai.com/v1/audio/speech"

type Client struct {
	httpClient *http.Client
	speechURL  string
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	return &Client{httpClient: httpClient, speechURL: defaultSpeechURL}
}

func (c *Client) SpeechPCM(ctx context.Context, apiKey, input string) ([]byte, error) {
	if apiKey == "" {
		return nil, errors.New("party OpenAI key is not configured")
	}
	if len(input) == 0 || len(input) > 1200 {
		return nil, errors.New("speech input must be 1 to 1200 bytes")
	}
	body, err := json.Marshal(map[string]any{
		"model": "gpt-4o-mini-tts", "voice": "coral", "input": input,
		"instructions":    "Speak warmly, clearly, and briskly for a cheerful family phone service.",
		"response_format": "pcm",
	})
	if err != nil {
		return nil, fmt.Errorf("encode speech request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.speechURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create speech request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "ringring/0.1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("generate speech: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("OpenAI speech API returned %s: %s", response.Status, safeAPIError(limited))
	}
	pcm, err := io.ReadAll(io.LimitReader(response.Body, 12<<20))
	if err != nil {
		return nil, fmt.Errorf("read speech: %w", err)
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 || len(pcm) >= 12<<20 {
		return nil, errors.New("OpenAI speech API returned invalid PCM audio")
	}
	return pcm, nil
}

func PCM24kToWAV8k(pcm []byte) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%6 != 0 {
		return nil, errors.New("24 kHz PCM must contain complete three-sample frames")
	}
	sampleCount := len(pcm) / 6
	dataSize := sampleCount * 2
	wav := make([]byte, 44+dataSize)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 8000)
	binary.LittleEndian.PutUint32(wav[28:32], 16000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(dataSize))
	for i := 0; i < sampleCount; i++ {
		offset := i * 6
		a := int32(int16(binary.LittleEndian.Uint16(pcm[offset : offset+2])))
		b := int32(int16(binary.LittleEndian.Uint16(pcm[offset+2 : offset+4])))
		c := int32(int16(binary.LittleEndian.Uint16(pcm[offset+4 : offset+6])))
		binary.LittleEndian.PutUint16(wav[44+i*2:46+i*2], uint16(int16((a+b+c)/3)))
	}
	return wav, nil
}

func safeAPIError(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		message := strings.ReplaceAll(strings.ReplaceAll(payload.Error.Message, "\r", " "), "\n", " ")
		if payload.Error.Type != "" {
			return payload.Error.Type + ": " + message
		}
		return message
	}
	return "request failed"
}
