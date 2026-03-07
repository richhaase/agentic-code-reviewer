package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
)

// apiRequestConfig holds the configuration for an API HTTP request.
type apiRequestConfig struct {
	URL     string
	Body    []byte
	Headers map[string]string
}

// doAPIRequest sends a POST request with a JSON body and custom headers.
// It uses http.NewRequestWithContext for cancellation support.
// The caller is responsible for closing the response body.
func doAPIRequest(ctx context.Context, cfg apiRequestConfig) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(cfg.Body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	return http.DefaultClient.Do(req)
}

// statusClassification categorizes an HTTP status code.
type statusClassification struct {
	AuthFailure bool
	Retryable   bool
	IsError     bool
}

// classifyHTTPStatus categorizes an HTTP status code into auth failure,
// retryable, or general error.
func classifyHTTPStatus(statusCode int) statusClassification {
	switch {
	case statusCode >= 200 && statusCode <= 299:
		return statusClassification{}
	case statusCode == 401 || statusCode == 403:
		return statusClassification{AuthFailure: true, IsError: true}
	case statusCode == 429 || statusCode >= 500:
		return statusClassification{Retryable: true, IsError: true}
	default:
		return statusClassification{IsError: true}
	}
}

// newStaticExecutionResult creates an ExecutionResult from a string.
// Used by API agents where the response body is already fully read.
func newStaticExecutionResult(text string) *ExecutionResult {
	reader := io.NopCloser(bytes.NewReader([]byte(text)))
	return NewExecutionResult(reader, func() int { return 0 }, func() string { return "" })
}
