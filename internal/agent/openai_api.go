package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/richhaase/agentic-code-reviewer/internal/git"
)

// Compile-time interface check.
var _ Agent = (*OpenAIAPIAgent)(nil)

const openaiDefaultBaseURL = "https://api.openai.com/v1/chat/completions"

// OpenAIAPIAgent implements the Agent interface using the OpenAI Chat Completions API directly.
type OpenAIAPIAgent struct {
	apiKey  string
	model   string
	baseURL string
}

// NewOpenAIAPIAgent creates a new OpenAIAPIAgent with the given API key and model.
func NewOpenAIAPIAgent(apiKey, model string) *OpenAIAPIAgent {
	return &OpenAIAPIAgent{
		apiKey:  apiKey,
		model:   model,
		baseURL: openaiDefaultBaseURL,
	}
}

// Name returns the agent's identifier.
func (a *OpenAIAPIAgent) Name() string {
	return "codex"
}

// IsAvailable checks if the API key is configured.
func (a *OpenAIAPIAgent) IsAvailable() error {
	if a.apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is not set")
	}
	return nil
}

// ExecuteReview runs a code review using the OpenAI Chat Completions API.
func (a *OpenAIAPIAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
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

// ExecuteSummary runs a summarization task using the OpenAI Chat Completions API.
func (a *OpenAIAPIAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	return a.callAPI(ctx, fullPrompt)
}

// openaiRequest is the request body for the OpenAI Chat Completions API.
type openaiRequest struct {
	Model    string           `json:"model"`
	Messages []openaiMessage  `json:"messages"`
}

// openaiMessage is a single message in an OpenAI API request.
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponse is the response body from the OpenAI Chat Completions API.
type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
}

// openaiChoice is a single choice in an OpenAI API response.
type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

// callAPI sends a prompt to the OpenAI Chat Completions API and returns the result.
func (a *OpenAIAPIAgent) callAPI(ctx context.Context, prompt string) (*ExecutionResult, error) {
	reqBody := openaiRequest{
		Model: a.model,
		Messages: []openaiMessage{
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
			"Authorization": "Bearer " + a.apiKey,
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
	var apiResp openaiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Extract text from the first choice
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in API response")
	}

	return newStaticExecutionResult(apiResp.Choices[0].Message.Content), nil
}
