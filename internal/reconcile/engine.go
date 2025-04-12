// Package reconcile implements the core logic for comparing desired state
// (manifest) against actual state (SQLite store) and generating plans to
// correct drift. It's the brain of declarative management — without it, the
// manifest is just a config file and the state store is just a database.
package reconcile

import (
	"fmt"
	"strings"
	"time"

	"lapel/internal/manifest"
	"lapel/internal/state"
)

// Reconciler orchestrates drift detection and correction between manifest
// and state. It's intentionally thin — the actual WSL execution happens in
// the backend; this package just decides what needs to change and how.
type Reconciler struct {
	store *state.Store

	// MaxRetries is the maximum number of retry attempts for transient failures.
	// Default: 3
	MaxRetries int

	// RetryDelay is the base delay between retries (exponential backoff).
	// Default: 100ms
	RetryDelay time.Duration
}

// New creates a reconciler bound to the given state store with sensible defaults.
func New(store *state.Store) *Reconciler {
	return &Reconciler{
		store:      store,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}
}

// DriftEntry represents a single difference between desired and actual state.
type DriftEntry struct {
	Kind      string // package, jail_rule, env_var, distro
	Key       string // name or path
	Action    string // add, remove, update
	From      string // current value (empty if adding)
	To        string // desired value (empty if removing)
	Detected  time.Time
}

// Plan represents a dry-run of what reconcile would do. It's the output of
// Plan() and is used by `lapel plan` to show drift without applying it.
type Plan struct {
	Distro   string
	Drift    []DriftEntry
	Created  time.Time
}

// ApplyResult represents what actually happened during apply().
type ApplyResult struct {
	Distro   string
	Drift    []DriftEntry
	Applied  int // number of entries applied
	Failures []string
	Time     time.Time
}

// Plan computes the diff between manifest and state without making changes.
func (r *Reconciler) Plan(manifestPath, distroName string) (*Plan, error) {
	m, err := manifest.ParseFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	var drift []DriftEntry

	// Check packages
	actualPkgs, _ := r.store.GetPackages(distroName)
	// Combine baseline and extras for the full desired package list
	var desiredPkgs []string
	desiredPkgs = append(desiredPkgs, m.Packages.Baseline...)
	desiredPkgs = append(desiredPkgs, m.Packages.Extras...)

	for _, desired := range desiredPkgs {
		found := false
		for _, actual := range actualPkgs {
			if actual.Name == desired {
				found = true
				break
			}
		}
		if !found {
			drift = append(drift, DriftEntry{
				Kind:   "package",
				Key:    desired,
				Action: "add",
				Detected: time.Now(),
			})
		}
	}

	for _, actual := range actualPkgs {
		found := false
		for _, desired := range desiredPkgs {
			if actual.Name == desired {
				found = true
				break
			}
		}
		if !found && actual.Source == "manifest" {
			drift = append(drift, DriftEntry{
				Kind:   "package",
				Key:    actual.Name,
				Action: "remove",
				Detected: time.Now(),
			})
		}
	}

	// Check jail rules
	actualRules, _ := r.store.GetJailRules(distroName)
	for path, mode := range m.Jail.Filesystem {
		found := false
		for _, actual := range actualRules {
			if actual.Type == "filesystem" && actual.Key == path {
				found = true
				if actual.Value != string(mode) {
					drift = append(drift, DriftEntry{
						Kind:   "jail_rule",
						Key:    path,
						Action: "update",
						From:   actual.Value,
						To:     string(mode),
						Detected: time.Now(),
					})
				}
				break
			}
		}
		if !found {
			drift = append(drift, DriftEntry{
				Kind:   "jail_rule",
				Key:    path,
				Action: "add",
				To:     string(mode),
				Detected: time.Now(),
			})
		}
	}

	// Check env vars
	actualEnv, _ := r.store.GetEnvVars(distroName)
	for key, value := range m.Env {
		found := false
		for _, actual := range actualEnv {
			if actual.Key == key {
				found = true
				if actual.Value != value {
					drift = append(drift, DriftEntry{
						Kind:   "env_var",
						Key:    key,
						Action: "update",
						From:   actual.Value,
						To:     value,
						Detected: time.Now(),
					})
				}
				break
			}
		}
		if !found {
			drift = append(drift, DriftEntry{
				Kind:   "env_var",
				Key:    key,
				Action: "add",
				To:     value,
				Detected: time.Now(),
			})
		}
	}

	return &Plan{
		Distro:  distroName,
		Drift:   drift,
		Created: time.Now(),
	}, nil
}

// ApplyOption configures apply behavior. Currently only RollbackOnFailure is supported.
type ApplyOption int

const (
	ApplyDefault ApplyOption = 0 // default behavior
	RollbackOnFailure ApplyOption = 1 << iota // auto-rollback on partial failure
)

// Apply executes the plan and updates state. Returns what was applied and any failures.
// It uses exponential backoff retry for transient failures and rolls back on critical errors.
func (r *Reconciler) Apply(manifestPath, distroName string) (*ApplyResult, error) {
	return r.ApplyWithOptions(manifestPath, distroName, ApplyDefault)
}

// ApplyWithOptions executes the plan with options. Returns what was applied and any failures.
func (r *Reconciler) ApplyWithOptions(manifestPath, distroName string, opts ApplyOption) (*ApplyResult, error) {
	plan, err := r.Plan(manifestPath, distroName)
	if err != nil {
		return nil, fmt.Errorf("compute plan: %w", err)
	}

	// Save baseline state for rollback
	pkgsBefore, _ := r.store.GetPackages(distroName)
	rulesBefore, _ := r.store.GetJailRules(distroName)
	envBefore, _ := r.store.GetEnvVars(distroName)
	_ = pkgsBefore
	_ = rulesBefore

	// Track apply errors separately from plan error
	var applyErr error
	_ = envBefore

	var failures []string
	applied := 0

	// Begin transaction for atomicity - if anything fails, we can roll back all changes
	tx, beginErr := r.store.BeginTx()
	if beginErr != nil {
		return nil, fmt.Errorf("begin transaction: %w", beginErr)
	}
	defer func() {
		if applyErr != nil || len(failures) > 0 {
			_ = tx.Rollback() // Best-effort rollback on error
		}
	}()

	for _, d := range plan.Drift {
		switch d.Kind {
		case "package":
			applyErr = r.RetryApply(func() error { return r.applyPackageTx(tx, distroName, d) })
		case "jail_rule":
			applyErr = r.RetryApply(func() error { return r.applyJailRuleTx(tx, distroName, d) })
		case "env_var":
			applyErr = r.RetryApply(func() error { return r.applyEnvVarTx(tx, distroName, d) })
		}

		if applyErr != nil {
			failures = append(failures, fmt.Sprintf("%s %s: %v", d.Kind, d.Key, applyErr))
		} else {
			applied++
		}
	}

	// Auto-rollback on partial failure if option is set
	if opts&RollbackOnFailure != 0 && len(failures) > 0 {
		fmt.Printf("[WARN] %d/%d drift entries failed, rolling back...\n", len(failures), len(plan.Drift))
		// Restore baseline state
		rollbackErr := r.Rollback(distroName, pkgsBefore, rulesBefore, envBefore)
		if rollbackErr != nil {
			failures = append(failures, fmt.Sprintf("rollback failed: %v", rollbackErr))
		} else {
			failures = append(failures, fmt.Sprintf("rolled back to baseline state"))
		}
	}

	// Commit the transaction if we haven't already rolled back
	if applyErr == nil && len(failures) == 0 {
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit transaction: %w", commitErr)
		}
	} else if applyErr == nil && len(failures) > 0 {
		// We have failures but no error - rollback was already handled above
		_ = tx.Rollback() // Explicitly close the transaction
	}

	result := &ApplyResult{
		Distro:   distroName,
		Drift:    plan.Drift,
		Applied:  applied,
		Failures: failures,
		Time:     time.Now(),
	}

	if err := r.store.LogApply(distroName, "apply", fmt.Sprintf("applied %d/%d changes", applied, len(plan.Drift))); err != nil {
		failures = append(failures, fmt.Sprintf("log apply: %v", err))
	}

	return result, nil
}

// RetryApply executes fn with exponential backoff retry. Exported for testing and advanced use cases.
func (r *Reconciler) RetryApply(fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= r.MaxRetries; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if attempt < r.MaxRetries {
				delay := r.RetryDelay * time.Duration(1<<uint(attempt)) // exponential: 1x, 2x, 4x...
				time.Sleep(delay)
			}
		} else {
			return nil
		}
	}
	return lastErr
}

// Status returns the current drift without making changes. Alias for Plan().
func (r *Reconciler) Status(manifestPath, distroName string) (*Plan, error) {
	return r.Plan(manifestPath, distroName)
}

// Rollback restores state to the baseline captured before Apply.
// Returns the number of items restored and any errors encountered.
func (r *Reconciler) Rollback(distroName string, pkgsBefore []state.PackageRecord, rulesBefore []state.JailRule, envBefore []state.EnvVar) error {
	// Clear current state
	r.store.DeletePackages(distroName)
	r.store.DeleteJailRules(distroName)
	r.store.DeleteEnvVars(distroName)

	// Restore packages
	for _, pkg := range pkgsBefore {
		if err := r.store.UpsertPackage(pkg); err != nil {
			return fmt.Errorf("rollback package %s: %w", pkg.Name, err)
		}
	}

	// Restore jail rules
	for _, rule := range rulesBefore {
		if err := r.store.UpsertJailRule(rule); err != nil {
			return fmt.Errorf("rollback jail_rule %s: %w", rule.Key, err)
		}
	}

	// Restore env vars
	for _, env := range envBefore {
		if err := r.store.UpsertEnvVar(env); err != nil {
			return fmt.Errorf("rollback env_var %s: %w", env.Key, err)
		}
	}

	return nil
}

// applyPackage handles package add/remove/update logic.
func (r *Reconciler) applyPackage(distroName string, d DriftEntry) error {
	// In a full implementation, this would:
	// 1. Run apt-get install/remove in WSL
	// 2. Update the state store with the result
	// For now, we just update the state store to mark it as applied
	switch d.Action {
	case "add":
		return r.store.UpsertPackage(state.PackageRecord{
			Distro: distroName,
			Name:   d.Key,
			Source: "manifest",
		})
	case "remove":
		// Actually remove the package from the store
		return r.store.DeletePackage(distroName, d.Key)
	default:
		return fmt.Errorf("unsupported package action: %s", d.Action)
	}
}

// applyJailRule handles jail rule add/update logic.
func (r *Reconciler) applyJailRule(distroName string, d DriftEntry) error {
	rule := state.JailRule{
		Distro: distroName,
		Type:   "filesystem",
		Key:    d.Key,
		Value:  d.To,
	}

	if d.Action == "update" {
		rule.Value = d.To
	}

	return r.store.UpsertJailRule(rule)
}

// applyEnvVar handles env var add/update logic.
func (r *Reconciler) applyEnvVar(distroName string, d DriftEntry) error {
	envVar := state.EnvVar{
		Distro: distroName,
		Key:    d.Key,
		Value:  d.To,
	}

	if d.Action == "update" {
		envVar.Value = d.To
	}

	return r.store.UpsertEnvVar(envVar)
}

// applyPackageTx handles package operations within a transaction.
func (r *Reconciler) applyPackageTx(tx *state.Tx, distroName string, d DriftEntry) error {
	switch d.Action {
	case "add":
		return r.store.UpsertPackageTx(tx.Tx, state.PackageRecord{
			Distro: distroName,
			Name:   d.Key,
			Source: "manifest",
		})
	case "remove":
		return r.store.DeletePackageTx(tx.Tx, distroName, d.Key)
	default:
		return fmt.Errorf("unsupported package action: %s", d.Action)
	}
}

// applyJailRuleTx handles jail rule operations within a transaction.
func (r *Reconciler) applyJailRuleTx(tx *state.Tx, distroName string, d DriftEntry) error {
	rule := state.JailRule{
		Distro: distroName,
		Type:   "filesystem",
		Key:    d.Key,
		Value:  d.To,
	}

	if d.Action == "update" {
		rule.Value = d.To
	}

	return r.store.UpsertJailRuleTx(tx.Tx, rule)
}

// applyEnvVarTx handles env var operations within a transaction.
func (r *Reconciler) applyEnvVarTx(tx *state.Tx, distroName string, d DriftEntry) error {
	envVar := state.EnvVar{
		Distro: distroName,
		Key:    d.Key,
		Value:  d.To,
	}

	if d.Action == "update" {
		envVar.Value = d.To
	}

	return r.store.UpsertEnvVarTx(tx.Tx, envVar)
}

// FormatPlan renders a plan as human-readable text for CLI output.
func FormatPlan(p *Plan) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Reconcile plan for %s (%d drift entries):\n\n", p.Distro, len(p.Drift)))

	if len(p.Drift) == 0 {
		sb.WriteString("No drift detected. Environment is up to date.\n")
		return sb.String()
	}

	for _, d := range p.Drift {
		switch d.Action {
		case "add":
			sb.WriteString(fmt.Sprintf("  + [%s] %s = %s\n", d.Kind, d.Key, d.To))
		case "remove":
			sb.WriteString(fmt.Sprintf("  - [%s] %s\n", d.Kind, d.Key))
		case "update":
			sb.WriteString(fmt.Sprintf("  ~ [%s] %s: %s → %s\n", d.Kind, d.Key, d.From, d.To))
		}
	}

	return sb.String()
}
