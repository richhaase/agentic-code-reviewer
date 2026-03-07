package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicAPIAgent_InterfaceCompliance(t *testing.T) {
	var _ Agent = (*AnthropicAPIAgent)(nil)
}

func TestAnthropicAPIAgent_Name(t *testing.T) {
	a := NewAnthropicAPIAgent("key", "claude-sonnet-4-6")
	if got := a.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}

func TestAnthropicAPIAgent_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{"valid key", "sk-test-key", false},
		{"empty key", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAnthropicAPIAgent(tt.apiKey, "claude-sonnet-4-6")
			err := a.IsAvailable()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsAvailable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAnthropicAPIAgent_ExecuteReview(t *testing.T) {
	var gotAPIKey string
	var gotVersion string
	var gotBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("unmarshaling request body: %v", err)
		}

		resp := `{"content": [{"type": "text", "text": "main.go:10: null pointer dereference"}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewAnthropicAPIAgent("test-api-key", "claude-sonnet-4-6")
	a.baseURL = server.URL

	config := &ReviewConfig{
		Diff:            "diff --git a/main.go b/main.go\n+var x *int\n+_ = *x",
		DiffPrecomputed: true,
	}

	result, err := a.ExecuteReview(context.Background(), config)
	if err != nil {
		t.Fatalf("ExecuteReview() error = %v", err)
	}

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = result.Close()

	// Verify headers
	if gotAPIKey != "test-api-key" {
		t.Errorf("x-api-key = %q, want %q", gotAPIKey, "test-api-key")
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", gotVersion, "2023-06-01")
	}

	// Verify model in body
	if model, ok := gotBody["model"].(string); !ok || model != "claude-sonnet-4-6" {
		t.Errorf("model = %v, want %q", gotBody["model"], "claude-sonnet-4-6")
	}

	// Verify output contains finding
	if !strings.Contains(string(output), "null pointer dereference") {
		t.Errorf("output = %q, want it to contain 'null pointer dereference'", output)
	}
}

func TestAnthropicAPIAgent_ExecuteReview_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
	}))
	defer server.Close()

	a := NewAnthropicAPIAgent("bad-key", "claude-sonnet-4-6")
	a.baseURL = server.URL

	config := &ReviewConfig{
		Diff:            "some diff",
		DiffPrecomputed: true,
	}

	_, err := a.ExecuteReview(context.Background(), config)
	if err == nil {
		t.Fatal("ExecuteReview() error = nil, want auth failure error")
	}
	if !strings.Contains(err.Error(), "auth failure") {
		t.Errorf("error = %q, want it to contain 'auth failure'", err.Error())
	}
}

func TestAnthropicAPIAgent_ExecuteSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"content": [{"type": "text", "text": "{\"findings\": [{\"title\": \"bug\"}]}"}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewAnthropicAPIAgent("test-key", "claude-sonnet-4-6")
	a.baseURL = server.URL

	input := []byte(`[{"description": "some finding"}]`)
	result, err := a.ExecuteSummary(context.Background(), "Summarize these findings", input)
	if err != nil {
		t.Fatalf("ExecuteSummary() error = %v", err)
	}

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = result.Close()

	if !strings.Contains(string(output), "findings") {
		t.Errorf("output = %q, want it to contain 'findings'", output)
	}
}

func TestAnthropicAPIAgent_MultipleContentBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"content": [{"type": "text", "text": "first block"}, {"type": "text", "text": "second block"}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewAnthropicAPIAgent("test-key", "claude-sonnet-4-6")
	a.baseURL = server.URL

	result, err := a.ExecuteSummary(context.Background(), "test prompt", []byte("{}"))
	if err != nil {
		t.Fatalf("ExecuteSummary() error = %v", err)
	}

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	_ = result.Close()

	outputStr := string(output)
	if !strings.Contains(outputStr, "first block") || !strings.Contains(outputStr, "second block") {
		t.Errorf("output = %q, want it to contain both content blocks", outputStr)
	}
}
