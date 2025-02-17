package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// HashSpec returns a stable sha256 hex digest of the canonical JSON encoding of
// v. json.Marshal emits map keys in sorted order, so this is deterministic for
// equal values and lets plan/diff detect drift cheaply.
func HashSpec(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// strVal normalizes a scanned value (string, []byte or nil) into a plain
// string so callers can compare against "" for nullable timestamp columns.
func strVal(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

// parseTime accepts an RFC3339Nano string or []byte and returns the parsed time.
func parseTime(v any) (time.Time, error) {
	var s string
	switch t := v.(type) {
	case nil:
		return time.Time{}, fmt.Errorf("nil timestamp")
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp type %T", v)
	}
	// Try SQLite datetime format first: "2006-01-02 15:04:05"
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp %q", s)
}

// nowUTC returns the current UTC time. Extracted to a function so tests can
// stub it if needed.
func nowUTC() time.Time {
	return time.Now().UTC()
}

// timeScan is a helper for scanning nullable SQLite timestamps into *time.Time.
type timeScan struct {
	t *time.Time
}

func (ts *timeScan) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*ts.t = time.Time{}
		return nil
	case time.Time:
		*ts.t = v.UTC()
		return nil
	case string:
		parsed, err := parseTime(v)
		if err != nil {
			return err
		}
		*ts.t = parsed
		return nil
	case []byte:
		parsed, err := parseTime(string(v))
		if err != nil {
			return err
		}
		*ts.t = parsed
		return nil
	default:
		return fmt.Errorf("unsupported timestamp type %T", value)
	}
}

// openTestStore opens a temporary SQLite store for tests.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := New(path)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
