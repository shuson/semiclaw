package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAIProvider struct {
	baseURL       string
	model         string
	resolveAPIKey APIKeyResolver
	httpClient    *http.Client
}

func NewOpenAIProvider(baseURL string, model string, timeout time.Duration, resolver APIKeyResolver) *OpenAIProvider {
	trimmed := strings.TrimSuffix(baseURL, "/")
	if trimmed == "" {
		trimmed = "https://api.openai.com"
	}
	if timeout <= 0 {
		timeout = 180 * time.Second
	}

	return &OpenAIProvider{
		baseURL:       trimmed,
		model:         model,
		resolveAPIKey: resolver,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

type openAIChatRequest struct {
	Model     string           `json:"model"`
	Messages  []Message        `json:"messages"`
	Stream    bool             `json:"stream"`
	Reasoning *openAIReasoning `json:"reasoning,omitempty"`
}

type openAIReasoning struct {
	Effort string `json:"effort,omitempty"`
}

func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message) (string, error) {
	apiKey, err := p.resolveAPIKey(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve api key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)

	reqBody := openAIChatRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   false,
		Reasoning: &openAIReasoning{
			Effort: "medium",
		},
	}

	statusCode, body, err := p.doChatRequest(ctx, reqBody, apiKey)
	if err != nil {
		return "", err
	}
	if statusCode < 200 || statusCode >= 300 {
		if shouldRetryWithoutReasoning(statusCode, body) {
			reqBody.Reasoning = nil
			statusCode, body, err = p.doChatRequest(ctx, reqBody, apiKey)
			if err != nil {
				return "", err
			}
		}
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("openai-compatible chat request failed: status=%d body=%s", statusCode, string(body))
	}

	content, err := parseOpenAIChatResponse(body)
	if err != nil {
		return "", err
	}
	return content, nil
}

func (p *OpenAIProvider) doChatRequest(ctx context.Context, body openAIChatRequest, apiKey string) (int, []byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal request payload: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		if isTimeoutErr(err) {
			return 0, nil, fmt.Errorf("call openai-compatible chat endpoint timed out after %s (set OLLAMA_TIMEOUT_SECONDS higher if remote API is slow): %w", p.httpClient.Timeout.String(), err)
		}
		return 0, nil, fmt.Errorf("call openai-compatible chat endpoint: %w", err)
	}
	defer response.Body.Close()

	rawBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read response body: %w", err)
	}
	return response.StatusCode, rawBody, nil
}

func shouldRetryWithoutReasoning(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusUnprocessableEntity {
		return false
	}
	content := strings.ToLower(string(body))
	return strings.Contains(content, "reasoning") &&
		(strings.Contains(content, "unknown") ||
			strings.Contains(content, "unsupported") ||
			strings.Contains(content, "invalid"))
}

func (p *OpenAIProvider) IsAvailable(ctx context.Context) (bool, error) {
	apiKey, err := p.resolveAPIKey(ctx)
	if err != nil {
		return false, fmt.Errorf("resolve api key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)

	payload, err := json.Marshal(openAIChatRequest{
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

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("build availability request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return false, nil
	}
	defer response.Body.Close()

	return response.StatusCode >= 200 && response.StatusCode < 300, nil
}

func parseOpenAIChatResponse(data []byte) (string, error) {
	var structured struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(data, &structured); err != nil {
		return "", fmt.Errorf("decode openai-compatible response: %w", err)
	}

	if len(structured.Choices) == 0 {
		return "", fmt.Errorf("openai-compatible response did not include choices")
	}

	content := strings.TrimSpace(structured.Choices[0].Message.Content)
	if content != "" {
		return content, nil
	}

	fallback := strings.TrimSpace(structured.Choices[0].Text)
	if fallback != "" {
		return fallback, nil
	}

	return "", fmt.Errorf("openai-compatible response did not include text content")
}
