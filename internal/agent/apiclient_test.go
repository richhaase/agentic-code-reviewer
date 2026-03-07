package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoAPIRequest_Success(t *testing.T) {
	var gotMethod string
	var gotContentType string
	var gotCustomHeader string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotCustomHeader = r.Header.Get("X-Custom")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	cfg := apiRequestConfig{
		URL:  server.URL,
		Body: []byte(`{"prompt":"hello"}`),
		Headers: map[string]string{
			"X-Custom": "test-value",
		},
	}

	resp, err := doAPIRequest(context.Background(), cfg)
	if err != nil {
		t.Fatalf("doAPIRequest() error = %v", err)
	}
	defer resp.Body.Close()

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotCustomHeader != "test-value" {
		t.Errorf("X-Custom = %q, want test-value", gotCustomHeader)
	}
	if string(gotBody) != `{"prompt":"hello"}` {
		t.Errorf("body = %q, want %q", gotBody, `{"prompt":"hello"}`)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	if string(respBody) != `{"result":"ok"}` {
		t.Errorf("response body = %q, want %q", respBody, `{"result":"ok"}`)
	}
}

func TestDoAPIRequest_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	cfg := apiRequestConfig{
		URL:  server.URL,
		Body: []byte(`{}`),
	}

	resp, err := doAPIRequest(context.Background(), cfg)
	if err != nil {
		t.Fatalf("doAPIRequest() error = %v, want nil (HTTP errors are in response)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestDoAPIRequest_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := apiRequestConfig{
		URL:  server.URL,
		Body: []byte(`{}`),
	}

	_, err := doAPIRequest(ctx, cfg)
	if err == nil {
		t.Fatal("doAPIRequest() error = nil, want context canceled error")
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantAuth   bool
		wantRetry  bool
		wantError  bool
	}{
		{"200 OK", 200, false, false, false},
		{"201 Created", 201, false, false, false},
		{"401 Unauthorized", 401, true, false, true},
		{"403 Forbidden", 403, true, false, true},
		{"429 Too Many Requests", 429, false, true, true},
		{"400 Bad Request", 400, false, false, true},
		{"404 Not Found", 404, false, false, true},
		{"500 Internal Server Error", 500, false, true, true},
		{"502 Bad Gateway", 502, false, true, true},
		{"503 Service Unavailable", 503, false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyHTTPStatus(tt.statusCode)
			if got.AuthFailure != tt.wantAuth {
				t.Errorf("AuthFailure = %v, want %v", got.AuthFailure, tt.wantAuth)
			}
			if got.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", got.Retryable, tt.wantRetry)
			}
			if got.IsError != tt.wantError {
				t.Errorf("IsError = %v, want %v", got.IsError, tt.wantError)
			}
		})
	}
}

func TestNewStaticExecutionResult(t *testing.T) {
	text := "This is the LLM response text."
	result := newStaticExecutionResult(text)

	// Should be readable
	got, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != text {
		t.Errorf("content = %q, want %q", got, text)
	}

	// Close should succeed
	err = result.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Exit code should be 0
	if result.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", result.ExitCode())
	}

	// Stderr should be empty
	if result.Stderr() != "" {
		t.Errorf("Stderr() = %q, want empty", result.Stderr())
	}
}

func TestNewStaticExecutionResult_Empty(t *testing.T) {
	result := newStaticExecutionResult("")

	got, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "" {
		t.Errorf("content = %q, want empty", got)
	}

	err = result.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if result.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", result.ExitCode())
	}
}
