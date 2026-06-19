package state

import (
	"fmt"
	"time"
)

// Resource kinds, matching reconcile Entry.Kind values.
const (
	KindEnvironment = "environment"
	KindProvider    = "provider"
	KindWorkspace   = "workspace"
)

// DesiredResource is a persisted desired-state baseline for one managed resource.
type DesiredResource struct {
	Kind        string
	Name        string
	SpecJSON    string
	DesiredHash string
	UpdatedAt   time.Time
}

// SetDesired persists the desired baseline for one managed resource so that
// diff/status can detect drift against it later. kind is one of KindEnvironment,
// KindProvider or KindWorkspace. specJSON must be canonical JSON of the declared
// spec; desiredHash is its content hash.
func (s *Store) SetDesired(kind, name, specJSON, desiredHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := nowUTC()
	switch kind {
	case KindEnvironment:
		_, err := s.db.Exec(`
INSERT INTO environments (name,distribution,desired_hash,actual_hash,spec_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
  desired_hash=excluded.desired_hash, actual_hash=excluded.actual_hash, spec_json=excluded.spec_json, updated_at=excluded.updated_at`,
			name, "", desiredHash, "", specJSON, now, now)
		return err
	case KindProvider:
		_, err := s.db.Exec(`
INSERT INTO providers (name,type,base_url,model,desired_hash,actual_hash,spec_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
  desired_hash=excluded.desired_hash, actual_hash=excluded.actual_hash, spec_json=excluded.spec_json, updated_at=excluded.updated_at`,
			name, "", "", "", desiredHash, "", specJSON, now, now)
		return err
	case KindWorkspace:
		_, err := s.db.Exec(`
INSERT INTO workspaces (name,path,source,import_mode,desired_hash,actual_hash,spec_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET
  desired_hash=excluded.desired_hash, actual_hash=excluded.actual_hash, spec_json=excluded.spec_json, updated_at=excluded.updated_at`,
			name, "", "", "", desiredHash, "", specJSON, now, now)
		return err
	default:
		return fmt.Errorf("unknown resource kind %q", kind)
	}
}

// DesiredResources returns every persisted desired baseline ordered by kind then
// name. Callers use this as the committed reference for drift detection.
func (s *Store) DesiredResources() ([]*DesiredResource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*DesiredResource
	for _, q := range []string{
		`SELECT 'environment',name,spec_json,desired_hash,updated_at FROM environments ORDER BY name`,
		`SELECT 'provider',name,spec_json,desired_hash,updated_at FROM providers ORDER BY name`,
		`SELECT 'workspace',name,spec_json,desired_hash,updated_at FROM workspaces ORDER BY name`,
	} {
		rows, err := s.db.Query(q)
		if err != nil {
			return nil, fmt.Errorf("querying desired resources: %w", err)
		}
		for rows.Next() {
			var r DesiredResource
			if err := rows.Scan(&r.Kind, &r.Name, &r.SpecJSON, &r.DesiredHash, &timeScan{t: &r.UpdatedAt}); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, &r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
