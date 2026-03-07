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

func TestGoogleAPIAgent_InterfaceCompliance(t *testing.T) {
	var _ Agent = (*GoogleAPIAgent)(nil)
}

func TestGoogleAPIAgent_Name(t *testing.T) {
	a := NewGoogleAPIAgent("key", "gemini-2.5-pro")
	if got := a.Name(); got != "gemini" {
		t.Errorf("Name() = %q, want %q", got, "gemini")
	}
}

func TestGoogleAPIAgent_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{"valid key", "test-key", false},
		{"empty key", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewGoogleAPIAgent(tt.apiKey, "gemini-2.5-pro")
			err := a.IsAvailable()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsAvailable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGoogleAPIAgent_ExecuteReview(t *testing.T) {
	var gotAPIKey string
	var gotBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Google API uses query param for API key, not header
		gotAPIKey = r.URL.Query().Get("key")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("unmarshaling request body: %v", err)
		}

		resp := `{"candidates": [{"content": {"parts": [{"text": "main.go:10: null pointer dereference"}]}}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewGoogleAPIAgent("test-api-key", "gemini-2.5-pro")
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

	// Verify API key in query param
	if gotAPIKey != "test-api-key" {
		t.Errorf("query param key = %q, want %q", gotAPIKey, "test-api-key")
	}

	// Verify request body uses contents/parts format
	contents, ok := gotBody["contents"].([]interface{})
	if !ok || len(contents) == 0 {
		t.Fatalf("request body missing 'contents' array")
	}
	firstContent, ok := contents[0].(map[string]interface{})
	if !ok {
		t.Fatalf("contents[0] is not a map")
	}
	parts, ok := firstContent["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		t.Fatalf("contents[0] missing 'parts' array")
	}

	// Verify output contains finding
	if !strings.Contains(string(output), "null pointer dereference") {
		t.Errorf("output = %q, want it to contain 'null pointer dereference'", output)
	}
}

func TestGoogleAPIAgent_ExecuteReview_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
	}))
	defer server.Close()

	a := NewGoogleAPIAgent("bad-key", "gemini-2.5-pro")
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

func TestGoogleAPIAgent_ExecuteSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"candidates": [{"content": {"parts": [{"text": "{\"findings\": [{\"title\": \"bug\"}]}"}]}}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewGoogleAPIAgent("test-key", "gemini-2.5-pro")
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

func TestGoogleAPIAgent_MultipleCandidatesAndParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"candidates": [{"content": {"parts": [{"text": "first part"}, {"text": "second part"}]}}, {"content": {"parts": [{"text": "third part"}]}}]}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	a := NewGoogleAPIAgent("test-key", "gemini-2.5-pro")
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
	if !strings.Contains(outputStr, "first part") || !strings.Contains(outputStr, "second part") || !strings.Contains(outputStr, "third part") {
		t.Errorf("output = %q, want it to contain all three parts", outputStr)
	}
}
