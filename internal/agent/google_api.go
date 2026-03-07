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
var _ Agent = (*GoogleAPIAgent)(nil)

const googleDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GoogleAPIAgent implements the Agent interface using the Google Gemini generateContent API directly.
type GoogleAPIAgent struct {
	apiKey  string
	model   string
	baseURL string
}

// NewGoogleAPIAgent creates a new GoogleAPIAgent with the given API key and model.
func NewGoogleAPIAgent(apiKey, model string) *GoogleAPIAgent {
	return &GoogleAPIAgent{
		apiKey:  apiKey,
		model:   model,
		baseURL: googleDefaultBaseURL,
	}
}

// Name returns the agent's identifier.
func (a *GoogleAPIAgent) Name() string {
	return "gemini"
}

// IsAvailable checks if the API key is configured.
func (a *GoogleAPIAgent) IsAvailable() error {
	if a.apiKey == "" {
		return fmt.Errorf("GOOGLE_API_KEY is not set")
	}
	return nil
}

// ExecuteReview runs a code review using the Google Gemini generateContent API.
func (a *GoogleAPIAgent) ExecuteReview(ctx context.Context, config *ReviewConfig) (*ExecutionResult, error) {
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
	prompt := RenderPrompt(DefaultGeminiPrompt, config.Guidance)
	prompt = BuildPromptWithDiff(prompt, diff)

	return a.callAPI(ctx, prompt)
}

// ExecuteSummary runs a summarization task using the Google Gemini generateContent API.
func (a *GoogleAPIAgent) ExecuteSummary(ctx context.Context, prompt string, input []byte) (*ExecutionResult, error) {
	if err := a.IsAvailable(); err != nil {
		return nil, err
	}

	fullPrompt := prompt + "\n\nINPUT JSON:\n" + string(input) + "\n"
	return a.callAPI(ctx, fullPrompt)
}

// googleRequest is the request body for the Google Gemini generateContent API.
type googleRequest struct {
	Contents []googleContent `json:"contents"`
}

// googleContent is a single content entry in a Google API request.
type googleContent struct {
	Parts []googlePart `json:"parts"`
}

// googlePart is a single part in a Google API content entry.
type googlePart struct {
	Text string `json:"text"`
}

// googleResponse is the response body from the Google Gemini generateContent API.
type googleResponse struct {
	Candidates []googleCandidate `json:"candidates"`
}

// googleCandidate is a single candidate in a Google API response.
type googleCandidate struct {
	Content googleContent `json:"content"`
}

// callAPI sends a prompt to the Google Gemini generateContent API and returns the result.
func (a *GoogleAPIAgent) callAPI(ctx context.Context, prompt string) (*ExecutionResult, error) {
	reqBody := googleRequest{
		Contents: []googleContent{
			{
				Parts: []googlePart{
					{Text: prompt},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Google API uses API key as query param, not as a header
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", a.baseURL, a.model, a.apiKey)

	resp, err := doAPIRequest(ctx, apiRequestConfig{
		URL:  url,
		Body: bodyBytes,
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
	var apiResp googleResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Extract text from all parts across all candidates
	var texts []string
	for _, candidate := range apiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			texts = append(texts, part.Text)
		}
	}

	return newStaticExecutionResult(strings.Join(texts, "\n")), nil
}
