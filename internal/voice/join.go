package voice

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amcchord/ringring/internal/observability"
	"github.com/amcchord/ringring/internal/openairuntime"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/telephony"
)

const (
	defaultJoinAudioTTL = time.Minute
	joinAudioPrefix     = "join-v1-"
)

func (s *Server) handleJoinParty(reader *bufio.Reader, writer *bufio.Writer, environment map[string]string) {
	result := "error"
	defer func() { s.Metrics.ObserveVoice("conference_join", result) }()
	_ = agiCommand(reader, writer, "SET VARIABLE RINGRING_JOIN_READY 0")

	partyID := environment["agi_arg_1"]
	endpoint := environment["agi_arg_2"]
	conference := environment["agi_arg_3"]
	conferenceParty, _, conferenceOK := telephony.ParseConferenceName(conference)
	if s.JoinMembers == nil || s.ConferenceAnnounce == nil || !safePartyID.MatchString(partyID) ||
		!safePartyID.MatchString(endpoint) || !conferenceOK || conferenceParty != partyID {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	member, err := s.JoinMembers.PartyMemberForDevice(ctx, partyID, endpoint)
	if err != nil {
		s.logger().Warn("authorize party call join", "error_class", observability.ErrorClass(err))
		return
	}

	playback, audioPath, err := s.joinAnnouncementAudio(ctx, partyID, member.DisplayName)
	if err != nil {
		s.logger().Warn("prepare party call join announcement", "error_class", observability.ErrorClass(err))
		playback = "beep"
	}
	if audioPath != "" {
		ttl := s.JoinAudioTTL
		if ttl <= 0 {
			ttl = defaultJoinAudioTTL
		}
		time.AfterFunc(ttl, func() {
			if err := os.Remove(audioPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.logger().Warn("remove party call join announcement", "error_class", observability.ErrorClass(err))
			}
		})
	}
	if err := s.ConferenceAnnounce.AnnounceJoin(ctx, conference, playback); err != nil {
		s.logger().Warn("play party call join announcement", "error_class", observability.ErrorClass(err))
		return
	}
	if err := agiCommand(reader, writer, "SET VARIABLE RINGRING_JOIN_READY 1"); err != nil {
		return
	}
	result = "ready"
}

func (s *Server) joinAnnouncementAudio(ctx context.Context, partyID, displayName string) (string, string, error) {
	if s.Source == nil || s.Cipher == nil || s.Speech == nil || s.AudioDir == "" || s.PlaybackDir == "" {
		return "", "", errors.New("party call join voice is not configured")
	}
	if !safePartyID.MatchString(partyID) || strings.TrimSpace(displayName) == "" {
		return "", "", errors.New("party call join voice input is invalid")
	}
	party, _, err := s.Source.PartyVoiceSettings(ctx, partyID)
	if err != nil {
		return "", "", err
	}
	if party.ID != partyID || party.OpenAIStatus != "ready" || party.OpenAIUsagePausedForSpendLimit() || party.OpenAIKeyCiphertext == "" {
		return "", "", errors.New("party call join voice is unavailable")
	}
	apiKey, err := s.Cipher.Decrypt(party.OpenAIKeyCiphertext, []byte(party.ID))
	if err != nil {
		return "", "", fmt.Errorf("decrypt party OpenAI key for call join: %w", err)
	}
	pcm, err := s.Speech.SpeechPCM(ctx, apiKey, fmt.Sprintf("Ring ring! %s is joining the party.", strings.TrimSpace(displayName)))
	if err != nil {
		return "", "", err
	}
	wav, err := openairuntime.PCM24kToWAV8k(pcm)
	if err != nil {
		return "", "", fmt.Errorf("convert call join speech: %w", err)
	}
	token, err := secure.Token(18)
	if err != nil {
		return "", "", err
	}
	filename := joinAudioPrefix + token + ".wav"
	if err := os.MkdirAll(s.AudioDir, 0o750); err != nil {
		return "", "", fmt.Errorf("create call join audio directory: %w", err)
	}
	localPath := filepath.Join(s.AudioDir, filename)
	if err := atomicWrite(localPath, wav); err != nil {
		return "", "", err
	}
	return filepath.Join(s.PlaybackDir, strings.TrimSuffix(filename, ".wav")), localPath, nil
}

// removeJoinAnnouncementAudio clears name-bearing conference announcements
// left behind by a process interruption. Normal announcements are removed by
// their short TTL; this startup sweep closes the crash/restart gap without
// touching reusable, name-free voice assets.
func (s *Server) removeJoinAnnouncementAudio() error {
	if s.AudioDir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.AudioDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read party call announcement directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, joinAudioPrefix) || !strings.HasSuffix(name, ".wav") {
			continue
		}
		if err := os.Remove(filepath.Join(s.AudioDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale party call announcement: %w", err)
		}
	}
	return nil
}
