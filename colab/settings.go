package colab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// APIMode selects the Colab runtime API.
type APIMode string

const (
	// APIModeTunnel uses the Colab tunnel API for full runtime lifecycle support.
	APIModeTunnel APIMode = "tunnel"
	// APIModePublic uses the public API, which supports runtime listing only.
	APIModePublic APIMode = "public"
)

// Settings stores persisted client capability and API mode preferences.
type Settings struct {
	UsePublicAPI        bool   `json:"use_public_api"`
	PublicAPISupported  bool   `json:"public_api_supported"`
	TunnelAPISupported  bool   `json:"tunnel_api_supported"`
	EnableUpdateCheck   bool   `json:"enable_update_check"`
	LastUpdateCheckUnix int64  `json:"last_update_check_unix"`
	LatestVersion       string `json:"latest_version"`
}

// SettingsStore persists Settings in an owner-only JSON file.
type SettingsStore struct {
	Path string
	mu   sync.Mutex
}

// DefaultSettingsStorePath returns the default settings path.
func DefaultSettingsStorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "go-colab", "settings.json")
}

// NewSettingsStore creates a settings store. An empty path uses the default path.
func NewSettingsStore(path string) *SettingsStore {
	if path == "" {
		path = DefaultSettingsStorePath()
	}
	return &SettingsStore{Path: path}
}

// Load returns saved settings. Missing or malformed files return defaults.
func (s *SettingsStore) Load() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	contents, err := os.ReadFile(s.Path)
	if err != nil {
		return defaultSettings()
	}
	settings := defaultSettings()
	if json.Unmarshal(contents, &settings) != nil {
		return defaultSettings()
	}
	return settings
}

// Save atomically persists settings with owner-only permissions.
func (s *SettingsStore) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.Path), ".settings-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.Path)
}

func defaultSettings() Settings {
	return Settings{EnableUpdateCheck: true}
}
