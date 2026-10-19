package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunProviderBenchmark(t *testing.T) {
	origClient := benchmarkHTTPClient
	defer func() { benchmarkHTTPClient = origClient }()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	res, err := runProviderBenchmark(context.Background(), "http://127.0.0.1:1234", "ornith-35b-q4_k_m", "LM Studio", 18.2, 16)
	if err != nil {
		t.Fatalf("runProviderBenchmark failed: %v", err)
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
