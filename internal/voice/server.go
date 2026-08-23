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

	extensionrules "github.com/amcchord/ringring/internal/extension"
	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/observability"
	"github.com/amcchord/ringring/internal/openairuntime"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/weather"
)

var (
	safePartyID                   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	zipCode                       = regexp.MustCompile(`^[0-9]{5}$`)
	errWeatherLocationNotResolved = errors.New("weather location was not resolved")
)

const (
	operatorReasonHelp               = "help"
	operatorReasonMisdial            = "misdial"
	operatorReasonPhoneUnavailable   = "phone-unavailable"
	operatorReasonServiceUnavailable = "service-unavailable"
	operatorCacheVersion             = "v3"
	weatherSetupCacheVersion         = "v1"
)

type PartySource interface {
	PartyVoiceSettings(context.Context, string) (model.Party, model.PartyServices, error)
}

type ExtensionManager interface {
	ChangeMemberExtensionByDevice(context.Context, string, string, string) error
}

type WeatherLocationManager interface {
	WeatherLocationForDevice(context.Context, string, string) (model.WeatherLocation, error)
	SetWeatherLocationByDevice(context.Context, string, string, store.WeatherLocationInput) (model.WeatherLocation, bool, error)
}

type AIAdultAccessSource interface {
	AIAdultAccessForDevice(context.Context, string, string) (bool, error)
}

type OperatorDisclosureStore interface {
	OperatorDisclosureForDevice(context.Context, string, string) (bool, error)
	MarkOperatorDisclosureForDevice(context.Context, string, string, time.Time) error
}

type SecretDecryptor interface {
	Decrypt(string, []byte) (string, error)
}

type WeatherSource interface {
	Geocode(context.Context, string) (weather.Location, error)
	Current(context.Context, float64, float64) (weather.Conditions, error)
}

type SpeechSource interface {
	SpeechPCM(context.Context, string, string) ([]byte, error)
}

type JoinMemberSource interface {
	PartyMemberForDevice(context.Context, string, string) (model.Member, error)
}

type ConferenceAnnouncer interface {
	AnnounceJoin(context.Context, string, string) error
}

type Server struct {
	Source             PartySource
	AIAdultAccess      AIAdultAccessSource
	OperatorDisclosure OperatorDisclosureStore
	Extensions         ExtensionManager
	WeatherLocations   WeatherLocationManager
	Reconcile          func(context.Context) error
	Cipher             SecretDecryptor
	Weather            WeatherSource
	Speech             SpeechSource
	JoinMembers        JoinMemberSource
	ConferenceAnnounce ConferenceAnnouncer
	AudioDir           string
	PlaybackDir        string
	Logger             *slog.Logger
	Metrics            *observability.Registry
	Now                func() time.Time
	CacheDuration      time.Duration
	JoinAudioTTL       time.Duration
	AIModel            string
	AIRealtimeURL      string
	AICallMaxDuration  time.Duration
	AIMaxConcurrent    int
	AIAdultOnlyEnabled bool
	aiMu               sync.Mutex
	aiTickets          map[string]aiTicket
	aiActive           int
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("FastAGI listener is required")
	}
	if err := s.removeJoinAnnouncementAudio(); err != nil {
		s.logger().Warn("clean stale party call join announcements", "error_class", observability.ErrorClass(err))
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
	_ = connection.SetDeadline(time.Now().Add(85 * time.Second))
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
	case "operator":
		s.handleOperator(reader, writer, environment)
	case "ai-authorize":
		s.handleAIAuthorize(reader, writer, environment)
	case "choose-extension":
		s.handleChooseExtension(reader, writer, environment)
	case "join-party":
		s.handleJoinParty(reader, writer, environment)
	default:
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
	}
	return
}

func (s *Server) handleOperator(reader *bufio.Reader, writer *bufio.Writer, environment map[string]string) {
	result := "error"
	defer func() { s.Metrics.ObserveVoice("operator", result) }()
	_ = agiCommand(reader, writer, "SET VARIABLE RINGRING_OPERATOR_READY 0")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	partyID := environment["agi_arg_1"]
	endpoint := environment["agi_arg_3"]
	discloseAI := true
	markDisclosure := false
	if s.OperatorDisclosure != nil && safePartyID.MatchString(partyID) && safePartyID.MatchString(endpoint) {
		disclosed, err := s.OperatorDisclosure.OperatorDisclosureForDevice(ctx, partyID, endpoint)
		if err != nil {
			s.logger().Warn("check RingRing operator disclosure", "error_class", observability.ErrorClass(err))
		} else {
			discloseAI = !disclosed
			markDisclosure = !disclosed
		}
	}
	path, err := s.operatorAudio(ctx, partyID, environment["agi_arg_2"], discloseAI)
	if err != nil {
		s.logger().Warn("prepare RingRing operator", "error_class", observability.ErrorClass(err))
		return
	}
	if err := agiCommand(reader, writer, `STREAM FILE "`+path+`" ""`); err != nil {
		return
	}
	if markDisclosure {
		now := time.Now()
		if s.Now != nil {
			now = s.Now()
		}
		if err := s.OperatorDisclosure.MarkOperatorDisclosureForDevice(ctx, partyID, endpoint, now); err != nil {
			s.logger().Warn("mark RingRing operator disclosure", "error_class", observability.ErrorClass(err))
		}
	}
	if err := agiCommand(reader, writer, "SET VARIABLE RINGRING_OPERATOR_READY 1"); err != nil {
		return
	}
	result = "ready"
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
		if !extensionrules.Valid(extension) {
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

func (s *Server) handleWeather(reader *bufio.Reader, writer *bufio.Writer, environment map[string]string) {
	result := "error"
	defer func() { s.Metrics.ObserveVoice("weather", result) }()
	partyID := environment["agi_arg_1"]
	endpoint := environment["agi_arg_2"]
	if s.Source == nil || !safePartyID.MatchString(partyID) || !safePartyID.MatchString(endpoint) {
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	_, services, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		s.logger().Warn("prepare weather line", "error_class", observability.ErrorClass(err))
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	if !services.WeatherEnabled || s.WeatherLocations == nil {
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	location, err := s.WeatherLocations.WeatherLocationForDevice(ctx, partyID, endpoint)
	if err != nil {
		s.logger().Warn("load member weather location", "error_class", observability.ErrorClass(err))
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	if location.Label == "" {
		if err := s.collectWeatherLocation(ctx, reader, writer, partyID, endpoint); err != nil {
			if errors.Is(err, errWeatherLocationNotResolved) || errors.Is(err, context.Canceled) {
				return
			}
			s.logger().Warn("collect weather location", "error_class", observability.ErrorClass(err))
			_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
			return
		}
		location, err = s.WeatherLocations.WeatherLocationForDevice(ctx, partyID, endpoint)
		if err != nil || location.Label == "" {
			_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
			return
		}
	}
	path, err := s.weatherAudio(ctx, partyID, location)
	if err != nil {
		s.logger().Warn("prepare weather line", "error_class", observability.ErrorClass(err))
		_ = agiCommand(reader, writer, "EXEC Playback ss-noservice")
		return
	}
	if err := agiCommand(reader, writer, `STREAM FILE "`+path+`" ""`); err == nil {
		result = "ready"
	}
}

func (s *Server) collectWeatherLocation(ctx context.Context, reader *bufio.Reader, writer *bufio.Writer, partyID, endpoint string) error {
	if s.WeatherLocations == nil || s.Weather == nil {
		return errors.New("weather location setup is not configured")
	}
	for attempt := 0; attempt < 3; attempt++ {
		prompt := "initial"
		if attempt > 0 {
			prompt = "retry"
		}
		path, err := s.weatherSetupAudio(ctx, partyID, prompt)
		if err != nil {
			return err
		}
		postalCode, err := agiCommandResult(reader, writer, `GET DATA "`+path+`" 12000 5`)
		if err != nil {
			return err
		}
		if postalCode == "-1" {
			return context.Canceled
		}
		if !zipCode.MatchString(postalCode) {
			continue
		}
		location, err := s.Weather.Geocode(ctx, postalCode)
		if err != nil {
			continue
		}
		now := time.Now()
		if s.Now != nil {
			now = s.Now()
		}
		_, _, err = s.WeatherLocations.SetWeatherLocationByDevice(ctx, partyID, endpoint, store.WeatherLocationInput{
			Query: location.Query, Label: location.Label, Latitude: location.Latitude, Longitude: location.Longitude, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		return nil
	}
	path, err := s.weatherSetupAudio(ctx, partyID, "failed")
	if err != nil {
		return err
	}
	if err := agiCommand(reader, writer, `STREAM FILE "`+path+`" ""`); err != nil {
		return err
	}
	return errWeatherLocationNotResolved
}

func (s *Server) operatorAudio(ctx context.Context, partyID, reason string, discloseAI bool) (string, error) {
	if s.Source == nil || s.Cipher == nil || s.Speech == nil || s.AudioDir == "" || s.PlaybackDir == "" {
		return "", errors.New("RingRing operator is not fully configured")
	}
	if !safePartyID.MatchString(partyID) {
		return "", errors.New("invalid operator party ID")
	}
	party, services, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		return "", err
	}
	if party.OpenAIStatus != "ready" || party.OpenAIUsagePausedForSpendLimit() || party.OpenAIKeyCiphertext == "" {
		return "", errors.New("party operator voice is unavailable")
	}
	promptName, phrase, err := operatorPrompt(reason, services, discloseAI)
	if err != nil {
		return "", err
	}
	disclosureVariant := "repeat"
	if discloseAI {
		disclosureVariant = "first"
	}
	filename := "operator-" + operatorCacheVersion + "-" + promptName + "-" + disclosureVariant + "-" + partyID + ".wav"
	localPath := filepath.Join(s.AudioDir, filename)
	if info, err := os.Stat(localPath); err == nil && info.Size() > 44 &&
		(services.UpdatedAt.IsZero() || !info.ModTime().Before(services.UpdatedAt)) {
		return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
	}

	apiKey, err := s.Cipher.Decrypt(party.OpenAIKeyCiphertext, []byte(party.ID))
	if err != nil {
		return "", fmt.Errorf("decrypt party OpenAI key for operator: %w", err)
	}
	pcm, err := s.Speech.SpeechPCM(ctx, apiKey, phrase)
	if err != nil {
		return "", err
	}
	wav, err := openairuntime.PCM24kToWAV8k(pcm)
	if err != nil {
		return "", fmt.Errorf("convert operator speech: %w", err)
	}
	if err := os.MkdirAll(s.AudioDir, 0o750); err != nil {
		return "", fmt.Errorf("create operator audio directory: %w", err)
	}
	if err := atomicWrite(localPath, wav); err != nil {
		return "", err
	}
	return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
}

func (s *Server) weatherSetupAudio(ctx context.Context, partyID, promptName string) (string, error) {
	if s.Source == nil || s.Cipher == nil || s.Speech == nil || s.AudioDir == "" || s.PlaybackDir == "" || !safePartyID.MatchString(partyID) {
		return "", errors.New("weather setup voice is not configured")
	}
	party, _, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		return "", err
	}
	if party.OpenAIStatus != "ready" || party.OpenAIUsagePausedForSpendLimit() || party.OpenAIKeyCiphertext == "" {
		return "", errors.New("party weather setup voice is unavailable")
	}
	phrase := ""
	switch promptName {
	case "initial":
		phrase = "Ring ring! RingRing doesn't know which local weather you want yet. This is an AI-generated voice. Enter your five digit U.S. ZIP code. RingRing will save the place for this extension. Your party host can change it or turn weather off later."
	case "retry":
		phrase = "Let's try that again. Enter five digits for a U.S. ZIP code."
	case "failed":
		phrase = "I couldn't find that ZIP code today. Please try star one two again, or ask your party host to enter a city, state, or postal code on RingRing."
	default:
		return "", errors.New("invalid weather setup prompt")
	}
	filename := "weather-setup-" + weatherSetupCacheVersion + "-" + promptName + "-" + partyID + ".wav"
	localPath := filepath.Join(s.AudioDir, filename)
	if info, err := os.Stat(localPath); err == nil && info.Size() > 44 {
		return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
	}
	apiKey, err := s.Cipher.Decrypt(party.OpenAIKeyCiphertext, []byte(party.ID))
	if err != nil {
		return "", fmt.Errorf("decrypt party OpenAI key for weather setup: %w", err)
	}
	pcm, err := s.Speech.SpeechPCM(ctx, apiKey, phrase)
	if err != nil {
		return "", err
	}
	wav, err := openairuntime.PCM24kToWAV8k(pcm)
	if err != nil {
		return "", fmt.Errorf("convert weather setup speech: %w", err)
	}
	if err := os.MkdirAll(s.AudioDir, 0o750); err != nil {
		return "", fmt.Errorf("create weather setup audio directory: %w", err)
	}
	if err := atomicWrite(localPath, wav); err != nil {
		return "", err
	}
	return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
}

func operatorPrompt(reason string, services model.PartyServices, discloseAI bool) (string, string, error) {
	disclosure := ""
	if discloseAI {
		disclosure = " I'm an AI-generated voice, not a person."
	}
	switch reason {
	case operatorReasonHelp:
		features := []string{
			"For a sound check, dial star one zero",
			"To choose a new extension, dial star one five",
		}
		if services.TimeEnabled {
			features = append(features, "For the time, dial star one one")
		}
		if services.WeatherEnabled {
			features = append(features, "For the weather, dial star one two")
		}
		if services.RadioEnabled {
			features = append(features, "For radio, dial star one three")
		}
		return reason, "Ring ring! Hi! I'm the RingRing operator." + disclosure + " Dial a family extension to ring someone. " +
			strings.Join(features, ". ") + ". To join a live party call, dial the star one six code shown in the RingRing phonebook. Dial zero whenever you need this tour. RingRing cannot call regular or emergency numbers, so keep another way to get help.", nil
	case operatorReasonMisdial:
		return reason, "Oops-a-daisy! RingRing operator here." + disclosure + " That number doesn't live in this party. Check the RingRing phonebook and try again, or dial zero for a quick tour. RingRing cannot call regular or emergency numbers.", nil
	case operatorReasonPhoneUnavailable:
		return reason, "Ring ring, operator here!" + disclosure + " That phone couldn't answer right now. Try again in a bit, or dial zero if you need help.", nil
	case operatorReasonServiceUnavailable:
		return reason, "Ring ring, operator here!" + disclosure + " That special line is taking a quick break. Try again soon, or dial zero if you need help.", nil
	default:
		return "", "", errors.New("invalid operator prompt")
	}
}

func (s *Server) weatherAudio(ctx context.Context, partyID string, location model.WeatherLocation) (string, error) {
	if s.Source == nil || s.Cipher == nil || s.Weather == nil || s.Speech == nil || s.AudioDir == "" || s.PlaybackDir == "" {
		return "", errors.New("weather voice service is not fully configured")
	}
	party, services, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		return "", err
	}
	if !services.WeatherEnabled || location.MemberID == "" || location.Label == "" || party.OpenAIStatus != "ready" || party.OpenAIUsagePausedForSpendLimit() || party.OpenAIKeyCiphertext == "" {
		return "", errors.New("party weather line is unavailable")
	}
	if !safePartyID.MatchString(location.MemberID) {
		return "", errors.New("invalid weather member ID")
	}
	filename := "weather-v2-" + location.MemberID + ".wav"
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
		(services.UpdatedAt.IsZero() || !info.ModTime().Before(services.UpdatedAt)) &&
		(location.UpdatedAt.IsZero() || !info.ModTime().Before(location.UpdatedAt)) {
		return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), nil
	}

	apiKey, err := s.Cipher.Decrypt(party.OpenAIKeyCiphertext, []byte(party.ID))
	if err != nil {
		return "", fmt.Errorf("decrypt party OpenAI key: %w", err)
	}
	conditions, err := s.Weather.Current(ctx, location.Latitude, location.Longitude)
	if err != nil {
		return "", err
	}
	phrase := weatherPhrase(location.Label, conditions)
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
