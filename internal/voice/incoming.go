package voice

import (
	"bufio"
	"context"
	"regexp"
	"time"

	extensionrules "github.com/amcchord/ringring/internal/extension"
	"github.com/amcchord/ringring/internal/observability"
)

var callIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *Server) handleIncomingCallPush(reader *bufio.Reader, writer *bufio.Writer, environment map[string]string) {
	result := "unavailable"
	if s.Metrics != nil {
		defer func() { s.Metrics.ObserveVoice("incoming_push", result) }()
	}
	_ = agiCommand(reader, writer, "SET VARIABLE RINGRING_PUSH_SENT 0")
	partyID := environment["agi_arg_1"]
	callerEndpoint := environment["agi_arg_2"]
	targetExtension := environment["agi_arg_3"]
	callID := environment["agi_arg_4"]
	if s.PhonePushes == nil || s.PushNotifier == nil || s.Cipher == nil ||
		!safePartyID.MatchString(partyID) || !safePartyID.MatchString(callerEndpoint) ||
		!extensionrules.Valid(targetExtension) || !callIDPattern.MatchString(callID) ||
		(s.PushEnvironment != "production" && s.PushEnvironment != "development") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.PhonePushes.PartyMemberForDevice(ctx, partyID, callerEndpoint); err != nil {
		s.logger().Warn("authorize incoming phone push", "error_class", observability.ErrorClass(err))
		result = "denied"
		return
	}
	registrations, err := s.PhonePushes.PhonePushRegistrationsForExtension(ctx, partyID, targetExtension)
	if err != nil {
		s.logger().Warn("load incoming phone push registrations", "error_class", observability.ErrorClass(err))
		result = "error"
		return
	}
	sent := false
	for _, registration := range registrations {
		if registration.Environment != s.PushEnvironment {
			continue
		}
		token, err := s.Cipher.Decrypt(registration.TokenCiphertext, []byte("phone-push:"+registration.DeviceID))
		if err != nil {
			s.logger().Warn("decrypt incoming phone push token", "error_class", observability.ErrorClass(err))
			continue
		}
		response, err := s.PushNotifier.SendVoIP(ctx, token, callID)
		if err != nil {
			s.logger().Warn("send incoming phone push", "error_class", observability.ErrorClass(err))
			continue
		}
		if response.Unregistered {
			if err := s.PhonePushes.DeletePhonePushRegistrationByHash(ctx, registration.TokenHash); err != nil {
				s.logger().Warn("remove invalid phone push registration", "error_class", observability.ErrorClass(err))
			}
			continue
		}
		sent = true
	}
	if !sent {
		result = "none"
		return
	}
	if err := agiCommand(reader, writer, "SET VARIABLE RINGRING_PUSH_SENT 1"); err != nil {
		result = "error"
		return
	}
	result = "sent"
}
