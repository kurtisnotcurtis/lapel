// Package manifest provides parsing, validation, and serialization of the
// Lapel manifest (lapel.yaml). It turns a YAML file on disk into
// a validated domain.Manifest ready for reconcile.
package manifest

import (
	"fmt"
	"os"

	"lapel/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	// CurrentSchemaVersion is the manifest format version this parser supports.
	CurrentSchemaVersion = 1

	// DefaultFileName is the conventional manifest filename in the project root.
	DefaultFileName = "lapel.yaml"
)

// ValidationError reports a single schema violation with its field path and message.
type ValidationError struct {
	Field   string // dotted path, e.g. "distro.base"
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ParseFile reads and validates a YAML manifest from disk. Returns the parsed
// Manifest with defaults filled in where fields were omitted.
func ParseFile(path string) (*domain.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return Parse(data)
}

// Parse parses YAML bytes into a validated Manifest with defaults applied.
func Parse(data []byte) (*domain.Manifest, error) {
	m := &domain.Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Distro:        domain.DistroConfig{},
		Packages:      domain.PackageConfig{},
		Jail:          domain.JailManifest{},
		Env:           make(map[string]string),
	}

	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	// Apply defaults for fields that need them.
	applyDefaults(m)

	if errs := Validate(m); len(errs) > 0 {
		return nil, &ValidationErrorList{Errors: errs}
	}

	return m, nil
}

// Write serializes a Manifest back to YAML bytes. Useful for apply (write
// the canonical manifest after reconciling).
func Write(m *domain.Manifest) ([]byte, error) {
	return yaml.Marshal(m)
}

// Validate checks a Manifest against schema rules. Returns all violations;
// callers typically render them as a single multi-line error.
func Validate(m *domain.Manifest) []error {
	var errs []error

	if m.SchemaVersion != CurrentSchemaVersion {
		errs = append(errs, &ValidationError{Field: "schemaVersion", Message: fmt.Sprintf("expected %d, got %d", CurrentSchemaVersion, m.SchemaVersion)})
	}

	if m.Distro.Name == "" {
		errs = append(errs, &ValidationError{Field: "distro.name", Message: "required"})
	} else if len(m.Distro.Name) > 64 {
		errs = append(errs, &ValidationError{Field: "distro.name", Message: "must be <= 64 characters"})
	}

	if m.Distro.Base != "" && m.Distro.Base != "ubuntu" && m.Distro.Base != "debian" {
		errs = append(errs, &ValidationError{Field: "distro.base", Message: fmt.Sprintf("must be 'ubuntu' or 'debian', got %q", m.Distro.Base)})
	}

	if m.Jail.Network != "" && m.Jail.Network != "unrestricted" && m.Jail.Network != "outbound-only" && m.Jail.Network != "none" {
		errs = append(errs, &ValidationError{Field: "jail.network", Message: fmt.Sprintf("must be 'unrestricted', 'outbound-only', or 'none', got %q", m.Jail.Network)})
	}

	for path, mode := range m.Jail.Filesystem {
		if mode != "read-write" && mode != "read-only" && mode != "deny" {
			errs = append(errs, &ValidationError{Field: fmt.Sprintf("jail.filesystem[%q]", path), Message: fmt.Sprintf("must be 'read-write', 'read-only', or 'deny', got %q", mode)})
		}
	}

	if m.Jail.Processes.TimeoutSeconds < 0 {
		errs = append(errs, &ValidationError{Field: "jail.processes.timeoutSeconds", Message: "must be non-negative"})
	}

	return errs
}

// ValidationErrorList groups multiple validation errors into a single error.
type ValidationErrorList struct {
	Errors []error
}

func (l *ValidationErrorList) Error() string {
	if len(l.Errors) == 0 {
		return "validation passed"
	}
	msg := fmt.Sprintf("%d validation error(s):", len(l.Errors))
	for _, e := range l.Errors {
		msg += "\n  - " + e.Error()
	}
	return msg
}

// applyDefaults fills in sensible defaults for omitted fields.
func applyDefaults(m *domain.Manifest) {
	if m.Distro.Systemd == nil {
		systemd := true
		m.Distro.Systemd = &systemd
	}
}
