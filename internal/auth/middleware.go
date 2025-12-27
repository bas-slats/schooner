package auth

import (
	"context"
	"net/http"
)

// ContextKey is a custom type for context keys
type ContextKey string

const (
	// SessionKey is the context key for the session
	SessionKey ContextKey = "session"
	// CookieName is the name of the session cookie
	CookieName = "schooner_session"
)

// Middleware provides authentication middleware
type Middleware struct {
	store        *SessionStore
	loginURL     string
	publicPaths  map[string]bool
	publicPrefix []string
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(store *SessionStore, loginURL string) *Middleware {
	return &Middleware{
		store:    store,
		loginURL: loginURL,
		publicPaths: map[string]bool{
			"/health":               true,
			"/oauth/github/login":   true,
			"/oauth/github/callback": true,
			"/oauth/github/status":  true,
		},
		publicPrefix: []string{
			"/webhook/",
			"/static/",
		},
	}
}

// RequireAuth returns middleware that requires authentication
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path is public
		if m.isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Get session from cookie
		cookie, err := r.Cookie(CookieName)
		if err != nil {
			m.redirectToLogin(w, r)
			return
		}

		// Validate session
		session := m.store.Get(cookie.Value)
		if session == nil {
			// Clear invalid cookie
			http.SetCookie(w, &http.Cookie{
				Name:     CookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
			})
			m.redirectToLogin(w, r)
			return
		}

		// Refresh session on each request
		m.store.Refresh(session.ID)

		// Add session to context
		ctx := context.WithValue(r.Context(), SessionKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath checks if a path is public
func (m *Middleware) isPublicPath(path string) bool {
	// Check exact matches
	if m.publicPaths[path] {
		return true
	}

	// Check prefix matches
	for _, prefix := range m.publicPrefix {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

// redirectToLogin redirects to the login page
func (m *Middleware) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	// For API requests, return 401
	if isAPIRequest(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// For browser requests, redirect to login
	http.Redirect(w, r, m.loginURL, http.StatusTemporaryRedirect)
}

// isAPIRequest checks if the request is an API request
func isAPIRequest(r *http.Request) bool {
	// Check if path starts with /api/
	if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
		return true
	}

	// Check Accept header
	accept := r.Header.Get("Accept")
	return accept == "application/json"
}

// GetSession retrieves the session from context
func GetSession(ctx context.Context) *Session {
	session, ok := ctx.Value(SessionKey).(*Session)
	if !ok {
		return nil
	}
	return session
}

// SetSessionCookie sets the session cookie
func SetSessionCookie(w http.ResponseWriter, sessionID string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// ClearSessionCookie clears the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}
