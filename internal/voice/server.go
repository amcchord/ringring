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
	"sync"
	"time"

	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/observability"
	"github.com/amcchord/ringring/internal/openairuntime"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/weather"
)

var safePartyID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type PartySource interface {
	PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error)
}

type ExtensionManager interface {
	ChangeMemberExtensionByDevice(context.Context, string, string, string) error
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
	Source            PartySource
	Extensions        ExtensionManager
	Reconcile         func(context.Context) error
	Cipher            SecretDecryptor
	Weather           WeatherSource
	Speech            SpeechSource
	AudioDir          string
	PlaybackDir       string
	Logger            *slog.Logger
	Metrics           *observability.Registry
	Now               func() time.Time
	CacheDuration     time.Duration
	AIModel           string
	AIRealtimeURL     string
	AICallMaxDuration time.Duration
	AIMaxConcurrent   int
	aiMu              sync.Mutex
	aiTickets         map[string]aiTicket
	aiActive          int
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
	_ = connection.SetDeadline(time.Now().Add(55 * time.Second))
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	environment, err := readEnvironment(reader)
	if err != nil {
		s.logger().Warn("read FastAGI environment", "error_class", observability.ErrorClass(err))
		return
	}
	switch environment["agi_network_script"] {
	case "weather":
		s.handleWeather(reader, writer, environment)
	case "ai-authorize":
		s.handleAIAuthorize(reader, writer, environment)
	case "choose-extension":
		s.handleChooseExtension(reader, writer, environment)
	default:
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
	}
	return
}

func (s *Server) handleChooseExtension(reader *bufio.Reader, writer *bufio.Writer, environment map[string]string) {
	result := "abandoned"
	defer func() { s.Metrics.ObserveVoice("extension", result) }()
	partyID := environment["agi_arg_1"]
	endpoint := environment["agi_arg_2"]
	if s.Extensions == nil || !safePartyID.MatchString(partyID) || !safePartyID.MatchString(endpoint) {
		result = "error"
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		extension, err := agiCommandResult(reader, writer, "GET DATA agent-newlocation 5000 5")
		if err != nil || extension == "-1" {
			return
		}
		if !validExtension(extension) {
			if err := agiCommand(reader, writer, "EXEC Playback invalid"); err != nil {
				return
			}
			continue
		}

		commands := []string{
			`STREAM FILE "you-entered" ""`,
			`SAY DIGITS ` + extension + ` ""`,
			`STREAM FILE "if-correct-press" ""`,
			`SAY DIGITS 1 ""`,
		}
		for _, command := range commands {
			if err := agiCommand(reader, writer, command); err != nil {
				return
			}
		}
		confirmation, err := agiCommandResult(reader, writer, "WAIT FOR DIGIT 5000")
		if err != nil || confirmation == "-1" {
			return
		}
		if confirmation != "49" { // AGI returns the ASCII value for DTMF 1.
			if err := agiCommand(reader, writer, "EXEC Playback please-try-again"); err != nil {
				return
			}
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		changeErr := s.Extensions.ChangeMemberExtensionByDevice(ctx, partyID, endpoint, extension)
		if errors.Is(changeErr, store.ErrExtensionTaken) || errors.Is(changeErr, store.ErrInvalidExtension) {
			cancel()
			if playbackErr := agiCommand(reader, writer, "EXEC Playback invalid"); playbackErr != nil {
				return
			}
			continue
		}
		if changeErr != nil {
			cancel()
			result = "error"
			s.logger().Warn("change extension from phone", "error_class", observability.ErrorClass(changeErr))
			_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
			return
		}
		if s.Reconcile != nil {
			if reconcileErr := s.Reconcile(ctx); reconcileErr != nil {
				s.logger().Warn("reconcile phones after extension change", "error_class", observability.ErrorClass(reconcileErr))
			}
		}
		cancel()
		result = "changed"

		for _, command := range []string{
			"EXEC Playback auth-thankyou",
			`STREAM FILE "vm-extension" ""`,
			`SAY DIGITS ` + extension + ` ""`,
		} {
			if err := agiCommand(reader, writer, command); err != nil {
				return
			}
		}
		return
	}
	_ = agiCommand(reader, writer, "EXEC Playback goodbye")
}

func validExtension(extension string) bool {
	if len(extension) < 2 || len(extension) > 5 {
		return false
	}
	for _, character := range extension {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func (s *Server) handleWeather(reader *bufio.Reader, writer *bufio.Writer, environment map[string]string) {
	result := "error"
	defer func() { s.Metrics.ObserveVoice("weather", result) }()
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
		s.logger().Warn("prepare weather line", "error_class", observability.ErrorClass(err))
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	if err := agiCommand(reader, writer, `STREAM FILE "`+path+`" ""`); err == nil {
		result = "ready"
	}
}

func (s *Server) weatherAudio(ctx context.Context, partyID string) (string, error) {
	if s.Source == nil || s.Cipher == nil || s.Weather == nil || s.Speech == nil || s.AudioDir == "" || s.PlaybackDir == "" {
		return "", errors.New("weather voice service is not fully configured")
	}
	party, services, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		return "", err
	}
	if !services.WeatherEnabled || services.WeatherLabel == "" || party.OpenAIStatus != "ready" || party.OpenAIUsagePausedForSpendLimit() || party.OpenAIKeyCiphertext == "" {
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
	result, err := agiCommandResult(reader, writer, command)
	if err != nil {
		return err
	}
	if result == "-1" {
		return errors.New("FastAGI channel is unavailable")
	}
	return nil
}

func agiCommandResult(reader *bufio.Reader, writer *bufio.Writer, command string) (string, error) {
	if command == "" || strings.ContainsAny(command, "\r\n") {
		return "", errors.New("invalid FastAGI command")
	}
	if _, err := writer.WriteString(command + "\n"); err != nil {
		return "", err
	}
	if err := writer.Flush(); err != nil {
		return "", err
	}
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	response = strings.TrimSpace(response)
	const prefix = "200 result="
	if !strings.HasPrefix(response, prefix) {
		return "", errors.New("FastAGI command failed")
	}
	result := strings.TrimPrefix(response, prefix)
	if separator := strings.IndexAny(result, " \t("); separator >= 0 {
		result = result[:separator]
	}
	return result, nil
}

func atomicWrite(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ringring-audio-*")
	if err != nil {
		return fmt.Errorf("create temporary voice audio: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		return fmt.Errorf("set voice audio permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write voice audio: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync voice audio: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close voice audio: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace voice audio: %w", err)
	}
	return nil
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
