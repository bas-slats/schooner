package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"schooner/internal/auth"
	"schooner/internal/config"
	"schooner/internal/database/queries"
	"schooner/internal/github"
)

// OAuthHandler handles GitHub OAuth flow
type OAuthHandler struct {
	cfg             *config.Config
	settingsQueries *queries.SettingsQueries
	githubClient    *github.Client
	sessionStore    *auth.SessionStore
}

// NewOAuthHandler creates a new OAuthHandler
func NewOAuthHandler(cfg *config.Config, settingsQueries *queries.SettingsQueries, githubClient *github.Client, sessionStore *auth.SessionStore) *OAuthHandler {
	return &OAuthHandler{
		cfg:             cfg,
		settingsQueries: settingsQueries,
		githubClient:    githubClient,
		sessionStore:    sessionStore,
	}
}

// generateState creates a random state string for OAuth
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Login handles GET /oauth/github/login - redirects to GitHub OAuth
func (h *OAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.cfg.GitHubOAuth.ClientID == "" {
		http.Error(w, "GitHub OAuth not configured. Please set github_oauth.client_id and github_oauth.client_secret in config.", http.StatusBadRequest)
		return
	}

	state, err := generateState()
	if err != nil {
		slog.Error("failed to generate state", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Store state in a cookie for verification
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Build GitHub OAuth URL
	params := url.Values{
		"client_id":    {h.cfg.GitHubOAuth.ClientID},
		"redirect_uri": {h.cfg.Server.BaseURL + "/oauth/github/callback"},
		"scope":        {"repo read:user"},
		"state":        {state},
	}

	authURL := "https://github.com/login/oauth/authorize?" + params.Encode()
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// Callback handles GET /oauth/github/callback - exchanges code for token
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Verify state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	state := r.URL.Query().Get("state")
	if state != stateCookie.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// Clear the state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Check for errors from GitHub
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		errDesc := r.URL.Query().Get("error_description")
		slog.Error("GitHub OAuth error", "error", errMsg, "description", errDesc)
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(errDesc), http.StatusTemporaryRedirect)
		return
	}

	// Get the authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	tokenResp, err := h.exchangeCodeForToken(code)
	if err != nil {
		slog.Error("failed to exchange code for token", "error", err)
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Failed to authenticate with GitHub"), http.StatusTemporaryRedirect)
		return
	}

	// Validate the token by getting the user
	h.githubClient.SetToken(tokenResp.AccessToken)
	username, err := h.githubClient.GetUser(ctx)
	if err != nil {
		slog.Error("failed to get GitHub user", "error", err)
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Failed to verify GitHub token"), http.StatusTemporaryRedirect)
		return
	}

	// Save the token to settings (for API access)
	if err := h.settingsQueries.Set(ctx, "github_token", tokenResp.AccessToken); err != nil {
		slog.Error("failed to save GitHub token", "error", err)
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Failed to save token"), http.StatusTemporaryRedirect)
		return
	}

	// Create session for the user
	session, err := h.sessionStore.Create(username, tokenResp.AccessToken)
	if err != nil {
		slog.Error("failed to create session", "error", err)
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Failed to create session"), http.StatusTemporaryRedirect)
		return
	}

	// Set session cookie (24 hours)
	auth.SetSessionCookie(w, session.ID, 86400)

	slog.Info("GitHub OAuth completed", "username", username)

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func (h *OAuthHandler) exchangeCodeForToken(code string) (*tokenResponse, error) {
	data := url.Values{
		"client_id":     {h.cfg.GitHubOAuth.ClientID},
		"client_secret": {h.cfg.GitHubOAuth.ClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access token in response: %s", string(body))
	}

	return &tokenResp, nil
}

// Status handles GET /oauth/github/status - returns OAuth configuration status
func (h *OAuthHandler) Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"oauth_configured": h.cfg.GitHubOAuth.ClientID != "" && h.cfg.GitHubOAuth.ClientSecret != "",
	})
}

// Logout handles GET /logout - logs out the user
func (h *OAuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Get session from cookie
	cookie, err := r.Cookie(auth.CookieName)
	if err == nil {
		// Delete session
		h.sessionStore.Delete(cookie.Value)
	}

	// Clear cookie
	auth.ClearSessionCookie(w)

	slog.Info("user logged out")

	// Redirect to login
	http.Redirect(w, r, "/oauth/github/login", http.StatusTemporaryRedirect)
}
