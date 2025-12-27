package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Session represents a user session
type Session struct {
	ID        string
	Username  string
	Token     string // GitHub access token
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionStore manages user sessions
type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	ttl      time.Duration
}

// NewSessionStore creates a new session store
func NewSessionStore(ttl time.Duration) *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}

	// Start cleanup goroutine
	go store.cleanup()

	return store
}

// Create creates a new session
func (s *SessionStore) Create(username, token string) (*Session, error) {
	id, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	session := &Session{
		ID:        id,
		Username:  username,
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
	}

	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()

	return session, nil
}

// Get retrieves a session by ID
func (s *SessionStore) Get(id string) *Session {
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	// Check if expired
	if time.Now().After(session.ExpiresAt) {
		s.Delete(id)
		return nil
	}

	return session
}

// Delete removes a session
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Refresh extends the session expiry
func (s *SessionStore) Refresh(id string) {
	s.mu.Lock()
	if session, ok := s.sessions[id]; ok {
		session.ExpiresAt = time.Now().Add(s.ttl)
	}
	s.mu.Unlock()
}

// cleanup periodically removes expired sessions
func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

// generateSessionID creates a cryptographically secure session ID
func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
