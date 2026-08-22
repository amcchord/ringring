package voice

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/openairuntime"
	"github.com/amcchord/ringring/internal/weather"
)

var safePartyID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type PartySource interface {
	PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error)
}

type SecretDecryptor interface {
	Decrypt(string, []byte) (string, error)
}

type WeatherSource interface {
	Current(context.Context, float64, float64) (weather.Conditions, error)
}

type SpeechSource interface {
	SpeechPCM(context.Context, string, string) ([]byte, error)
}

type Server struct {
	Source        PartySource
	Cipher        SecretDecryptor
	Weather       WeatherSource
	Speech        SpeechSource
	AudioDir      string
	PlaybackDir   string
	Logger        *slog.Logger
	Now           func() time.Time
	CacheDuration time.Duration
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("FastAGI listener is required")
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept FastAGI connection: %w", err)
		}
		go s.handle(connection)
	}
}

func (s *Server) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(40 * time.Second))
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	environment, err := readEnvironment(reader)
	if err != nil {
		s.logger().Warn("read FastAGI environment", "error", err)
		return
	}
	if environment["agi_network_script"] != "weather" {
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	partyID := environment["agi_arg_1"]
	if !safePartyID.MatchString(partyID) {
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	_ = agiCommand(reader, writer, "EXEC Playback one-moment-please")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path, err := s.weatherAudio(ctx, partyID)
	if err != nil {
		s.logger().Warn("prepare weather line", "party_id", partyID, "error", err)
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	_ = agiCommand(reader, writer, `STREAM FILE "`+path+`" ""`)
}

func (s *Server) weatherAudio(ctx context.Context, partyID string) (string, error) {
	if s.Source == nil || s.Cipher == nil || s.Weather == nil || s.Speech == nil || s.AudioDir == "" || s.PlaybackDir == "" {
		return "", errors.New("weather voice service is not fully configured")
	}
	party, services, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		return "", err
	}
	if !services.WeatherEnabled || services.WeatherLabel == "" || party.OpenAIStatus != "ready" || party.OpenAIKeyCiphertext == "" {
		return "", errors.New("party weather line is unavailable")
	}
	filename := "weather-" + partyID + ".wav"
	localPath := filepath.Join(s.AudioDir, filename)
	cacheDuration := s.CacheDuration
	if cacheDuration == 0 {
		cacheDuration = 10 * time.Minute
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if info, err := os.Stat(localPath); err == nil &&
		now.Sub(info.ModTime()) >= 0 && now.Sub(info.ModTime()) < cacheDuration &&
		(services.UpdatedAt.IsZero() || !info.ModTime().Before(services.UpdatedAt)) {
		return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
	}

	apiKey, err := s.Cipher.Decrypt(party.OpenAIKeyCiphertext, []byte(party.ID))
	if err != nil {
		return "", fmt.Errorf("decrypt party OpenAI key: %w", err)
	}
	conditions, err := s.Weather.Current(ctx, services.WeatherLatitude, services.WeatherLongitude)
	if err != nil {
		return "", err
	}
	phrase := weatherPhrase(services.WeatherLabel, conditions)
	pcm, err := s.Speech.SpeechPCM(ctx, apiKey, phrase)
	if err != nil {
		return "", err
	}
	wav, err := openairuntime.PCM24kToWAV8k(pcm)
	if err != nil {
		return "", fmt.Errorf("convert weather speech: %w", err)
	}
	if err := os.MkdirAll(s.AudioDir, 0o750); err != nil {
		return "", fmt.Errorf("create weather audio directory: %w", err)
	}
	if err := atomicWrite(localPath, wav); err != nil {
		return "", err
	}
	return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
}

func weatherPhrase(label string, conditions weather.Conditions) string {
	precipitation := conditions.PrecipitationChance
	if precipitation < 0 {
		precipitation = 0
	}
	if precipitation > 100 {
		precipitation = 100
	}
	return fmt.Sprintf(
		"Hello. This weather report uses an AI-generated voice. In %s, it is %.0f degrees Fahrenheit with %s. It feels like %.0f degrees. Today's high is %.0f and the low is %.0f, with a %d percent chance of precipitation. Weather data is from Open-Meteo.",
		label, math.Round(conditions.Temperature), weather.Description(conditions.WeatherCode),
		math.Round(conditions.ApparentTemperature), math.Round(conditions.High), math.Round(conditions.Low), precipitation,
	)
}

func readEnvironment(reader *bufio.Reader) (map[string]string, error) {
	values := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return values, nil
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
}

func agiCommand(reader *bufio.Reader, writer *bufio.Writer, command string) error {
	if command == "" || strings.ContainsAny(command, "\r\n") {
		return errors.New("invalid FastAGI command")
	}
	if _, err := writer.WriteString(command + "\n"); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	response, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(response, "200 result=") {
		return fmt.Errorf("FastAGI command failed")
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ringring-audio-*")
	if err != nil {
		return fmt.Errorf("create temporary weather audio: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		return fmt.Errorf("set weather audio permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write weather audio: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync weather audio: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close weather audio: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace weather audio: %w", err)
	}
	return nil
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
