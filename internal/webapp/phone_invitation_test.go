package webapp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amcchord/ringring/internal/config"
	"github.com/amcchord/ringring/internal/provisioning"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
)

func TestPhoneInvitationAPIClaimsAnExtensionAndReturnsPhoneSettings(t *testing.T) {
	ctx := t.Context()
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 19, 0, 0, 0, time.UTC)
	host, err := database.UpsertGoogleUser(ctx, store.GoogleProfile{
		Subject: "phone-invite-host", Email: "host@example.test", Name: "Host",
	}, now, "usr_phone_invite")
	if err != nil {
		t.Fatal(err)
	}
	party, err := database.CreateParty(ctx, store.NewParty{
		ID: "pty_phone_invite", Name: "Color Club", Slug: "color-club", HostUserID: host.ID, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedToken := strings.Repeat("s", 43)
	if err := database.CreateInvitation(ctx, store.NewInvitation{
		ID: "inv_phone_seed", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash(seedToken), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	seedCiphertext, err := cipher.Encrypt("seed-secret", []byte("dev_phone_seed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := database.ClaimInvitation(ctx, store.NewClaim{
		TokenHash: secure.Hash(seedToken), MemberID: "mem_phone_seed", DisplayName: "Studio phone", Extension: "101",
		DeviceID: "dev_phone_seed", DeviceLabel: "Desk phone", SIPUsername: "472836",
		SIPSecretCiphertext: seedCiphertext, Provisioning: store.NewProvisioningToken{
			TokenHash: secure.Hash("phone-seed-provisioning"), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	inviteToken := strings.Repeat("i", 43)
	if err := database.CreateInvitation(ctx, store.NewInvitation{
		ID: "inv_phone_join", PartyID: party.ID, CreatedByUserID: host.ID,
		TokenHash: secure.Hash(inviteToken), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	app, err := New(config.Config{
		Environment: "development", BaseURL: "http://ringring.test", SIPPublicHost: "sip.example.test",
	}, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return now }
	server := httptest.NewServer(app)
	defer server.Close()
	inviteURL := server.URL + "/api/v1/phone-invitations/" + inviteToken
	headRequest, err := http.NewRequest(http.MethodHead, inviteURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	headResponse, err := server.Client().Do(headRequest)
	if err != nil {
		t.Fatal(err)
	}
	headResponse.Body.Close()
	if headResponse.StatusCode != http.StatusMethodNotAllowed || headResponse.Header.Get("Allow") != "GET, POST" {
		t.Fatalf("HEAD invitation response: status=%d allow=%q", headResponse.StatusCode, headResponse.Header.Get("Allow"))
	}

	previewResponse := get(t, server.Client(), inviteURL)
	previewBody := readBody(t, previewResponse)
	if previewResponse.StatusCode != http.StatusOK || !strings.Contains(previewResponse.Header.Get("Cache-Control"), "no-store") || previewResponse.Header.Get("Cross-Origin-Resource-Policy") != "same-origin" || previewResponse.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unsafe phone invitation preview: status=%d headers=%v body=%s", previewResponse.StatusCode, previewResponse.Header, previewBody)
	}
	var preview phoneInvitationPreview
	if err := json.Unmarshal([]byte(previewBody), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Version != 1 || preview.PartyName != "Color Club" || preview.SuggestedExtension != "102" || strings.Contains(previewBody, "Studio phone") || strings.Contains(previewBody, "sip") {
		t.Fatalf("unexpected invitation preview: %#v body=%s", preview, previewBody)
	}

	badRequest, err := http.NewRequest(http.MethodPost, inviteURL, strings.NewReader(`{"display_name":"Workshop phone","extension":"102","adult_extension":false,"unexpected":true}`))
	if err != nil {
		t.Fatal(err)
	}
	badRequest.Header.Set("Content-Type", "application/json")
	badResponse, err := server.Client().Do(badRequest)
	if err != nil {
		t.Fatal(err)
	}
	badBody := readBody(t, badResponse)
	if badResponse.StatusCode != http.StatusBadRequest || !strings.Contains(badBody, "Invalid JSON") {
		t.Fatalf("unknown JSON field response: status=%d body=%s", badResponse.StatusCode, badBody)
	}
	if response := get(t, server.Client(), inviteURL); response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("invalid claim consumed the invitation: status=%d", response.StatusCode)
	} else {
		response.Body.Close()
	}

	conflictResponse := postPhoneInvitation(t, server.Client(), inviteURL, phoneInvitationClaim{
		DisplayName: "Workshop phone", Extension: "101", DeviceLabel: "iPhone app",
	})
	conflictBody := readBody(t, conflictResponse)
	if conflictResponse.StatusCode != http.StatusConflict || !strings.Contains(conflictBody, "Extension unavailable") {
		t.Fatalf("extension conflict response: status=%d body=%s", conflictResponse.StatusCode, conflictBody)
	}

	claimResponse := postPhoneInvitation(t, server.Client(), inviteURL, phoneInvitationClaim{
		DisplayName: "Workshop phone", Extension: "102", AdultExtension: false, DeviceLabel: "iPhone app",
	})
	claimBody := readBody(t, claimResponse)
	if claimResponse.StatusCode != http.StatusOK || !strings.Contains(claimResponse.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("phone invitation claim: status=%d headers=%v body=%s", claimResponse.StatusCode, claimResponse.Header, claimBody)
	}
	var document provisioning.PhoneProvisioningDocument
	if err := json.Unmarshal([]byte(claimBody), &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 1 || document.SIP.Server != "sip.example.test" || document.SIP.Extension != "102" || document.SIP.Transport != "tls" || document.SIP.Password == "" {
		t.Fatalf("unexpected claimed phone account: %#v", document.SIP)
	}
	if len(document.Destinations) < 3 || document.Destinations[0].Label != "Studio phone" || document.Destinations[0].Dial != "101" || strings.Contains(claimBody, "Color Club") || strings.Contains(claimBody, "iPhone app") {
		t.Fatalf("unexpected private call menu: %#v body=%s", document.Destinations, claimBody)
	}

	usedResponse := get(t, server.Client(), inviteURL)
	usedBody := readBody(t, usedResponse)
	if usedResponse.StatusCode != http.StatusGone || !strings.Contains(usedBody, "Phone invitation unavailable") {
		t.Fatalf("used invitation response: status=%d body=%s", usedResponse.StatusCode, usedBody)
	}
	unknownResponse := get(t, server.Client(), server.URL+"/api/v1/phone-invitations/"+strings.Repeat("u", 43))
	unknownBody := readBody(t, unknownResponse)
	if unknownResponse.StatusCode != http.StatusGone || unknownBody != usedBody {
		t.Fatalf("unknown invitation disclosed different state: status=%d body=%s used=%s", unknownResponse.StatusCode, unknownBody, usedBody)
	}
}

func TestAppleAppSiteAssociationOpensOnlyInvitationLinks(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(config.Config{Environment: "development", BaseURL: "http://ringring.test"}, database, cipher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/.well-known/apple-app-site-association", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || !strings.Contains(response.Header().Get("Cache-Control"), "public") {
		t.Fatalf("AASA response: status=%d headers=%v", response.Code, response.Header())
	}
	if response.Body.String() != `{"applinks":{"apps":[],"details":[{"appID":"7PTN7E8EDS.com.mcchord.ringring","paths":["/join/*"]}]}}` {
		t.Fatalf("unexpected AASA body: %s", response.Body.String())
	}
}

func postPhoneInvitation(t *testing.T, client *http.Client, target string, claim phoneInvitationClaim) *http.Response {
	t.Helper()
	body, err := json.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
