package colab

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session represents a live Colab runtime allocation.
type Session struct {
	Name              string      `json:"name"`
	Endpoint          string      `json:"endpoint"`
	URL               string      `json:"url"`
	Token             string      `json:"token"`
	TokenExp          time.Time   `json:"token_expires_at"`
	Shape             Shape       `json:"shape"`
	Accel             Accelerator `json:"accelerator"`
	CreatedAt         time.Time   `json:"created_at"`
	KernelID          string      `json:"kernel_id,omitempty"`
	KernelSessionID   string      `json:"kernel_session_id,omitempty"`
	LastExecutionAt   time.Time   `json:"last_execution_at,omitempty"`
	LastExecutionFile string      `json:"last_execution_file,omitempty"`
	LastExecutionCell int         `json:"last_execution_cell,omitempty"`
}

// IsTokenValid reports whether the session token is still valid.
func (s *Session) IsTokenValid() bool {
	return time.Now().Before(s.TokenExp)
}

// sessionsFile is the on-disk format.
type sessionsFile struct {
	Sessions map[string]*Session `json:"sessions"`
}

// SessionStore persists sessions to a JSON file.
// Safe for concurrent use within one process.
type SessionStore struct {
	Path string // default ~/.config/go-colab/sessions.json
	mu   sync.Mutex
}

// DefaultSessionStorePath returns the default path for the session store.
func DefaultSessionStorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "go-colab", "sessions.json")
}

// NewSessionStore creates a SessionStore at the given path.
// If path is empty, uses DefaultSessionStorePath().
func NewSessionStore(path string) *SessionStore {
	if path == "" {
		path = DefaultSessionStorePath()
	}
	return &SessionStore{Path: path}
}

// Load reads all sessions from disk. Returns empty map if file absent.
func (s *SessionStore) Load() (map[string]*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *SessionStore) load() (map[string]*Session, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]*Session), nil
	}
	if err != nil {
		return nil, fmt.Errorf("colab: read sessions: %w", err)
	}
	var f sessionsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("colab: parse sessions: %w", err)
	}
	if f.Sessions == nil {
		f.Sessions = make(map[string]*Session)
	}
	return f.Sessions, nil
}

// Save writes sessions map to disk atomically (write to temp, rename).
func (s *SessionStore) Save(sessions map[string]*Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(sessions)
}

func (s *SessionStore) save(sessions map[string]*Session) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	f := sessionsFile{Sessions: sessions}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: temp file + rename
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Add inserts or replaces a session by name.
func (s *SessionStore) Add(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions, err := s.load()
	if err != nil {
		return err
	}
	sessions[session.Name] = session
	return s.save(sessions)
}

// Remove deletes a session by name. No-op if not found.
func (s *SessionStore) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sessions, err := s.load()
	if err != nil {
		return err
	}
	delete(sessions, name)
	return s.save(sessions)
}

// Get returns a session by name. Returns nil, nil if not found.
func (s *SessionStore) Get(name string) (*Session, error) {
	sessions, err := s.Load()
	if err != nil {
		return nil, err
	}
	return sessions[name], nil
}

// Active returns the single active session, or an error if there are zero or more than one.
// This mirrors the Python CLI behaviour: when only one session exists, commands can omit -s.
func (s *SessionStore) Active() (*Session, error) {
	sessions, err := s.Load()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("colab: no active sessions")
	}
	if len(sessions) > 1 {
		return nil, fmt.Errorf("colab: multiple sessions active, specify session name with -s")
	}
	for _, sess := range sessions {
		return sess, nil
	}
	return nil, fmt.Errorf("colab: no active sessions")
}

// Resolve returns the named session, or falls back to Active() if name is empty.
func (s *SessionStore) Resolve(name string) (*Session, error) {
	if name != "" {
		sess, err := s.Get(name)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, fmt.Errorf("colab: session %q not found", name)
		}
		return sess, nil
	}
	return s.Active()
}
