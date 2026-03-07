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

func TestOpenAIAPIAgent_InterfaceCompliance(t *testing.T) {
	var _ Agent = (*OpenAIAPIAgent)(nil)
}

func TestOpenAIAPIAgent_Name(t *testing.T) {
	a := NewOpenAIAPIAgent("key", "gpt-5.4")
	if got := a.Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q", got, "codex")
	}
}

func TestOpenAIAPIAgent_IsAvailable(t *testing.T) {
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
			a := NewOpenAIAPIAgent(tt.apiKey, "gpt-5.4")
			err := a.IsAvailable()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsAvailable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenAIAPIAgent_ExecuteReview(t *testing.T) {
	var gotAuthHeader string
	var gotBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("unmarshaling request body: %v", err)
		}

		resp := `{"choices": [{"message": {"content": "main.go:10: null pointer dereference"}}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewOpenAIAPIAgent("test-api-key", "gpt-5.4")
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

	// Verify Bearer auth header
	if gotAuthHeader != "Bearer test-api-key" {
		t.Errorf("Authorization = %q, want %q", gotAuthHeader, "Bearer test-api-key")
	}

	// Verify model in body
	if model, ok := gotBody["model"].(string); !ok || model != "gpt-5.4" {
		t.Errorf("model = %v, want %q", gotBody["model"], "gpt-5.4")
	}

	// Verify output contains finding
	if !strings.Contains(string(output), "null pointer dereference") {
		t.Errorf("output = %q, want it to contain 'null pointer dereference'", output)
	}
}

func TestOpenAIAPIAgent_ExecuteReview_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
	}))
	defer server.Close()

	a := NewOpenAIAPIAgent("bad-key", "gpt-5.4")
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

func TestOpenAIAPIAgent_ExecuteSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"choices": [{"message": {"content": "{\"findings\": [{\"title\": \"bug\"}]}"}}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewOpenAIAPIAgent("test-key", "gpt-5.4")
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
