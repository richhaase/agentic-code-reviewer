package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// Compile-time interface check.
var _ Agent = (*AnthropicAPIAgent)(nil)

const anthropicDefaultBaseURL = "https://api.anthropic.com/v1/messages"

// AnthropicAPIAgent implements the Agent interface using the Anthropic Messages API directly.
type AnthropicAPIAgent struct {
	apiKey  string
	model   string
	baseURL string
}

// NewAnthropicAPIAgent creates a new AnthropicAPIAgent with the given API key and model.
func NewAnthropicAPIAgent(apiKey, model string) *AnthropicAPIAgent {
	return &AnthropicAPIAgent{
		apiKey:  apiKey,
		model:   model,
		baseURL: anthropicDefaultBaseURL,
	}
}

// Name returns the agent's identifier.
func (a *AnthropicAPIAgent) Name() string {
	return "claude"
}

// IsAvailable checks if the API key is configured.
func (a *AnthropicAPIAgent) IsAvailable() error {
	if a.apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}
	return nil
}

// ExecuteReview runs a code review using the Anthropic Messages API.
func (a *AnthropicAPIAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	// Get the diff
	diff := config.Diff
	if !config.DiffPrecomputed {
		var err error
		diff, err = git.GetDiff(ctx, config.BaseRef, config.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff for review: %w", err)
		}
	}

	// Build the prompt
	prompt := RenderPrompt(DefaultClaudePrompt, config.Guidance)
	prompt = BuildPromptWithDiff(prompt, diff)

	return a.callAPI(ctx, prompt)
}

// ExecuteSummary runs a summarization task using the Anthropic Messages API.
func (a *AnthropicAPIAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	return a.callAPI(ctx, fullPrompt)
}

// anthropicRequest is the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is a single message in an Anthropic API request.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response body from the Anthropic Messages API.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
}

// anthropicContentBlock is a single content block in an Anthropic API response.
type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// callAPI sends a prompt to the Anthropic Messages API and returns the result.
func (a *AnthropicAPIAgent) callAPI(ctx context.Context, prompt string) (*ExecutionResult, error) {
	reqBody := anthropicRequest{
		Model:     a.model,
		MaxTokens: 16384,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  a.baseURL,
		Body: bodyBytes,
		Headers: map[string]string{
			"x-api-key":         a.apiKey,
			"anthropic-version": "2023-06-01",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Check for HTTP errors
	status := classifyHTTPStatus(resp.StatusCode)
	if status.IsError {
		if status.AuthFailure {
			return nil, fmt.Errorf("auth failure: %s", string(respBody))
		}
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Extract text from all content blocks
	var texts []string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}

	return newStaticExecutionResult(strings.Join(texts, "\n")), nil
}
