package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type APIKeyResolver func(ctx context.Context) (string, error)

type OllamaProvider struct {
	baseURL       string
	model         string
	resolveAPIKey APIKeyResolver
	httpClient    *http.Client
}

func NewOllamaProvider(baseURL string, model string, timeout time.Duration, resolver APIKeyResolver) *OllamaProvider {
	trimmed := strings.TrimSuffix(baseURL, "/")
	if trimmed == "" {
		trimmed = "https://ollama.com"
	}
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	return &OllamaProvider{
		baseURL:       trimmed,
		model:         model,
		resolveAPIKey: resolver,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

type ollamaChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Think    any       `json:"think,omitempty"`
}

func (p *OllamaProvider) Chat(ctx context.Context, messages []Message) (string, error) {
	apiKey, err := p.resolveAPIKey(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve api key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)

	reqBody := ollamaChatRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   false,
		Think:    true,
	}

	statusCode, body, err := p.doChatRequest(ctx, reqBody, apiKey)
	if err != nil {
		return "", err
	}
	if statusCode < 200 || statusCode >= 300 {
		if shouldRetryWithoutThinking(statusCode, body) {
			reqBody.Think = nil
			statusCode, body, err = p.doChatRequest(ctx, reqBody, apiKey)
			if err != nil {
				return "", err
			}
		}
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("ollama chat request failed: status=%d body=%s", statusCode, string(body))
	}

	content, err := parseChatResponse(body)
	if err != nil {
		return "", err
	}
	return content, nil
}

func (p *OllamaProvider) doChatRequest(ctx context.Context, body ollamaChatRequest, apiKey string) (int, []byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal request payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(request)
	if err != nil {
		if isTimeoutErr(err) {
			return 0, nil, fmt.Errorf("call ollama chat endpoint timed out after %s (set OLLAMA_TIMEOUT_SECONDS higher if model warm-up is slow): %w", p.httpClient.Timeout.String(), err)
		}
		return 0, nil, fmt.Errorf("call ollama chat endpoint: %w", err)
	}
	defer response.Body.Close()

	rawBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read response body: %w", err)
	}
	return response.StatusCode, rawBody, nil
}

func shouldRetryWithoutThinking(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusUnprocessableEntity {
		return false
	}
	content := strings.ToLower(string(body))
	return strings.Contains(content, "think") &&
		(strings.Contains(content, "unknown") ||
			strings.Contains(content, "unsupported") ||
			strings.Contains(content, "invalid"))
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (p *OllamaProvider) IsAvailable(ctx context.Context) (bool, error) {
	apiKey, err := p.resolveAPIKey(ctx)
	if err != nil {
		return false, fmt.Errorf("resolve api key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)

	payload, err := json.Marshal(ollamaChatRequest{
		Model: p.model,
		Messages: []Message{{
			Role:    "user",
			Content: "ping",
		}},
		Stream: false,
	})
	if err != nil {
		return false, fmt.Errorf("marshal availability payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("build availability request: %w", err)
	}
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return false, nil
	}
	defer response.Body.Close()

	return response.StatusCode >= 200 && response.StatusCode < 300, nil
}

func parseChatResponse(data []byte) (string, error) {
	var structured struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Response string `json:"response"`
		Content  string `json:"content"`
	}

	if err := json.Unmarshal(data, &structured); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}

	switch {
	case strings.TrimSpace(structured.Message.Content) != "":
		return structured.Message.Content, nil
	case strings.TrimSpace(structured.Response) != "":
		return structured.Response, nil
	case strings.TrimSpace(structured.Content) != "":
		return structured.Content, nil
	default:
		return "", errors.New("ollama response did not include text content")
	}
}
