package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/amcchord/ringring/internal/config"
	"github.com/amcchord/ringring/internal/localauth"
	"github.com/amcchord/ringring/internal/model"
	"github.com/amcchord/ringring/internal/openaiadmin"
	"github.com/amcchord/ringring/internal/secure"
	"github.com/amcchord/ringring/internal/store"
	"github.com/amcchord/ringring/internal/telephony"
	"github.com/amcchord/ringring/internal/weather"
	webassets "github.com/amcchord/ringring/web"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	sessionCookie       = "ringring_session"
	oauthStateCookie    = "ringring_oauth_state"
	oauthVerifierCookie = "ringring_oauth_verifier"
	inviteFlashCookie   = "ringring_invite_reveal"
	joinCSRFCookie      = "ringring_join_csrf"
	authCSRFCookie      = "ringring_auth_csrf"
	recoveryFlashCookie = "ringring_recovery_reveal"
)

var (
	extensionPattern = regexp.MustCompile(`^[0-9]{2,5}$`)
	usernamePattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{1,30}[a-z0-9])$`)
)

type App struct {
	cfg       config.Config
	store     *store.Store
	cipher    *secure.Cipher
	openAI    *openaiadmin.Client
	telephony *telephony.Reconciler
	weather   weatherGeocoder
	oauth     *oauth2.Config
	logger    *slog.Logger
	handler   http.Handler
	static    http.Handler
	now       func() time.Time
	authUsers *rateLimiter
	authSlots chan struct{}
	dummyHash string
}

type weatherGeocoder interface {
	Geocode(context.Context, string) (weather.Location, error)
}

type PageData struct {
	BodyClass      string
	User           *model.User
	CSRF           string
	AuthConfigured bool
	DevAuth        bool
	Parties        []model.Party
	Party          model.Party
	Members        []model.Member
	Services       model.PartyServices
	InviteURL      string
	JoinCSRF       string
	Claim          model.ClaimedDevice
	SIPPublicHost  string
	SetupForHost   bool
	PartyURL       string
	ErrorTitle     string
	ErrorMessage   string
	ErrorBackURL   string
	ErrorBackLabel string
	AuthCSRF       string
	FormError      string
	FormUsername   string
	FormName       string
	SignupEnabled  bool
	SignupCode     bool
	RecoveryCodes  []string
	RecoveryTitle  string
	RecoveryText   string
	RecoveryNext   string
	RecoveryButton string
}

type authSession struct {
	User  model.User
	Token string
}

type setupFlash struct {
	PartyID     string `json:"party_id"`
	MemberID    string `json:"member_id"`
	MemberName  string `json:"member_name"`
	Extension   string `json:"extension"`
	DeviceID    string `json:"device_id"`
	DeviceLabel string `json:"device_label"`
	SIPUsername string `json:"sip_username"`
	SIPSecret   string `json:"sip_secret"`
}

type recoveryFlash struct {
	Kind     string   `json:"kind"`
	Username string   `json:"username"`
	Codes    []string `json:"codes"`
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func New(cfg config.Config, database *store.Store, cipher *secure.Cipher, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return nil, fmt.Errorf("load static assets: %w", err)
	}

	dummyHash, err := localauth.HashPassword("ringring timing equalizer only")
	if err != nil {
		return nil, fmt.Errorf("prepare local authentication: %w", err)
	}
	app := &App{
		cfg:       cfg,
		store:     database,
		cipher:    cipher,
		logger:    logger,
		now:       time.Now,
		static:    http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
		weather:   weather.New(nil),
		authSlots: make(chan struct{}, 4),
		dummyHash: dummyHash,
	}
	app.authUsers = newRateLimiter(func() time.Time { return app.now() })
	if cfg.OpenAIProvisioningConfigured() {
		app.openAI = openaiadmin.New(cfg.OpenAIAdminKey, cfg.OpenAIPartySpendLimitCents, &http.Client{Timeout: 30 * time.Second})
	}
	if cfg.GoogleAuthConfigured() {
		app.oauth = &oauth2.Config{
			ClientID: cfg.GoogleClientID, ClientSecret: cfg.GoogleClientSecret,
			RedirectURL: cfg.BaseURL + "/auth/google/callback",
			Scopes:      []string{"openid", "email", "profile"}, Endpoint: google.Endpoint,
		}
	}
	if cfg.AsteriskConfigDir != "" {
		app.telephony = &telephony.Reconciler{
			Source: database, Cipher: cipher, ConfigDir: cfg.AsteriskConfigDir,
			Reloader: telephony.AMI{Address: cfg.AsteriskAMIAddr, Username: cfg.AsteriskAMIUser, Secret: cfg.AsteriskAMISecret},
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.health)
	mux.HandleFunc("GET /readyz", app.ready)
	mux.Handle("GET /static/", app.static)
	mux.HandleFunc("GET /signup", app.signupForm)
	mux.HandleFunc("POST /signup", app.signup)
	mux.HandleFunc("GET /login", app.loginForm)
	mux.HandleFunc("POST /login", app.login)
	mux.HandleFunc("GET /recover", app.recoverForm)
	mux.HandleFunc("POST /recover", app.recover)
	mux.HandleFunc("GET /account/recovery-codes", app.recoveryCodes)
	mux.HandleFunc("GET /auth/google", app.googleLogin)
	mux.HandleFunc("GET /auth/google/callback", app.googleCallback)
	mux.HandleFunc("POST /auth/dev", app.devLogin)
	mux.HandleFunc("POST /auth/logout", app.requireUser(app.logout))
	mux.HandleFunc("GET /app", app.requireUser(app.dashboard))
	mux.HandleFunc("POST /parties", app.requireUser(app.createParty))
	mux.HandleFunc("GET /parties/{partyID}", app.requireUser(app.party))
	mux.HandleFunc("POST /parties/{partyID}/invites", app.requireUser(app.createInvitation))
	mux.HandleFunc("POST /parties/{partyID}/services", app.requireUser(app.updateServices))
	mux.HandleFunc("GET /parties/{partyID}/setup", app.requireUser(app.rotatedSetup))
	mux.HandleFunc("POST /parties/{partyID}/devices/{deviceID}/rotate", app.requireUser(app.rotateDevice))
	mux.HandleFunc("POST /parties/{partyID}/devices/{deviceID}/revoke", app.requireUser(app.revokeDevice))
	mux.HandleFunc("GET /join/{token}", app.join)
	mux.HandleFunc("POST /join/{token}", app.claimInvitation)
	mux.HandleFunc("GET /", app.home)

	app.handler = app.securityHeaders(app.recoverPanic(app.requestLog(app.rateLimit(mux))))
	return app, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *App) ReconcileTelephony(ctx context.Context) error {
	if a.telephony == nil {
		return nil
	}
	return a.telephony.Reconcile(ctx)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		a.errorPage(w, http.StatusNotFound, "That number is not in service", "The page you dialed does not exist.", "/", "Back home")
		return
	}
	session, _ := a.currentSession(r)
	data := a.pageData(session)
	data.BodyClass = "home-page"
	a.render(w, "home", data)
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	a.health(w, r)
}

func (a *App) signupForm(w http.ResponseWriter, r *http.Request) {
	if session, _ := a.currentSession(r); session != nil {
		http.Redirect(w, r, "/app", http.StatusFound)
		return
	}
	data := a.authPageData(w, r)
	data.BodyClass = "auth-page"
	if !a.cfg.HostSignupEnabled() {
		data.FormError = "Host signup is temporarily closed. Existing hosts can still sign in."
	}
	a.renderNoStore(w, "signup", data)
}

func (a *App) signup(w http.ResponseWriter, r *http.Request) {
	if !a.parseSmallForm(w, r) {
		return
	}
	if !a.validAuthForm(r) {
		a.authError(w, r, "signup", "That form expired. Refresh the page and try again.", http.StatusForbidden)
		return
	}
	if !a.cfg.HostSignupEnabled() {
		a.authError(w, r, "signup", "Host signup is temporarily closed. Existing hosts can still sign in.", http.StatusServiceUnavailable)
		return
	}
	name := strings.Join(strings.Fields(r.FormValue("name")), " ")
	username := normalizeUsername(r.FormValue("username"))
	password := r.FormValue("password")
	if a.cfg.HostSignupCode != "" && !secure.Equal(r.FormValue("signup_code"), a.cfg.HostSignupCode) {
		a.authError(w, r, "signup", "That family access code did not match.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 40 {
		a.authError(w, r, "signup", "Use a name from 1 to 40 characters.", http.StatusBadRequest)
		return
	}
	if !usernamePattern.MatchString(username) {
		a.authError(w, r, "signup", "Choose 3–32 letters or numbers. Dots, dashes, and underscores are okay in the middle.", http.StatusBadRequest)
		return
	}
	if message := passwordProblem(password, r.FormValue("password_confirm")); message != "" {
		a.authError(w, r, "signup", message, http.StatusBadRequest)
		return
	}
	if !a.takeAuthSlot() {
		a.authError(w, r, "signup", "A lot of people are ringing at once. Please try again in a moment.", http.StatusTooManyRequests)
		return
	}
	defer a.releaseAuthSlot()

	passwordHash, err := localauth.HashPassword(password)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	codes, codeHashes, err := newRecoveryCodes(8)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	userID, err := secure.ID("usr")
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	sessionToken, err := secure.Token(32)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	expires := a.now().Add(30 * 24 * time.Hour)
	_, err = a.store.CreateLocalUser(r.Context(), store.NewLocalUser{
		ID: userID, Name: name, Username: username, PasswordHash: passwordHash,
		RecoveryCodeHashes: codeHashes, SessionTokenHash: secure.Hash(sessionToken),
		SessionExpiresAt: expires, CreatedAt: a.now(),
	})
	if errors.Is(err, store.ErrUsernameTaken) {
		a.authError(w, r, "signup", "That username is not available. Try a slightly different one.", http.StatusConflict)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.setSessionCookie(w, sessionToken, expires)
	a.clearCookie(w, authCSRFCookie, "/")
	flash := recoveryFlash{Kind: "signup", Username: username, Codes: codes}
	if err := a.setRecoveryFlash(w, flash); err != nil {
		a.logger.Error("set signup recovery reveal", "error", err)
		a.renderRecoveryCodes(w, r, flash)
		return
	}
	http.Redirect(w, r, "/account/recovery-codes", http.StatusSeeOther)
}

func (a *App) loginForm(w http.ResponseWriter, r *http.Request) {
	if session, _ := a.currentSession(r); session != nil {
		http.Redirect(w, r, "/app", http.StatusFound)
		return
	}
	data := a.authPageData(w, r)
	data.BodyClass = "auth-page"
	a.renderNoStore(w, "login", data)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.parseSmallForm(w, r) {
		return
	}
	if !a.validAuthForm(r) {
		a.authError(w, r, "login", "That form expired. Refresh the page and try again.", http.StatusForbidden)
		return
	}
	username := normalizeUsername(r.FormValue("username"))
	if !a.authUsers.allow("login:"+username, 10, 15*time.Minute) {
		a.authError(w, r, "login", "Too many sign-in attempts. Wait a few minutes and try again.", http.StatusTooManyRequests)
		return
	}
	if !a.takeAuthSlot() {
		a.authError(w, r, "login", "A lot of people are ringing at once. Please try again in a moment.", http.StatusTooManyRequests)
		return
	}
	defer a.releaseAuthSlot()

	credential, err := a.store.LocalCredentialByUsername(r.Context(), username)
	encoded := a.dummyHash
	if err == nil {
		encoded = credential.PasswordHash
	} else if !errors.Is(err, store.ErrNotFound) {
		a.internalError(w, r, err)
		return
	}
	password := r.FormValue("password")
	if len(password) > 512 || !utf8.ValidString(password) {
		password = "invalid oversized password"
	}
	valid, verifyErr := localauth.VerifyPassword(encoded, password)
	if verifyErr != nil {
		a.logger.Error("verify local password", "error", verifyErr)
		valid = false
	}
	if err != nil || !valid {
		a.authError(w, r, "login", "That username and password did not match.", http.StatusUnauthorized)
		return
	}
	if err := a.startUserSession(w, r, credential.User); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.clearCookie(w, authCSRFCookie, "/")
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (a *App) recoverForm(w http.ResponseWriter, r *http.Request) {
	data := a.authPageData(w, r)
	data.BodyClass = "auth-page"
	a.renderNoStore(w, "recover", data)
}

func (a *App) recover(w http.ResponseWriter, r *http.Request) {
	if !a.parseSmallForm(w, r) {
		return
	}
	if !a.validAuthForm(r) {
		a.authError(w, r, "recover", "That form expired. Refresh the page and try again.", http.StatusForbidden)
		return
	}
	username := normalizeUsername(r.FormValue("username"))
	if !a.authUsers.allow("recover:"+username, 6, 15*time.Minute) {
		a.authError(w, r, "recover", "Too many recovery attempts. Wait a few minutes and try again.", http.StatusTooManyRequests)
		return
	}
	if message := passwordProblem(r.FormValue("password"), r.FormValue("password_confirm")); message != "" {
		a.authError(w, r, "recover", message, http.StatusBadRequest)
		return
	}
	if !a.takeAuthSlot() {
		a.authError(w, r, "recover", "A lot of people are ringing at once. Please try again in a moment.", http.StatusTooManyRequests)
		return
	}
	defer a.releaseAuthSlot()

	passwordHash, err := localauth.HashPassword(r.FormValue("password"))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	codes, codeHashes, err := newRecoveryCodes(8)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	err = a.store.RecoverLocalUser(r.Context(), username, localauth.RecoveryCodeHash(r.FormValue("recovery_code")), passwordHash, codeHashes, a.now())
	if errors.Is(err, store.ErrRecoveryCode) {
		a.authError(w, r, "recover", "Those account and recovery details did not match.", http.StatusUnauthorized)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.clearCookie(w, sessionCookie, "/")
	a.clearCookie(w, authCSRFCookie, "/")
	flash := recoveryFlash{Kind: "reset", Username: username, Codes: codes}
	if err := a.setRecoveryFlash(w, flash); err != nil {
		a.logger.Error("set reset recovery reveal", "error", err)
		a.renderRecoveryCodes(w, r, flash)
		return
	}
	http.Redirect(w, r, "/account/recovery-codes", http.StatusSeeOther)
}

func (a *App) recoveryCodes(w http.ResponseWriter, r *http.Request) {
	flash, err := a.readRecoveryFlash(w, r)
	if err != nil {
		a.errorPage(w, http.StatusGone, "Those codes have left the screen", "Create an account or use another saved recovery code to make a fresh set.", "/login", "Back to sign in")
		return
	}
	a.renderRecoveryCodes(w, r, flash)
}

func (a *App) renderRecoveryCodes(w http.ResponseWriter, r *http.Request, flash recoveryFlash) {
	session, _ := a.currentSession(r)
	data := a.pageData(session)
	data.BodyClass = "auth-page"
	data.RecoveryCodes = flash.Codes
	data.FormUsername = flash.Username
	data.RecoveryTitle = "Save your recovery codes"
	data.RecoveryText = "These are the only way to reset your password without email. Put them in a password manager or print them; each code works once."
	data.RecoveryNext = "/app"
	data.RecoveryButton = "Open my parties"
	if flash.Kind == "reset" {
		data.RecoveryTitle = "Your password is reset"
		data.RecoveryText = "All old sessions and recovery codes are now invalid. Save this fresh set before signing in."
		data.RecoveryNext = "/login"
		data.RecoveryButton = "Sign in with the new password"
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	a.renderNoStore(w, "recovery_codes", data)
}

func (a *App) authPageData(w http.ResponseWriter, r *http.Request) PageData {
	token, err := secure.Token(24)
	if err != nil {
		a.logger.Error("create authentication CSRF token", "error", err)
		return a.pageData(nil)
	}
	http.SetCookie(w, &http.Cookie{
		Name: authCSRFCookie, Value: token, Path: "/", MaxAge: 20 * 60,
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteStrictMode,
	})
	data := a.pageData(nil)
	data.AuthCSRF = token
	data.SignupEnabled = a.cfg.HostSignupEnabled()
	data.SignupCode = a.cfg.HostSignupCode != ""
	return data
}

func (a *App) authError(w http.ResponseWriter, r *http.Request, page, message string, status int) {
	data := a.authPageData(w, r)
	data.BodyClass = "auth-page"
	data.FormError = message
	data.FormUsername = normalizeUsername(r.FormValue("username"))
	data.FormName = strings.Join(strings.Fields(r.FormValue("name")), " ")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	a.render(w, page, data)
}

func (a *App) validAuthForm(r *http.Request) bool {
	cookie, err := r.Cookie(authCSRFCookie)
	return err == nil && a.sameOrigin(r) && secure.Equal(cookie.Value, r.FormValue("csrf"))
}

func (a *App) parseSmallForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form is too large", http.StatusRequestEntityTooLarge)
		return false
	}
	return true
}

func (a *App) takeAuthSlot() bool {
	select {
	case a.authSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *App) releaseAuthSlot() {
	<-a.authSlots
}

func normalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func passwordProblem(password, confirmation string) string {
	runes := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || runes < 12 || runes > 128 || len(password) > 512 {
		return "Use a password or passphrase from 12 to 128 characters."
	}
	if password != confirmation {
		return "The two passwords did not match."
	}
	return ""
}

func newRecoveryCodes(count int) ([]string, [][]byte, error) {
	codes := make([]string, 0, count)
	hashes := make([][]byte, 0, count)
	for range count {
		code, err := localauth.NewRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, localauth.RecoveryCodeHash(code))
	}
	return codes, hashes, nil
}

func (a *App) googleLogin(w http.ResponseWriter, r *http.Request) {
	if a.oauth == nil {
		a.errorPage(w, http.StatusServiceUnavailable, "Host sign-up is not open yet", "Google login still needs to be connected.", "/", "Back home")
		return
	}
	state, err := secure.Token(32)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	verifier := oauth2.GenerateVerifier()
	a.setShortCookie(w, oauthStateCookie, state, "/auth/google/callback")
	a.setShortCookie(w, oauthVerifierCookie, verifier, "/auth/google/callback")
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *App) googleCallback(w http.ResponseWriter, r *http.Request) {
	if a.oauth == nil {
		a.errorPage(w, http.StatusServiceUnavailable, "Google login is not configured", "Ask the RingRing administrator to finish setup.", "/", "Back home")
		return
	}
	stateCookie, stateErr := r.Cookie(oauthStateCookie)
	verifierCookie, verifierErr := r.Cookie(oauthVerifierCookie)
	a.clearCookie(w, oauthStateCookie, "/auth/google/callback")
	a.clearCookie(w, oauthVerifierCookie, "/auth/google/callback")
	if stateErr != nil || verifierErr != nil || !secure.Equal(stateCookie.Value, r.URL.Query().Get("state")) {
		a.errorPage(w, http.StatusBadRequest, "That sign-in expired", "Please start the Google sign-in again.", "/auth/google", "Try again")
		return
	}
	if oauthError := r.URL.Query().Get("error"); oauthError != "" {
		a.errorPage(w, http.StatusBadRequest, "Google sign-in stopped", "No changes were made. You can try again whenever you are ready.", "/", "Back home")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	token, err := a.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifierCookie.Value))
	if err != nil {
		a.logger.Warn("Google OAuth exchange failed", "error", err)
		a.errorPage(w, http.StatusBadGateway, "Google did not answer", "Please try signing in again.", "/auth/google", "Try again")
		return
	}
	profile, err := googleProfile(ctx, a.oauth.Client(ctx, token))
	if err != nil {
		a.logger.Warn("Google profile request failed", "error", err)
		a.errorPage(w, http.StatusBadGateway, "We could not read your profile", "Please try signing in again.", "/auth/google", "Try again")
		return
	}
	if !profile.EmailVerified {
		a.errorPage(w, http.StatusForbidden, "A verified email is required", "Verify the email on your Google account and try again.", "/", "Back home")
		return
	}
	if err := a.finishLogin(w, r, store.GoogleProfile{Subject: profile.Subject, Email: profile.Email, Name: profile.Name, AvatarURL: profile.Picture}); err != nil {
		a.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (a *App) devLogin(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.DevAuth || a.cfg.Environment == "production" {
		http.NotFound(w, r)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		email = "host@ringring.test"
	}
	if err := a.finishLogin(w, r, store.GoogleProfile{Subject: "dev:" + email, Email: email, Name: "Local Host"}); err != nil {
		a.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (a *App) finishLogin(w http.ResponseWriter, r *http.Request, profile store.GoogleProfile) error {
	userID, err := secure.ID("usr")
	if err != nil {
		return err
	}
	user, err := a.store.UpsertGoogleUser(r.Context(), profile, a.now(), userID)
	if err != nil {
		return err
	}
	return a.startUserSession(w, r, user)
}

func (a *App) startUserSession(w http.ResponseWriter, r *http.Request, user model.User) error {
	token, err := secure.Token(32)
	if err != nil {
		return err
	}
	expires := a.now().Add(30 * 24 * time.Hour)
	if err := a.store.CreateSession(r.Context(), secure.Hash(token), user.ID, expires, a.now()); err != nil {
		return err
	}
	a.setSessionCookie(w, token, expires)
	return nil
}

func (a *App) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		MaxAge: int((30 * 24 * time.Hour).Seconds()), HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request, session authSession) {
	if !a.validCSRF(r, session) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	if err := a.store.DeleteSession(r.Context(), secure.Hash(session.Token)); err != nil {
		a.logger.Warn("delete session", "error", err)
	}
	a.clearCookie(w, sessionCookie, "/")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request, session authSession) {
	parties, err := a.store.ListPartiesByHost(r.Context(), session.User.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	data := a.pageData(&session)
	data.BodyClass = "app-page"
	data.Parties = parties
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, "dashboard", data)
}

func (a *App) createParty(w http.ResponseWriter, r *http.Request, session authSession) {
	if !a.validCSRF(r, session) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	name := strings.Join(strings.Fields(r.FormValue("name")), " ")
	if utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 48 {
		a.errorPage(w, http.StatusBadRequest, "Choose a party name", "Party names can be 2 to 48 characters.", "/app", "Back to my parties")
		return
	}
	partyID, err := secure.ID("pty")
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	suffix := strings.ToLower(strings.ReplaceAll(partyID[len(partyID)-6:], "_", ""))
	party, err := a.store.CreateParty(r.Context(), store.NewParty{
		ID: partyID, Name: name, Slug: slugify(name) + "-" + suffix, HostUserID: session.User.ID, CreatedAt: a.now(),
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.provisionOpenAI(r.Context(), party); err != nil {
		a.logger.Warn("party OpenAI provisioning failed", "party_id", party.ID, "error", err)
		_ = a.store.UpdatePartyOpenAI(r.Context(), party.ID, "", "", "", "error")
	}
	http.Redirect(w, r, "/parties/"+url.PathEscape(party.ID), http.StatusSeeOther)
}

func (a *App) provisionOpenAI(ctx context.Context, party model.Party) error {
	if a.openAI == nil {
		return a.store.UpdatePartyOpenAI(ctx, party.ID, "", "", "", "not-configured")
	}
	provisioned, err := a.openAI.Provision(ctx, party.ID, party.Name)
	if err != nil {
		return err
	}
	ciphertext, err := a.cipher.Encrypt(provisioned.APIKey, []byte(party.ID))
	if err != nil {
		return err
	}
	return a.store.UpdatePartyOpenAI(ctx, party.ID, provisioned.ProjectID, provisioned.ServiceAccountID, ciphertext, "ready")
}

func (a *App) party(w http.ResponseWriter, r *http.Request, session authSession) {
	partyID := r.PathValue("partyID")
	party, err := a.store.PartyForHost(r.Context(), partyID, session.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		a.errorPage(w, http.StatusNotFound, "That party is not here", "It may belong to another host or no longer exist.", "/app", "Back to my parties")
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	members, err := a.store.ListMembers(r.Context(), party.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	services, err := a.store.PartyServices(r.Context(), party.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	data := a.pageData(&session)
	data.Party = party
	data.Members = members
	data.Services = services
	data.InviteURL = a.readInviteFlash(w, r, party.ID)
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, "party", data)
}

func (a *App) updateServices(w http.ResponseWriter, r *http.Request, session authSession) {
	if !a.validCSRF(r, session) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	partyID := r.PathValue("partyID")
	party, err := a.store.PartyForHost(r.Context(), partyID, session.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	existing, err := a.store.PartyServices(r.Context(), partyID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	query := strings.Join(strings.Fields(r.FormValue("weather_query")), " ")
	weatherEnabled := r.FormValue("weather_enabled") != ""
	if weatherEnabled && party.OpenAIStatus != "ready" {
		a.errorPage(w, http.StatusConflict, "The weather line needs its voice", "Wait until this party's AI line says ready, then turn weather on.", "/parties/"+url.PathEscape(partyID), "Back to the party")
		return
	}
	if weatherEnabled && query == "" {
		a.errorPage(w, http.StatusBadRequest, "Add a weather location", "Enter a city or postal code before turning on the weather line.", "/parties/"+url.PathEscape(partyID), "Back to the party")
		return
	}

	location := weather.Location{Query: query}
	if query == existing.WeatherQuery {
		location.Label = existing.WeatherLabel
		location.Latitude = existing.WeatherLatitude
		location.Longitude = existing.WeatherLongitude
	}
	if query != "" && (query != existing.WeatherQuery || location.Label == "") {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		location, err = a.weather.Geocode(ctx, query)
		if err != nil {
			a.logger.Warn("weather location lookup failed", "party_id", partyID, "error", err)
			a.errorPage(w, http.StatusBadRequest, "We could not find that place", "Try a city with its state or country, or use a postal code.", "/parties/"+url.PathEscape(partyID), "Back to the party")
			return
		}
	}
	_, err = a.store.UpdatePartyServices(r.Context(), partyID, session.User.ID, store.ServiceSettingsInput{
		TimeEnabled: r.FormValue("time_enabled") != "", WeatherEnabled: weatherEnabled,
		WeatherQuery: location.Query, WeatherLabel: location.Label,
		WeatherLatitude: location.Latitude, WeatherLongitude: location.Longitude,
		RadioEnabled: r.FormValue("radio_enabled") != "", UpdatedAt: a.now(),
	})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.ReconcileTelephony(r.Context()); err != nil {
		a.logger.Error("telephony reconcile after service update", "party_id", partyID, "error", err)
	}
	http.Redirect(w, r, "/parties/"+url.PathEscape(partyID), http.StatusSeeOther)
}

func (a *App) createInvitation(w http.ResponseWriter, r *http.Request, session authSession) {
	if !a.validCSRF(r, session) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	partyID := r.PathValue("partyID")
	if _, err := a.store.PartyForHost(r.Context(), partyID, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.internalError(w, r, err)
		return
	}
	token, err := secure.Token(32)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	inviteID, err := secure.ID("inv")
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.store.CreateInvitation(r.Context(), store.NewInvitation{
		ID: inviteID, PartyID: partyID, CreatedByUserID: session.User.ID, TokenHash: secure.Hash(token),
		ExpiresAt: a.now().Add(a.cfg.InviteTTL), CreatedAt: a.now(),
	}); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.setInviteFlash(w, partyID, token)
	http.Redirect(w, r, "/parties/"+url.PathEscape(partyID), http.StatusSeeOther)
}

func (a *App) rotateDevice(w http.ResponseWriter, r *http.Request, session authSession) {
	if !a.validCSRF(r, session) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	partyID := r.PathValue("partyID")
	deviceID := r.PathValue("deviceID")
	sipUsername, sipSecret, ciphertext, err := a.newSIPCredentials(deviceID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	rotated, err := a.store.RotateDevice(r.Context(), partyID, session.User.ID, deviceID, sipUsername, ciphertext)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.ReconcileTelephony(r.Context()); err != nil {
		a.logger.Error("telephony reconcile after device rotation", "party_id", partyID, "device_id", deviceID, "error", err)
	}
	a.setSetupFlash(w, setupFlash{
		PartyID: partyID, MemberID: rotated.Member.ID, MemberName: rotated.Member.DisplayName,
		Extension: rotated.Member.Extension, DeviceID: rotated.Device.ID, DeviceLabel: rotated.Device.Label,
		SIPUsername: sipUsername, SIPSecret: sipSecret,
	})
	http.Redirect(w, r, "/parties/"+url.PathEscape(partyID)+"/setup", http.StatusSeeOther)
}

func (a *App) revokeDevice(w http.ResponseWriter, r *http.Request, session authSession) {
	if !a.validCSRF(r, session) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	partyID := r.PathValue("partyID")
	if err := a.store.RevokeDevice(r.Context(), partyID, session.User.ID, r.PathValue("deviceID"), a.now()); errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.ReconcileTelephony(r.Context()); err != nil {
		a.logger.Error("telephony reconcile after device revocation", "party_id", partyID, "device_id", r.PathValue("deviceID"), "error", err)
	}
	http.Redirect(w, r, "/parties/"+url.PathEscape(partyID), http.StatusSeeOther)
}

func (a *App) rotatedSetup(w http.ResponseWriter, r *http.Request, session authSession) {
	partyID := r.PathValue("partyID")
	party, err := a.store.PartyForHost(r.Context(), partyID, session.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	flash, err := a.readSetupFlash(w, r, partyID)
	if err != nil {
		a.errorPage(w, http.StatusGone, "Those settings have left the screen", "Rotate the phone again to make a fresh one-time setup card.", "/parties/"+url.PathEscape(partyID), "Back to the party")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.pageData(&session)
	data.SetupForHost = true
	data.PartyURL = "/parties/" + url.PathEscape(partyID)
	data.SIPPublicHost = a.cfg.SIPPublicHost
	data.Claim = model.ClaimedDevice{
		Party:     party,
		Member:    model.Member{ID: flash.MemberID, PartyID: partyID, DisplayName: flash.MemberName, Extension: flash.Extension},
		Device:    model.Device{ID: flash.DeviceID, MemberID: flash.MemberID, Label: flash.DeviceLabel, SIPUsername: flash.SIPUsername},
		SIPSecret: flash.SIPSecret,
	}
	a.render(w, "setup", data)
}

func (a *App) join(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	party, err := a.store.PartyByInvitation(r.Context(), secure.Hash(token), a.now())
	if err != nil {
		a.invitationError(w, err)
		return
	}
	csrf, err := secure.Token(24)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: joinCSRFCookie, Value: csrf, Path: r.URL.Path, MaxAge: 15 * 60,
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.pageData(nil)
	data.Party = party
	data.JoinCSRF = csrf
	a.render(w, "join", data)
}

func (a *App) claimInvitation(w http.ResponseWriter, r *http.Request) {
	csrfCookie, err := r.Cookie(joinCSRFCookie)
	csrfValue := r.FormValue("csrf")
	originOK := a.sameOrigin(r)
	if err != nil || !secure.Equal(csrfCookie.Value, csrfValue) || !originOK {
		a.logger.Warn("rejected invitation claim", "csrf_cookie_present", err == nil, "csrf_form_present", csrfValue != "", "origin_ok", originOK, "origin", r.Header.Get("Origin"))
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	token := r.PathValue("token")
	displayName := strings.Join(strings.Fields(r.FormValue("display_name")), " ")
	extension := strings.TrimSpace(r.FormValue("extension"))
	deviceLabel := strings.Join(strings.Fields(r.FormValue("device_label")), " ")
	if deviceLabel == "" {
		deviceLabel = "My phone"
	}
	if utf8.RuneCountInString(displayName) < 1 || utf8.RuneCountInString(displayName) > 40 ||
		utf8.RuneCountInString(deviceLabel) > 40 || !validExtension(extension) {
		a.errorPage(w, http.StatusBadRequest, "Check those phone details", "Use a name up to 40 characters and an extension with 2 to 5 digits.", r.URL.Path, "Try again")
		return
	}
	memberID, err := secure.ID("mem")
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	deviceID, err := secure.ID("dev")
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	sipUsername, sipSecret, ciphertext, err := a.newSIPCredentials(deviceID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	party, member, device, err := a.store.ClaimInvitation(r.Context(), store.NewClaim{
		TokenHash: secure.Hash(token), MemberID: memberID, DisplayName: displayName, Extension: extension,
		DeviceID: deviceID, DeviceLabel: deviceLabel, SIPUsername: sipUsername,
		SIPSecretCiphertext: ciphertext, Now: a.now(),
	})
	if errors.Is(err, store.ErrExtensionTaken) {
		a.errorPage(w, http.StatusConflict, "That extension is already ringing", "Choose another short number and try again.", r.URL.Path, "Choose another")
		return
	}
	if err != nil {
		a.invitationError(w, err)
		return
	}
	a.clearCookie(w, joinCSRFCookie, r.URL.Path)
	if err := a.ReconcileTelephony(r.Context()); err != nil {
		a.logger.Error("telephony reconcile after invitation claim", "party_id", party.ID, "device_id", device.ID, "error", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.pageData(nil)
	data.Claim = model.ClaimedDevice{Party: party, Member: member, Device: device, SIPSecret: sipSecret}
	data.SIPPublicHost = a.cfg.SIPPublicHost
	a.render(w, "setup", data)
}

func (a *App) newSIPCredentials(deviceID string) (string, string, string, error) {
	suffix, err := secure.Token(18)
	if err != nil {
		return "", "", "", err
	}
	secret, err := secure.Token(24)
	if err != nil {
		return "", "", "", err
	}
	ciphertext, err := a.cipher.Encrypt(secret, []byte(deviceID))
	if err != nil {
		return "", "", "", err
	}
	return "rrd_" + suffix, secret, ciphertext, nil
}

func (a *App) requireUser(next func(http.ResponseWriter, *http.Request, authSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := a.currentSession(r)
		if err != nil {
			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/login", http.StatusFound)
			} else {
				http.Error(w, "sign in required", http.StatusUnauthorized)
			}
			return
		}
		next(w, r, *session)
	}
}

func (a *App) currentSession(r *http.Request) (*authSession, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, store.ErrNotFound
	}
	user, err := a.store.UserBySession(r.Context(), secure.Hash(cookie.Value), a.now())
	if err != nil {
		return nil, err
	}
	return &authSession{User: user, Token: cookie.Value}, nil
}

func (a *App) validCSRF(r *http.Request, session authSession) bool {
	return a.sameOrigin(r) && secure.Equal(r.FormValue("csrf"), secure.CSRF(session.Token, a.cfg.SessionSecret))
}

func (a *App) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Sandboxed local preview browsers may serialize their origin as null.
	// Never allow that exception in production.
	if origin == "null" && a.cfg.Environment != "production" {
		return true
	}
	want, err := url.Parse(a.cfg.BaseURL)
	if err != nil {
		return false
	}
	got, err := url.Parse(origin)
	return err == nil && strings.EqualFold(got.Scheme, want.Scheme) && strings.EqualFold(got.Host, want.Host)
}

func (a *App) pageData(session *authSession) PageData {
	data := PageData{
		AuthConfigured: a.cfg.HostSignupEnabled(), DevAuth: a.cfg.DevAuth,
		SignupEnabled: a.cfg.HostSignupEnabled(), SignupCode: a.cfg.HostSignupCode != "",
	}
	if session != nil {
		data.User = &session.User
		data.CSRF = secure.CSRF(session.Token, a.cfg.SessionSecret)
	}
	return data
}

func (a *App) renderNoStore(w http.ResponseWriter, page string, data PageData) {
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, page, data)
}

func (a *App) render(w http.ResponseWriter, page string, data PageData) {
	parsed, err := template.ParseFS(webassets.Files, "templates/base.html", "templates/"+page+".html")
	if err != nil {
		a.logger.Error("parse templates", "page", page, "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := parsed.ExecuteTemplate(w, "base", data); err != nil {
		a.logger.Error("render template", "page", page, "error", err)
	}
}

func (a *App) errorPage(w http.ResponseWriter, status int, title, message, backURL, backLabel string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	a.render(w, "error", PageData{
		ErrorTitle: title, ErrorMessage: message, ErrorBackURL: backURL, ErrorBackLabel: backLabel,
		AuthConfigured: a.cfg.HostSignupEnabled(), SignupEnabled: a.cfg.HostSignupEnabled(),
	})
}

func (a *App) invitationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInviteUsed):
		a.errorPage(w, http.StatusGone, "That invitation already rang", "Ask the party host for a new one-time link.", "/", "Back home")
	case errors.Is(err, store.ErrInviteExpired):
		a.errorPage(w, http.StatusGone, "That invitation expired", "Ask the party host to make a fresh link.", "/", "Back home")
	case errors.Is(err, store.ErrNotFound):
		a.errorPage(w, http.StatusNotFound, "That invitation is not in service", "Check the whole link or ask the party host for a new one.", "/", "Back home")
	default:
		a.logger.Error("invitation operation", "error", err)
		a.errorPage(w, http.StatusInternalServerError, "The line went quiet", "RingRing hit a temporary problem. Please try again.", "/", "Back home")
	}
}

func (a *App) internalError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "method", r.Method, "path", safePath(r.URL.Path), "error", err)
	a.errorPage(w, http.StatusInternalServerError, "The line went quiet", "RingRing hit a temporary problem. Please try again.", "/", "Back home")
}

func (a *App) setInviteFlash(w http.ResponseWriter, partyID, token string) {
	value, err := a.cipher.Encrypt(partyID+"\n"+token, []byte("invite-flash"))
	if err != nil {
		a.logger.Error("encrypt invitation flash", "error", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: inviteFlashCookie, Value: value, Path: "/parties/" + partyID, MaxAge: 120,
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) readInviteFlash(w http.ResponseWriter, r *http.Request, partyID string) string {
	cookie, err := r.Cookie(inviteFlashCookie)
	if err != nil {
		return ""
	}
	a.clearCookie(w, inviteFlashCookie, "/parties/"+partyID)
	plaintext, err := a.cipher.Decrypt(cookie.Value, []byte("invite-flash"))
	if err != nil {
		return ""
	}
	flashPartyID, token, ok := strings.Cut(plaintext, "\n")
	if !ok || flashPartyID != partyID {
		return ""
	}
	return a.cfg.BaseURL + "/join/" + url.PathEscape(token)
}

func (a *App) setSetupFlash(w http.ResponseWriter, flash setupFlash) {
	encoded, err := json.Marshal(flash)
	if err != nil {
		a.logger.Error("encode setup flash", "error", err)
		return
	}
	value, err := a.cipher.Encrypt(string(encoded), []byte("setup-flash"))
	if err != nil {
		a.logger.Error("encrypt setup flash", "error", err)
		return
	}
	path := "/parties/" + flash.PartyID + "/setup"
	http.SetCookie(w, &http.Cookie{
		Name: "ringring_setup_reveal", Value: value, Path: path, MaxAge: 120,
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteStrictMode,
	})
}

func (a *App) readSetupFlash(w http.ResponseWriter, r *http.Request, partyID string) (setupFlash, error) {
	path := "/parties/" + partyID + "/setup"
	cookie, err := r.Cookie("ringring_setup_reveal")
	if err != nil {
		return setupFlash{}, err
	}
	a.clearCookie(w, "ringring_setup_reveal", path)
	plaintext, err := a.cipher.Decrypt(cookie.Value, []byte("setup-flash"))
	if err != nil {
		return setupFlash{}, err
	}
	var flash setupFlash
	if err := json.Unmarshal([]byte(plaintext), &flash); err != nil {
		return setupFlash{}, err
	}
	if flash.PartyID != partyID || flash.DeviceID == "" || flash.SIPUsername == "" || flash.SIPSecret == "" {
		return setupFlash{}, errors.New("invalid setup flash")
	}
	return flash, nil
}

func (a *App) setRecoveryFlash(w http.ResponseWriter, flash recoveryFlash) error {
	encoded, err := json.Marshal(flash)
	if err != nil {
		return fmt.Errorf("encode recovery reveal: %w", err)
	}
	value, err := a.cipher.Encrypt(string(encoded), []byte("recovery-flash"))
	if err != nil {
		return fmt.Errorf("encrypt recovery reveal: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name: recoveryFlashCookie, Value: value, Path: "/account/recovery-codes", MaxAge: 180,
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (a *App) readRecoveryFlash(w http.ResponseWriter, r *http.Request) (recoveryFlash, error) {
	cookie, err := r.Cookie(recoveryFlashCookie)
	if err != nil {
		return recoveryFlash{}, err
	}
	a.clearCookie(w, recoveryFlashCookie, "/account/recovery-codes")
	plaintext, err := a.cipher.Decrypt(cookie.Value, []byte("recovery-flash"))
	if err != nil {
		return recoveryFlash{}, err
	}
	var flash recoveryFlash
	if err := json.Unmarshal([]byte(plaintext), &flash); err != nil {
		return recoveryFlash{}, err
	}
	if (flash.Kind != "signup" && flash.Kind != "reset") || flash.Username == "" || len(flash.Codes) != 8 {
		return recoveryFlash{}, errors.New("invalid recovery reveal")
	}
	for _, code := range flash.Codes {
		if len(localauth.NormalizeRecoveryCode(code)) != 26 {
			return recoveryFlash{}, errors.New("invalid recovery code in reveal")
		}
	}
	return flash, nil
}

func (a *App) setShortCookie(w http.ResponseWriter, name, value, path string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: path, MaxAge: 10 * 60,
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: path, MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: a.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) secureCookies() bool {
	return strings.HasPrefix(a.cfg.BaseURL, "https://")
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'none'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if a.cfg.Environment == "production" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, r)
		a.logger.Info("request", "method", r.Method, "path", safePath(r.URL.Path), "status", writer.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (a *App) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("request panic", "path", safePath(r.URL.Path), "panic", recovered)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func safePath(path string) string {
	if strings.HasPrefix(path, "/join/") {
		return "/join/:token"
	}
	return path
}

func slugify(value string) string {
	var result strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) && r <= unicode.MaxASCII, unicode.IsDigit(r):
			result.WriteRune(r)
			lastHyphen = false
		case result.Len() > 0 && !lastHyphen:
			result.WriteByte('-')
			lastHyphen = true
		}
	}
	slug := strings.Trim(result.String(), "-")
	if slug == "" {
		return "party"
	}
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return slug
}

func validExtension(extension string) bool {
	if !extensionPattern.MatchString(extension) {
		return false
	}
	switch extension {
	case "911", "112", "999":
		return false
	default:
		return true
	}
}

type googleUserInfo struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func googleProfile(ctx context.Context, client *http.Client) (googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return googleUserInfo{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return googleUserInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return googleUserInfo{}, fmt.Errorf("userinfo returned %s", response.Status)
	}
	var profile googleUserInfo
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&profile); err != nil {
		return googleUserInfo{}, err
	}
	if profile.Subject == "" || profile.Email == "" {
		return googleUserInfo{}, errors.New("userinfo omitted required identity fields")
	}
	return profile, nil
}
