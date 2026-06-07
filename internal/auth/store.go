// Package auth manages LinkedIn session credentials for the lnk CLI.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configDir = ".config/lnk"
	credsFile = "credentials.json"
	envLiAt   = "LNK_LI_AT"
	envCSRF   = "LNK_CSRF_TOKEN"
)

// Credentials holds the LinkedIn session tokens required for API calls.
type Credentials struct {
	// LiAt is the primary session cookie value (li_at).
	LiAt string `json:"li_at"`
	// CSRFToken is the ajax:<value> token extracted from JSESSIONID.
	CSRFToken string `json:"csrf_token"`
	// BCookie is the browser identifier cookie value.
	BCookie string `json:"bcookie,omitempty"`
}

// Store persists and retrieves LinkedIn credentials.
type Store struct {
	path string
}

// Default returns a Store backed by ~/.config/lnk/credentials.json.
func Default() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("auth: cannot determine home directory: %w", err)
	}
	return &Store{path: filepath.Join(home, configDir, credsFile)}, nil
}

// NewStore creates a Store that reads/writes to path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Save persists creds to disk with 0600 permissions.
func (s *Store) Save(creds Credentials) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("auth: cannot create config directory: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: cannot marshal credentials: %w", err)
	}
	// Write to temp file then rename for atomicity.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("auth: cannot write credentials: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("auth: cannot rename credentials file: %w", err)
	}
	return nil
}

// Load reads credentials, applying env-var overrides.
// If the credentials file does not exist, returns empty Credentials (not an error).
func (s *Store) Load() (Credentials, error) {
	var creds Credentials

	data, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return creds, fmt.Errorf("auth: cannot read credentials: %w", err)
	}
	if err == nil {
		if jsonErr := json.Unmarshal(data, &creds); jsonErr != nil {
			return creds, fmt.Errorf("auth: cannot parse credentials: %w", jsonErr)
		}
	}

	// Env-var overrides take precedence.
	if v := os.Getenv(envLiAt); v != "" {
		creds.LiAt = v
	}
	if v := os.Getenv(envCSRF); v != "" {
		creds.CSRFToken = v
	}

	return creds, nil
}

// Clear removes the credentials file.
func (s *Store) Clear() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("auth: cannot remove credentials: %w", err)
	}
	return nil
}

// IsLoggedIn returns true when the minimum required tokens are present.
func (s *Store) IsLoggedIn() bool {
	creds, err := s.Load()
	if err != nil {
		return false
	}
	return creds.LiAt != "" && creds.CSRFToken != ""
}

// Path returns the filesystem path of the credentials file.
func (s *Store) Path() string { return s.path }
