package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lapel/community"
	"lapel/internal/config"
	"lapel/internal/jail"
	"lapel/internal/reconcile"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunProviderBenchmark(t *testing.T) {
	origClient := benchmarkHTTPClient
	defer func() { benchmarkHTTPClient = origClient }()

	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "ornith-35b-q4_k_m",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": "func ReverseInts(s []int) { for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 { s[i], s[j] = s[j], s[i] } }",
					},
				},
			},
			"usage": map[string]int{
				"completion_tokens": 42,
			},
		})
	})

	benchmarkHTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			return rec.Result(), nil
		}),
	}

	res, err := runProviderBenchmark(context.Background(), "http://127.0.0.1:1234", "ornith-35b-q4_k_m", "LM Studio", 18.2, 16, true)
	if err != nil {
		t.Fatalf("runProviderBenchmark failed: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 requests (warmup + timed sample), got %d", calls)
	}
	if res.CompletionTokens != 42 {
		t.Errorf("CompletionTokens = %d, want 42", res.CompletionTokens)
	}
	if res.TokensPerSec <= 0 {
		t.Errorf("expected positive TokensPerSec, got %f", res.TokensPerSec)
	}
	if res.ModelName != "ornith-35b-q4_k_m" {
		t.Errorf("ModelName = %q, want ornith-35b-q4_k_m", res.ModelName)
	}
}

func TestStarterManifestAndExampleBothValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lapel.yaml")
	cmd := initCmd()
	cmd.SetArgs([]string{"--path", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("initCmd failed: %v", err)
	}

	m, err := config.Load(path)
	if err != nil {
		t.Fatalf("starter manifest failed strict Load/Validate: %v", err)
	}
	if m.Environment.Distribution != "lapel" {
		t.Errorf("starter manifest distribution = %q, want lapel", m.Environment.Distribution)
	}
	if m.Jail.Processes.TimeoutSeconds != 0 {
		t.Errorf("starter manifest timeoutSeconds = %d, want 0 (interactive default)", m.Jail.Processes.TimeoutSeconds)
	}

	// Verify docs/examples/lapel.yaml.example matches and validates strictly.
	examplePath := filepath.Join("..", "..", "docs", "examples", "lapel.yaml.example")
	exampleBytes, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("reading example manifest: %v", err)
	}
	if strings.ReplaceAll(string(exampleBytes), "\r\n", "\n") != starterManifest() {
		t.Error("docs/examples/lapel.yaml.example drifted from starterManifest()")
	}
	if _, err := config.Load(examplePath); err != nil {
		t.Fatalf("example manifest failed strict Load/Validate: %v", err)
	}
}

func TestWriteDiffColumnOrder(t *testing.T) {
	res := &reconcile.DiffResult{
		Manifest: config.Manifest{APIVersion: "lapel.dev/v1", Name: "demo"},
		Entries: []reconcile.Entry{
			{Kind: "environment", Name: "lapel", Desired: true, Actual: "installed=lapel"},
			{Kind: "provider", Name: "local", Desired: false, Actual: "closed port"},
		},
	}
	var buf bytes.Buffer
	writeDiff(&buf, res)
	out := buf.String()

	lines := strings.Split(out, "\n")
	// Expected table lines:
	//   KIND         NAME   STATUS  ACTUAL
	//   environment  lapel  OK      installed=lapel
	//   provider     local  DRIFT   closed port
	var envFields, provFields []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "environment" {
			envFields = fields
		}
		if len(fields) >= 4 && fields[0] == "provider" {
			provFields = fields
		}
	}
	if len(envFields) < 4 || envFields[0] != "environment" || envFields[1] != "lapel" || envFields[2] != "OK" {
		t.Errorf("environment row columns misordered:\n%s", out)
	}
	if len(provFields) < 4 || provFields[0] != "provider" || provFields[1] != "local" || provFields[2] != "DRIFT" {
		t.Errorf("provider row columns misordered:\n%s", out)
	}
}

func TestShareReportRejectsRedirect(t *testing.T) {
	origClient := communityHTTPClient
	defer func() { communityHTTPClient = origClient }()

	communityHTTPClient = &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Location", "https://lapel.com/")
			rec.WriteHeader(http.StatusFound)
			return rec.Result(), nil
		}),
	}

	err := shareReport("https://api.lapel.dev/v1", community.Report{HardwareHash: "abc12345"})
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected redirect rejection error, got %v", err)
	}
}

func TestHashPrefixHandlesShortStrings(t *testing.T) {
	if got := hashPrefix("unknown", 8); got != "unknown" {
		t.Errorf("hashPrefix(unknown, 8) = %q, want unknown", got)
	}
	if got := hashPrefix("abcdef123456", 8); got != "abcdef12" {
		t.Errorf("hashPrefix(abcdef123456, 8) = %q, want abcdef12", got)
	}
}

func TestJailControlSymbol(t *testing.T) {
	if got := jailControlSymbol(jail.Enforced); got != "✓" {
		t.Errorf("Enforced symbol = %q, want ✓", got)
	}
	if got := jailControlSymbol(jail.Requested); got != "⚠" {
		t.Errorf("Requested symbol = %q, want ⚠", got)
	}
	if got := jailControlSymbol(jail.NeedsConfig); got != "⚠" {
		t.Errorf("NeedsConfig symbol = %q, want ⚠", got)
	}
	if got := jailControlSymbol(jail.Unsupported); got != "✗" {
		t.Errorf("Unsupported symbol = %q, want ✗", got)
	}
}
