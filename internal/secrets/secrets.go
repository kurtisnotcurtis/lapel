// Package secrets is a small file-backed secret store used to hold values that
// must not live in the manifest itself — primarily provider API keys referenced
// by name (manifest.Provider.APIKeySecret). Secrets are stored as a JSON map of
// name -> value on disk with 0600 permissions.
package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Store is a file-backed secret store. It is safe for the single-writer CLI use
// case; concurrent writes are not serialized beyond the OS rename atomicity used
// on each Set.
type Store struct {
	path string
	m    map[string]string
}

// Open loads an existing secrets file at path (or starts empty if it does not
// exist). The containing directory is created if necessary.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating secrets dir: %w", err)
	}
	s := &Store{path: path, m: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("reading secrets %s: %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.m); err != nil {
		return nil, fmt.Errorf("parsing secrets %s: %w", path, err)
	}
	return s, nil
}

// Get returns the stored value for name and whether it was found.
func (s *Store) Get(name string) (string, bool) {
	v, ok := s.m[name]
	return v, ok
}

// Set stores value under name.
func (s *Store) Set(name, value string) error {
	s.m[name] = value
	return s.flush()
}

// Delete removes a secret if present.
func (s *Store) Delete(name string) error {
	if _, ok := s.m[name]; !ok {
		return nil
	}
	delete(s.m, name)
	return s.flush()
}

// Keys returns the sorted list of stored secret names.
func (s *Store) Keys() []string {
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of stored secrets.
func (s *Store) Len() int { return len(s.m) }

// Flush writes the current state to disk atomically with 0600 permissions.
func (s *Store) flush() error {
	data, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding secrets: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing temp secrets: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("renaming secrets into place: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("secrets file permissions: %w", err)
	}
	return nil
}

// GenerateToken returns a cryptographically-random hex token of n bytes. It is
// used to mint ephemeral secrets (e.g. per-session tokens) when needed.
func GenerateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
