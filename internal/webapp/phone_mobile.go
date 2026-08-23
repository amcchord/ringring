package webapp

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/amcchord/ringring/internal/observability"
	"github.com/amcchord/ringring/internal/provisioning"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
)

var phonePushTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type phoneStateDocument struct {
	Version      int                             `json:"version"`
	Extension    string                          `json:"extension"`
	Destinations []provisioning.PhoneDestination `json:"destinations"`
}

type phonePushRequest struct {
	Token       string `json:"token"`
	Environment string `json:"environment"`
}

func (a *App) phoneStateAPI(w http.ResponseWriter, r *http.Request) {
	phoneMobileHeaders(w)
	device, ok := a.authenticatePhoneAPI(w, r)
	if !ok {
		return
	}
	members, err := a.store.ListMembers(r.Context(), device.PartyID)
	if err != nil {
		a.logger.Error("load phone API menu members", "error_class", observability.ErrorClass(err))
		writeAPIProblem(w, http.StatusInternalServerError, "Phone menu unavailable", "RingRing could not safely refresh this phone menu. Please try again.")
		return
	}
	party, services, err := a.store.PartyVoiceSettings(r.Context(), device.PartyID)
	if err != nil {
		a.logger.Error("load phone API menu services", "error_class", observability.ErrorClass(err))
		writeAPIProblem(w, http.StatusInternalServerError, "Phone menu unavailable", "RingRing could not safely refresh this phone menu. Please try again.")
		return
	}
	destinations := phoneCallDestinations(device.Extension, members, availableFirstCallLines(party, services, a.cfg.AIAdultOnlyEnabled, device.AdultExtension))
	activeCalls, _, _ := a.activePartyCalls(r.Context(), device.PartyID, members)
	for _, call := range activeCalls {
		participants := append([]string(nil), call.Participants...)
		sort.Strings(participants)
		label := "Join a live call"
		if len(participants) > 0 {
			label = "Join " + strings.Join(participants, " + ")
		}
		detail := "A party call is happening now."
		if call.PhoneCount == 1 {
			detail = "1 phone is talking now."
		} else if call.PhoneCount > 1 {
			detail = strconv.Itoa(call.PhoneCount) + " phones are talking now."
		}
		destinations = append(destinations, provisioning.PhoneDestination{
			Kind: "call", Label: label, Detail: detail, Dial: call.JoinNumber,
		})
	}
	document := phoneStateDocument{Version: 1, Extension: device.Extension, Destinations: destinations}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(document); err != nil {
		a.logger.Error("write phone API menu", "error_class", observability.ErrorClass(err))
	}
}

func (a *App) phonePushAPI(w http.ResponseWriter, r *http.Request) {
	phoneMobileHeaders(w)
	device, ok := a.authenticatePhoneAPI(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodDelete {
		if err := a.store.DeletePhonePushRegistration(r.Context(), device.DeviceID); err != nil {
			a.logger.Error("delete phone push registration", "error_class", observability.ErrorClass(err))
			writeAPIProblem(w, http.StatusInternalServerError, "Background calls unavailable", "RingRing could not remove this phone's background-call registration.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.cfg.APNSConfigured() {
		writeAPIProblem(w, http.StatusServiceUnavailable, "Background calls unavailable", "This RingRing server has not configured Apple background-call delivery yet.")
		return
	}
	request, ok := decodePhonePushRequest(w, r)
	if !ok {
		return
	}
	request.Token = strings.ToLower(strings.TrimSpace(request.Token))
	if !phonePushTokenPattern.MatchString(request.Token) || request.Environment != a.cfg.APNSEnvironment {
		writeAPIProblem(w, http.StatusBadRequest, "Invalid push registration", "Send the current 32-byte VoIP token for this server's documented Apple environment.")
		return
	}
	ciphertext, err := a.cipher.Encrypt(request.Token, []byte("phone-push:"+device.DeviceID))
	if err != nil {
		a.logger.Error("encrypt phone push registration", "error_class", observability.ErrorClass(err))
		writeAPIProblem(w, http.StatusInternalServerError, "Background calls unavailable", "RingRing could not safely store this background-call registration.")
		return
	}
	if err := a.store.SavePhonePushRegistration(r.Context(), store.PhonePushRegistration{
		DeviceID: device.DeviceID, TokenHash: secure.Hash(request.Token), TokenCiphertext: ciphertext,
		Environment: request.Environment, UpdatedAt: a.now(),
	}); err != nil {
		a.logger.Error("save phone push registration", "error_class", observability.ErrorClass(err))
		writeAPIProblem(w, http.StatusInternalServerError, "Background calls unavailable", "RingRing could not safely store this background-call registration.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) authenticatePhoneAPI(w http.ResponseWriter, r *http.Request) (store.PhoneDevice, bool) {
	username, password, ok := r.BasicAuth()
	if !ok || !sipAPIUsernamePattern.MatchString(username) || password == "" || len(password) > 256 {
		writePhoneUnauthorized(w)
		return store.PhoneDevice{}, false
	}
	device, err := a.store.PhoneDeviceBySIPUsername(r.Context(), username)
	if errors.Is(err, store.ErrNotFound) {
		writePhoneUnauthorized(w)
		return store.PhoneDevice{}, false
	}
	if err != nil {
		a.logger.Error("load authenticated phone API device", "error_class", observability.ErrorClass(err))
		writeAPIProblem(w, http.StatusInternalServerError, "Phone service unavailable", "RingRing could not safely authenticate this phone.")
		return store.PhoneDevice{}, false
	}
	expected, err := a.cipher.Decrypt(device.SIPSecretCiphertext, []byte(device.DeviceID))
	if err != nil {
		a.logger.Error("decrypt authenticated phone API credential", "error_class", observability.ErrorClass(err))
		writeAPIProblem(w, http.StatusInternalServerError, "Phone service unavailable", "RingRing could not safely authenticate this phone.")
		return store.PhoneDevice{}, false
	}
	wantedHash := sha256.Sum256([]byte(expected))
	providedHash := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(wantedHash[:], providedHash[:]) != 1 {
		writePhoneUnauthorized(w)
		return store.PhoneDevice{}, false
	}
	return device, true
}

var sipAPIUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func decodePhonePushRequest(w http.ResponseWriter, r *http.Request) (phonePushRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIProblem(w, http.StatusUnsupportedMediaType, "JSON required", "Send the push registration as application/json.")
		return phonePushRequest{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request phonePushRequest
	if err := decoder.Decode(&request); err != nil {
		var maximumError *http.MaxBytesError
		if errors.As(err, &maximumError) {
			writeAPIProblem(w, http.StatusRequestEntityTooLarge, "Push registration too large", "Send only the documented push-registration fields.")
			return phonePushRequest{}, false
		}
		writeAPIProblem(w, http.StatusBadRequest, "Invalid JSON", "Send one complete push-registration object.")
		return phonePushRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIProblem(w, http.StatusBadRequest, "Invalid JSON", "Send exactly one JSON object.")
		return phonePushRequest{}, false
	}
	return request, true
}

func phoneMobileHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Vary", "Authorization")
}

func writePhoneUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="RingRing phone", charset="UTF-8"`)
	writeAPIProblem(w, http.StatusUnauthorized, "Phone authentication failed", "This phone's private RingRing credential is invalid or has been revoked.")
}
