# CI Support via API-Direct Agents — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable ACR to run in CI (GitHub Actions) by adding API-direct agent implementations that call LLM APIs via HTTP when API keys are present, falling back to existing CLI agents otherwise.

**Architecture:** Add three new Agent interface implementations (Anthropic, OpenAI, Google) that make single HTTP API calls instead of shelling out to CLI tools. A shared HTTP helper handles request/response plumbing. The factory gains API-key-then-CLI resolution. A composite GitHub Action installs ACR and runs it on PRs.

**Tech Stack:** Go `net/http` + `encoding/json` (no new dependencies), shell scripts for the GitHub Action.

---

### Task 1: Shared HTTP Helper

**Files:**
- Create: `internal/agent/apiclient.go`
- Test: `internal/agent/apiclient_test.go`

This is the foundation — a thin HTTP client used by all three API agents.

**Step 1: Write the failing tests**

```go
// internal/agent/apiclient_test.go
package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIRequest_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("X-Custom-Header") != "test-value" {
			t.Errorf("missing custom header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		// Verify body
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "hello") {
			t.Errorf("body = %q, want it to contain 'hello'", string(body))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "ok"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:    server.URL,
		Body:   []byte(`{"message": "hello"}`),
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAPIRequest_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  server.URL,
		Body: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAPIRequest_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  server.URL,
		Body: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestClassifyHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantAuth   bool
		wantRetry  bool
		wantErr    bool
	}{
		{"200 OK", 200, false, false, false},
		{"401 Unauthorized", 401, true, false, true},
		{"403 Forbidden", 403, true, false, true},
		{"429 Rate Limited", 429, false, true, true},
		{"500 Server Error", 500, false, true, true},
		{"502 Bad Gateway", 502, false, true, true},
		{"503 Service Unavailable", 503, false, true, true},
		{"400 Bad Request", 400, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class := classifyHTTPStatus(tt.statusCode)
			if class.AuthFailure != tt.wantAuth {
				t.Errorf("AuthFailure = %v, want %v", class.AuthFailure, tt.wantAuth)
			}
			if class.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", class.Retryable, tt.wantRetry)
			}
			if class.IsError != tt.wantErr {
				t.Errorf("IsError = %v, want %v", class.IsError, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run "TestAPIRequest|TestClassifyHTTPStatus" -v`
Expected: FAIL — `doAPIRequest`, `apiRequestConfig`, `classifyHTTPStatus` undefined

**Step 3: Write the implementation**

```go
// internal/agent/apiclient.go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// apiRequestConfig holds the parameters for an API request.
type apiRequestConfig struct {
	URL     string
	Body    []byte
	Headers map[string]string
}

// statusClassification categorizes an HTTP status code for error handling.
type statusClassification struct {
	AuthFailure bool
	Retryable   bool
	IsError     bool
}

// doAPIRequest sends a POST request to the given URL with JSON body and headers.
func doAPIRequest(ctx context.Context, cfg apiRequestConfig) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(cfg.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}

	return resp, nil
}

// classifyHTTPStatus categorizes an HTTP status code.
func classifyHTTPStatus(statusCode int) statusClassification {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return statusClassification{}
	case statusCode == 401 || statusCode == 403:
		return statusClassification{AuthFailure: true, IsError: true}
	case statusCode == 429 || statusCode >= 500:
		return statusClassification{Retryable: true, IsError: true}
	default:
		return statusClassification{IsError: true}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run "TestAPIRequest|TestClassifyHTTPStatus" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/apiclient.go internal/agent/apiclient_test.go
git commit -m "feat: add shared API HTTP helper for direct LLM API calls"
```

---

### Task 2: Anthropic API Agent

**Files:**
- Create: `internal/agent/anthropic_api.go`
- Test: `internal/agent/anthropic_api_test.go`

**Step 1: Write the failing tests**

```go
// internal/agent/anthropic_api_test.go
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

func TestAnthropicAPIAgent_Name(t *testing.T) {
	agent := NewAnthropicAPIAgent("test-key", "claude-sonnet-4-6")
	if got := agent.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}

func TestAnthropicAPIAgent_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{"valid key", "sk-test-123", false},
		{"empty key", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAnthropicAPIAgent(tt.apiKey, "claude-sonnet-4-6")
			err := agent.IsAvailable()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsAvailable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAnthropicAPIAgent_InterfaceCompliance(t *testing.T) {
	var _ Agent = (*AnthropicAPIAgent)(nil)
}

func TestAnthropicAPIAgent_ExecuteReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), "test-key")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}

		// Verify request body has messages
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["model"] != "claude-sonnet-4-6" {
			t.Errorf("model = %q, want %q", req["model"], "claude-sonnet-4-6")
		}

		// Return a response with findings
		resp := map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": "main.go:10: potential nil pointer dereference"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewAnthropicAPIAgent("test-key", "claude-sonnet-4-6")
	agent.baseURL = server.URL // override for testing

	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:         "main",
		WorkDir:         ".",
		Diff:            "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n",
		DiffPrecomputed: true,
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	if !strings.Contains(string(output), "nil pointer") {
		t.Errorf("output = %q, want it to contain 'nil pointer'", string(output))
	}
}

func TestAnthropicAPIAgent_ExecuteReview_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
	}))
	defer server.Close()

	agent := NewAnthropicAPIAgent("bad-key", "claude-sonnet-4-6")
	agent.baseURL = server.URL

	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:         "main",
		Diff:            "some diff",
		DiffPrecomputed: true,
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err == nil {
		if result != nil {
			result.Close()
		}
		t.Fatal("expected error for auth failure")
	}

	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "auth") {
		t.Errorf("error = %v, want auth-related error", err)
	}
}

func TestAnthropicAPIAgent_ExecuteSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": `{"findings": [{"title": "Bug", "summary": "A bug"}]}`},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewAnthropicAPIAgent("test-key", "claude-sonnet-4-6")
	agent.baseURL = server.URL

	ctx := context.Background()
	result, err := agent.ExecuteSummary(ctx, "summarize these", []byte(`[{"text":"finding1"}]`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	if !strings.Contains(string(output), "findings") {
		t.Errorf("output = %q, want it to contain 'findings'", string(output))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run "TestAnthropicAPI" -v`
Expected: FAIL — `NewAnthropicAPIAgent`, `AnthropicAPIAgent` undefined

**Step 3: Write the implementation**

```go
// internal/agent/anthropic_api.go
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// Compile-time interface check
var _ Agent = (*AnthropicAPIAgent)(nil)

const defaultAnthropicURL = "https://api.anthropic.com/v1/messages"
const anthropicAPIVersion = "2023-06-01"

// AnthropicAPIAgent implements the Agent interface using the Anthropic HTTP API directly.
type AnthropicAPIAgent struct {
	apiKey  string
	model   string
	baseURL string // overridable for testing
}

// NewAnthropicAPIAgent creates a new AnthropicAPIAgent.
func NewAnthropicAPIAgent(apiKey, model string) *AnthropicAPIAgent {
	return &AnthropicAPIAgent{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultAnthropicURL,
	}
}

func (a *AnthropicAPIAgent) Name() string { return "claude" }

func (a *AnthropicAPIAgent) IsAvailable() error {
	if a.apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	return nil
}

func (a *AnthropicAPIAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	diff := config.Diff
	if !config.DiffPrecomputed {
		var err error
		diff, err = git.GetDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff: %w", err)
		}
	}

	prompt := RenderPrompt(DefaultClaudePrompt, config.Guidance)
	prompt = BuildPromptWithDiff(prompt, diff)

	return a.callAPI(ctx, prompt)
}

func (a *AnthropicAPIAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	return a.callAPI(ctx, fullPrompt)
}

func (a *AnthropicAPIAgent) callAPI(ctx context.Context, prompt string) (*ExecutionResult, error) {
	reqBody := map[string]interface{}{
		"model":      a.model,
		"max_tokens": 16384,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  a.baseURL,
		Body: bodyBytes,
		Headers: map[string]string{
			"x-api-key":         a.apiKey,
			"anthropic-version": anthropicAPIVersion,
		},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	class := classifyHTTPStatus(resp.StatusCode)
	if class.IsError {
		errMsg := fmt.Sprintf("Anthropic API error (HTTP %d): %s", resp.StatusCode, string(respBody))
		if class.AuthFailure {
			return nil, fmt.Errorf("auth failure: %s", errMsg)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	text, err := extractAnthropicText(respBody)
	if err != nil {
		return nil, err
	}

	return newStaticExecutionResult(text), nil
}

// extractAnthropicText extracts the text content from an Anthropic API response.
func extractAnthropicText(body []byte) (string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	var texts []string
	for _, block := range resp.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// newStaticExecutionResult creates an ExecutionResult from a static string.
// Used by API agents where the response is already fully read.
func newStaticExecutionResult(text string) *ExecutionResult {
	reader := io.NopCloser(bytes.NewReader([]byte(text)))
	return NewExecutionResult(reader, func() int { return 0 }, func() string { return "" })
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run "TestAnthropicAPI" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/anthropic_api.go internal/agent/anthropic_api_test.go
git commit -m "feat: add Anthropic API-direct agent implementation"
```

---

### Task 3: OpenAI API Agent

**Files:**
- Create: `internal/agent/openai_api.go`
- Test: `internal/agent/openai_api_test.go`

**Step 1: Write the failing tests**

```go
// internal/agent/openai_api_test.go
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

func TestOpenAIAPIAgent_Name(t *testing.T) {
	agent := NewOpenAIAPIAgent("test-key", "gpt-5.4")
	if got := agent.Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q", got, "codex")
	}
}

func TestOpenAIAPIAgent_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{"valid key", "sk-test-123", false},
		{"empty key", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewOpenAIAPIAgent(tt.apiKey, "gpt-5.4")
			err := agent.IsAvailable()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsAvailable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenAIAPIAgent_InterfaceCompliance(t *testing.T) {
	var _ Agent = (*OpenAIAPIAgent)(nil)
}

func TestOpenAIAPIAgent_ExecuteReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing Bearer auth header")
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["model"] != "gpt-5.4" {
			t.Errorf("model = %q, want %q", req["model"], "gpt-5.4")
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "main.go:10: buffer overflow risk"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewOpenAIAPIAgent("test-key", "gpt-5.4")
	agent.baseURL = server.URL

	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:         "main",
		Diff:            "diff content",
		DiffPrecomputed: true,
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	if !strings.Contains(string(output), "buffer overflow") {
		t.Errorf("output = %q, want it to contain 'buffer overflow'", string(output))
	}
}

func TestOpenAIAPIAgent_ExecuteSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"findings": []}`}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewOpenAIAPIAgent("test-key", "gpt-5.4")
	agent.baseURL = server.URL

	ctx := context.Background()
	result, err := agent.ExecuteSummary(ctx, "summarize", []byte(`[]`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	if !strings.Contains(string(output), "findings") {
		t.Errorf("output = %q, want it to contain 'findings'", string(output))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run "TestOpenAIAPI" -v`
Expected: FAIL — `NewOpenAIAPIAgent`, `OpenAIAPIAgent` undefined

**Step 3: Write the implementation**

```go
// internal/agent/openai_api.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// Compile-time interface check
var _ Agent = (*OpenAIAPIAgent)(nil)

const defaultOpenAIURL = "https://api.openai.com/v1/chat/completions"

// OpenAIAPIAgent implements the Agent interface using the OpenAI HTTP API directly.
type OpenAIAPIAgent struct {
	apiKey  string
	model   string
	baseURL string
}

// NewOpenAIAPIAgent creates a new OpenAIAPIAgent.
func NewOpenAIAPIAgent(apiKey, model string) *OpenAIAPIAgent {
	return &OpenAIAPIAgent{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultOpenAIURL,
	}
}

func (a *OpenAIAPIAgent) Name() string { return "codex" }

func (a *OpenAIAPIAgent) IsAvailable() error {
	if a.apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY not set")
	}
	return nil
}

func (a *OpenAIAPIAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	diff := config.Diff
	if !config.DiffPrecomputed {
		var err error
		diff, err = git.GetDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff: %w", err)
		}
	}

	// Use the Claude prompt format — it's more concise and works well for all API agents.
	// The Codex CLI uses its own built-in review mode; the API agent uses the same
	// prompt as Claude since we're making a standard chat completion call.
	prompt := RenderPrompt(DefaultClaudePrompt, config.Guidance)
	prompt = BuildPromptWithDiff(prompt, diff)

	return a.callAPI(ctx, prompt)
}

func (a *OpenAIAPIAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	return a.callAPI(ctx, fullPrompt)
}

func (a *OpenAIAPIAgent) callAPI(ctx context.Context, prompt string) (*ExecutionResult, error) {
	reqBody := map[string]interface{}{
		"model": a.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  a.baseURL,
		Body: bodyBytes,
		Headers: map[string]string{
			"Authorization": "Bearer " + a.apiKey,
		},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	class := classifyHTTPStatus(resp.StatusCode)
	if class.IsError {
		errMsg := fmt.Sprintf("OpenAI API error (HTTP %d): %s", resp.StatusCode, string(respBody))
		if class.AuthFailure {
			return nil, fmt.Errorf("auth failure: %s", errMsg)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	text, err := extractOpenAIText(respBody)
	if err != nil {
		return nil, err
	}

	return newStaticExecutionResult(text), nil
}

// extractOpenAIText extracts text from an OpenAI chat completion response.
func extractOpenAIText(body []byte) (string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	var texts []string
	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			texts = append(texts, choice.Message.Content)
		}
	}
	return strings.Join(texts, "\n"), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run "TestOpenAIAPI" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/openai_api.go internal/agent/openai_api_test.go
git commit -m "feat: add OpenAI API-direct agent implementation"
```

---

### Task 4: Google API Agent

**Files:**
- Create: `internal/agent/google_api.go`
- Test: `internal/agent/google_api_test.go`

**Step 1: Write the failing tests**

```go
// internal/agent/google_api_test.go
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

func TestGoogleAPIAgent_Name(t *testing.T) {
	agent := NewGoogleAPIAgent("test-key", "gemini-3.0-flash")
	if got := agent.Name(); got != "gemini" {
		t.Errorf("Name() = %q, want %q", got, "gemini")
	}
}

func TestGoogleAPIAgent_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{"valid key", "test-key-123", false},
		{"empty key", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewGoogleAPIAgent(tt.apiKey, "gemini-3.0-flash")
			err := agent.IsAvailable()
			if (err != nil) != tt.wantErr {
				t.Errorf("IsAvailable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGoogleAPIAgent_InterfaceCompliance(t *testing.T) {
	var _ Agent = (*GoogleAPIAgent)(nil)
}

func TestGoogleAPIAgent_ExecuteReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("missing API key in query params")
		}

		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{"content": map[string]interface{}{
					"parts": []map[string]string{
						{"text": "handler.go:25: SQL injection vulnerability"},
					},
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewGoogleAPIAgent("test-key", "gemini-3.0-flash")
	agent.baseURL = server.URL

	ctx := context.Background()
	config := &ReviewConfig{
		BaseRef:         "main",
		Diff:            "diff content",
		DiffPrecomputed: true,
	}

	result, err := agent.ExecuteReview(ctx, config)
	if err != nil {
		t.Fatalf("ExecuteReview() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	if !strings.Contains(string(output), "SQL injection") {
		t.Errorf("output = %q, want it to contain 'SQL injection'", string(output))
	}
}

func TestGoogleAPIAgent_ExecuteSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{"content": map[string]interface{}{
					"parts": []map[string]string{
						{"text": `{"findings": []}`},
					},
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	agent := NewGoogleAPIAgent("test-key", "gemini-3.0-flash")
	agent.baseURL = server.URL

	ctx := context.Background()
	result, err := agent.ExecuteSummary(ctx, "summarize", []byte(`[]`))
	if err != nil {
		t.Fatalf("ExecuteSummary() error: %v", err)
	}
	defer result.Close()

	output, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	if !strings.Contains(string(output), "findings") {
		t.Errorf("output = %q, want it to contain 'findings'", string(output))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run "TestGoogleAPI" -v`
Expected: FAIL — `NewGoogleAPIAgent`, `GoogleAPIAgent` undefined

**Step 3: Write the implementation**

```go
// internal/agent/google_api.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// Compile-time interface check
var _ Agent = (*GoogleAPIAgent)(nil)

const defaultGoogleURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GoogleAPIAgent implements the Agent interface using the Google Gemini HTTP API directly.
type GoogleAPIAgent struct {
	apiKey  string
	model   string
	baseURL string
}

// NewGoogleAPIAgent creates a new GoogleAPIAgent.
func NewGoogleAPIAgent(apiKey, model string) *GoogleAPIAgent {
	return &GoogleAPIAgent{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultGoogleURL,
	}
}

func (a *GoogleAPIAgent) Name() string { return "gemini" }

func (a *GoogleAPIAgent) IsAvailable() error {
	if a.apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY not set")
	}
	return nil
}

func (a *GoogleAPIAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	diff := config.Diff
	if !config.DiffPrecomputed {
		var err error
		diff, err = git.GetDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff: %w", err)
		}
	}

	prompt := RenderPrompt(DefaultGeminiPrompt, config.Guidance)
	prompt = BuildPromptWithDiff(prompt, diff)

	return a.callAPI(ctx, prompt)
}

func (a *GoogleAPIAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	return a.callAPI(ctx, fullPrompt)
}

func (a *GoogleAPIAgent) callAPI(ctx context.Context, prompt string) (*ExecutionResult, error) {
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Google API uses query param for auth and model in URL path
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", a.baseURL, a.model, a.apiKey)

	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  url,
		Body: bodyBytes,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	class := classifyHTTPStatus(resp.StatusCode)
	if class.IsError {
		errMsg := fmt.Sprintf("Google API error (HTTP %d): %s", resp.StatusCode, string(respBody))
		if class.AuthFailure {
			return nil, fmt.Errorf("auth failure: %s", errMsg)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	text, err := extractGoogleText(respBody)
	if err != nil {
		return nil, err
	}

	return newStaticExecutionResult(text), nil
}

// extractGoogleText extracts text from a Google Gemini API response.
func extractGoogleText(body []byte) (string, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Google response: %w", err)
	}

	var texts []string
	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	return strings.Join(texts, "\n"), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run "TestGoogleAPI" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/google_api.go internal/agent/google_api_test.go
git commit -m "feat: add Google Gemini API-direct agent implementation"
```

---

### Task 5: Factory — API-Key-Then-CLI Resolution

**Files:**
- Modify: `internal/agent/factory.go`
- Modify: `internal/agent/factory_test.go`

**Step 1: Write the failing tests**

Add these tests to `internal/agent/factory_test.go`:

```go
func TestNewAgent_PrefersAPIKeyOverCLI(t *testing.T) {
	// Set API key for claude
	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")

	agent, err := NewAgent("claude")
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Should return an AnthropicAPIAgent, not a ClaudeAgent
	if _, ok := agent.(*AnthropicAPIAgent); !ok {
		t.Errorf("expected *AnthropicAPIAgent, got %T", agent)
	}
}

func TestNewAgent_FallsBackToCLI(t *testing.T) {
	// Ensure no API key is set
	t.Setenv("ANTHROPIC_API_KEY", "")

	agent, err := NewAgent("claude")
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Should return a ClaudeAgent (CLI fallback)
	if _, ok := agent.(*ClaudeAgent); !ok {
		t.Errorf("expected *ClaudeAgent, got %T", agent)
	}
}

func TestNewAgent_OpenAIAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key-123")

	agent, err := NewAgent("codex")
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	if _, ok := agent.(*OpenAIAPIAgent); !ok {
		t.Errorf("expected *OpenAIAPIAgent, got %T", agent)
	}
}

func TestNewAgent_GeminiAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-123")

	agent, err := NewAgent("gemini")
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	if _, ok := agent.(*GoogleAPIAgent); !ok {
		t.Errorf("expected *GoogleAPIAgent, got %T", agent)
	}
}

func TestNewAgent_CustomModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ACR_ANTHROPIC_MODEL", "claude-opus-4-6")

	agent, err := NewAgent("claude")
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	apiAgent, ok := agent.(*AnthropicAPIAgent)
	if !ok {
		t.Fatalf("expected *AnthropicAPIAgent, got %T", agent)
	}
	if apiAgent.model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", apiAgent.model, "claude-opus-4-6")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run "TestNewAgent_Prefers|TestNewAgent_FallsBack|TestNewAgent_OpenAI|TestNewAgent_Gemini|TestNewAgent_Custom" -v`
Expected: FAIL — factory doesn't check for API keys yet

**Step 3: Update the factory**

Replace the `NewAgent` function in `internal/agent/factory.go`:

```go
// apiAgentConfig maps agent names to their API key env var, model env var, default model, and constructor.
var apiAgentConfig = map[string]struct {
	keyEnvVar     string
	modelEnvVar   string
	defaultModel  string
	newAgent      func(apiKey, model string) Agent
}{
	"claude": {
		keyEnvVar:    "ANTHROPIC_API_KEY",
		modelEnvVar:  "ACR_ANTHROPIC_MODEL",
		defaultModel: "claude-sonnet-4-6",
		newAgent:     func(k, m string) Agent { return NewAnthropicAPIAgent(k, m) },
	},
	"codex": {
		keyEnvVar:    "OPENAI_API_KEY",
		modelEnvVar:  "ACR_OPENAI_MODEL",
		defaultModel: "gpt-5.4",
		newAgent:     func(k, m string) Agent { return NewOpenAIAPIAgent(k, m) },
	},
	"gemini": {
		keyEnvVar:    "GEMINI_API_KEY",
		modelEnvVar:  "ACR_GOOGLE_MODEL",
		defaultModel: "gemini-3.0-flash",
		newAgent:     func(k, m string) Agent { return NewGoogleAPIAgent(k, m) },
	},
}

// NewAgent creates an Agent by name.
// Resolution order: API key in env → API-direct agent; no key → CLI agent.
// Supported agents: codex, claude, gemini
func NewAgent(name string) (Agent, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, supported: %v", name, SupportedAgents)
	}

	// Check for API key — if present, use API-direct agent
	if apiCfg, ok := apiAgentConfig[name]; ok {
		if apiKey := os.Getenv(apiCfg.keyEnvVar); apiKey != "" {
			model := os.Getenv(apiCfg.modelEnvVar)
			if model == "" {
				model = apiCfg.defaultModel
			}
			return apiCfg.newAgent(apiKey, model), nil
		}
	}

	// Fall back to CLI agent
	return reg.newAgent(), nil
}
```

Add `"os"` to the imports in factory.go.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run "TestNewAgent" -v`
Expected: PASS (all existing + new tests)

**Step 5: Commit**

```bash
git add internal/agent/factory.go internal/agent/factory_test.go
git commit -m "feat: factory resolves API-direct agents when API keys are present"
```

---

### Task 6: Update Auth Hints for API Mode

**Files:**
- Modify: `internal/agent/auth.go`

The auth hints should reflect that API keys are now a valid auth method.

**Step 1: Update the auth hints**

In `internal/agent/auth.go`, update the `authHints` map:

```go
var authHints = map[string]string{
	"gemini": "Set GEMINI_API_KEY or install the gemini CLI and run 'gemini auth login' to authenticate.",
	"claude": "Set ANTHROPIC_API_KEY or install the claude CLI and run 'claude login' to authenticate.",
	"codex":  "Set OPENAI_API_KEY or install the codex CLI and run 'codex auth' to authenticate.",
}
```

**Step 2: Run existing auth tests**

Run: `go test ./internal/agent/ -run "TestAuth" -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/agent/auth.go
git commit -m "feat: update auth hints to mention API key option"
```

---

### Task 7: Model Config in .acr.yaml

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestConfig_ModelsSection(t *testing.T) {
	yaml := `
models:
  claude: claude-opus-4-6
  codex: gpt-5.3-codex
  gemini: gemini-3.1-pro
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".acr.yaml")
	os.WriteFile(configPath, []byte(yaml), 0644)

	result, err := LoadFromPathWithWarnings(configPath)
	if err != nil {
		t.Fatalf("LoadFromPathWithWarnings() error: %v", err)
	}

	if result.Config.Models == nil {
		t.Fatal("Models is nil")
	}
	if result.Config.Models.Claude != nil && *result.Config.Models.Claude != "claude-opus-4-6" {
		t.Errorf("Models.Claude = %q, want %q", *result.Config.Models.Claude, "claude-opus-4-6")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run "TestConfig_ModelsSection" -v`
Expected: FAIL — `Models` field doesn't exist on Config

**Step 3: Add Models to Config struct**

In `internal/config/config.go`, add to the `Config` struct:

```go
type ModelsConfig struct {
	Claude *string `yaml:"claude"`
	Codex  *string `yaml:"codex"`
	Gemini *string `yaml:"gemini"`
}
```

Add `Models ModelsConfig `yaml:"models"`` to the `Config` struct.

Add `"models"` to `knownTopLevelKeys`.

Add `knownModelsKeys` and check in `checkUnknownKeys`:

```go
var knownModelsKeys = []string{"claude", "codex", "gemini"}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run "TestConfig_ModelsSection" -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add models section to .acr.yaml config"
```

---

### Task 8: Wire Model Config Into Factory

**Files:**
- Modify: `internal/agent/factory.go`

The factory currently reads model from env vars only. It should also accept model overrides from resolved config. This requires a small API change — `NewAgent` needs to accept optional model overrides.

**Step 1: Update NewAgent to accept model overrides**

Add a `NewAgentWithModels` function (keeping `NewAgent` for backwards compatibility):

```go
// ModelOverrides holds per-agent model overrides from config.
type ModelOverrides struct {
	Claude string
	Codex  string
	Gemini string
}

// NewAgentWithModels creates an Agent by name with optional model overrides.
// Model precedence: env var > config override > default.
func NewAgentWithModels(name string, models ModelOverrides) (Agent, error) {
	reg, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q, supported: %v", name, SupportedAgents)
	}

	if apiCfg, ok := apiAgentConfig[name]; ok {
		if apiKey := os.Getenv(apiCfg.keyEnvVar); apiKey != "" {
			// Env var model takes highest precedence
			model := os.Getenv(apiCfg.modelEnvVar)
			// Then config override
			if model == "" {
				switch name {
				case "claude":
					model = models.Claude
				case "codex":
					model = models.Codex
				case "gemini":
					model = models.Gemini
				}
			}
			// Then default
			if model == "" {
				model = apiCfg.defaultModel
			}
			return apiCfg.newAgent(apiKey, model), nil
		}
	}

	return reg.newAgent(), nil
}
```

**Step 2: Run all tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/agent/factory.go
git commit -m "feat: add NewAgentWithModels for config-driven model selection"
```

---

### Task 9: GitHub Action — Composite Action

**Files:**
- Create: `action/action.yml`
- Create: `action/install-acr.sh`

**Step 1: Create install-acr.sh**

```bash
#!/bin/bash
set -euo pipefail

# install-acr.sh — Download and install the ACR binary for GitHub Actions.
# Usage: ./install-acr.sh [version]
# version: "latest" (default) or a specific version like "v1.2.3"

VERSION="${1:-latest}"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

REPO="richhaase/agentic-code-reviewer"

if [ "$VERSION" = "latest" ]; then
  VERSION=$(gh release view --repo "$REPO" --json tagName -q '.tagName')
fi

ARCHIVE="acr_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "Installing ACR ${VERSION} (${OS}/${ARCH})..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -sL "$URL" -o "$TMPDIR/$ARCHIVE"
tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"

# Add to PATH
install -m 755 "$TMPDIR/acr" /usr/local/bin/acr

echo "ACR ${VERSION} installed successfully"
acr --version
```

**Step 2: Create action.yml**

```yaml
name: 'ACR - Agentic Code Reviewer'
description: 'Run parallel AI-powered code reviews on pull requests'
branding:
  icon: 'eye'
  color: 'blue'

inputs:
  fail-on-findings:
    description: 'Fail the action if findings are detected'
    required: false
    default: 'false'
  acr-version:
    description: 'ACR version to install (e.g., "latest" or "v1.2.3")'
    required: false
    default: 'latest'

outputs:
  findings-count:
    description: 'Number of findings detected'
    value: ${{ steps.review.outputs.findings-count }}
  exit-code:
    description: 'ACR exit code (0=clean, 1=findings, 2=error)'
    value: ${{ steps.review.outputs.exit-code }}

runs:
  using: 'composite'
  steps:
    - name: Install ACR
      shell: bash
      run: |
        chmod +x "${{ github.action_path }}/install-acr.sh"
        "${{ github.action_path }}/install-acr.sh" "${{ inputs.acr-version }}"

    - name: Run ACR Review
      id: review
      shell: bash
      run: |
        # Extract PR number from GitHub event
        PR_NUMBER=$(jq -r '.pull_request.number // .number // empty' "$GITHUB_EVENT_PATH")
        if [ -z "$PR_NUMBER" ]; then
          echo "::error::Could not determine PR number. This action must run on pull_request events."
          exit 2
        fi

        echo "Running ACR review on PR #${PR_NUMBER}..."

        set +e
        acr --pr "$PR_NUMBER" --yes
        EXIT_CODE=$?
        set -e

        echo "exit-code=$EXIT_CODE" >> "$GITHUB_OUTPUT"

        # Count findings from exit code (1 = findings present)
        if [ "$EXIT_CODE" -eq 1 ]; then
          echo "findings-count=1" >> "$GITHUB_OUTPUT"
        else
          echo "findings-count=0" >> "$GITHUB_OUTPUT"
        fi

        # Handle exit code based on fail-on-findings input
        if [ "$EXIT_CODE" -eq 1 ] && [ "${{ inputs.fail-on-findings }}" != "true" ]; then
          echo "Findings detected but fail-on-findings is false. Exiting successfully."
          exit 0
        fi

        exit "$EXIT_CODE"
```

**Step 3: Make install script executable and commit**

```bash
chmod +x action/install-acr.sh
git add action/action.yml action/install-acr.sh
git commit -m "feat: add GitHub Action for running ACR on pull requests"
```

---

### Task 10: Run Full Test Suite and Lint

**Step 1: Run all tests**

Run: `make check`
Expected: All tests pass, lint clean, vet clean, staticcheck clean

**Step 2: Fix any issues found**

Address any lint warnings, test failures, or formatting issues.

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: address lint and test issues from CI support changes"
```

---

### Task 11: Update README Documentation

**Files:**
- Modify: `README.md`

Add a "CI / GitHub Actions" section to the README documenting:
- The GitHub Action usage
- API key env vars needed
- Example workflow YAML
- How `.acr.yaml` controls review behavior in CI

**Step 1: Add CI section to README**

Add after the existing "Configuration" section:

```markdown
## CI / GitHub Actions

ACR can run automatically on pull requests via the included GitHub Action.

### Setup

1. Add API key secrets to your repository (Settings > Secrets):
   - `OPENAI_API_KEY` for Codex agent
   - `ANTHROPIC_API_KEY` for Claude agent
   - `GEMINI_API_KEY` for Gemini agent

2. Create `.github/workflows/acr.yml`:

\`\`\`yaml
name: Code Review
on: [pull_request]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
      - uses: richhaase/agentic-code-reviewer/action@v1
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
\`\`\`

Review behavior (agents, reviewers, timeouts) is controlled by your `.acr.yaml` config file.

### API-Direct Mode

When API keys are set in the environment, ACR calls LLM APIs directly instead of requiring CLI tools. This happens automatically — no configuration needed.

| Agent | API Key | Default Model |
|-------|---------|---------------|
| Codex | `OPENAI_API_KEY` | gpt-5.4 |
| Claude | `ANTHROPIC_API_KEY` | claude-sonnet-4-6 |
| Gemini | `GEMINI_API_KEY` | gemini-3.0-flash |

Override models via env vars (`ACR_OPENAI_MODEL`, `ACR_ANTHROPIC_MODEL`, `ACR_GOOGLE_MODEL`) or `.acr.yaml`:

\`\`\`yaml
models:
  codex: gpt-5.4
  claude: claude-sonnet-4-6
  gemini: gemini-3.0-flash
\`\`\`
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add CI/GitHub Actions and API-direct mode documentation"
```
