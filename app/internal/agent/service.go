package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"semiclaw/app/internal/db"
	"semiclaw/app/internal/provider"
)

const DefaultSystemPrompt = `You are Semiclaw, a general purpose AI agent.
Be practical, clear, and action-oriented.
Prefer short responses unless the user asks for details.`

type Service struct {
	store *db.Store
}

func NewService(store *db.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Chat(
	ctx context.Context,
	userID string,
	basePrompt string,
	message string,
	modelProvider provider.Provider,
) (string, error) {
	systemPrompt, err := s.buildSystemPrompt(ctx, basePrompt)
	if err != nil {
		return "", fmt.Errorf("build system prompt: %w", err)
	}

	history, err := s.store.GetRecentMessages(ctx, userID, 20)
	if err != nil {
		return "", fmt.Errorf("load message history: %w", err)
	}

	conversation := make([]provider.Message, 0, len(history)+2)
	conversation = append(conversation, provider.Message{Role: "system", Content: systemPrompt})
	for _, record := range history {
		conversation = append(conversation, provider.Message{Role: record.Role, Content: record.Content})
	}
	conversation = append(conversation, provider.Message{Role: "user", Content: message})

	response, err := s.runRefinementLoop(ctx, modelProvider, conversation, message)
	if err != nil {
		return "", err
	}

	if err := s.store.SaveMessage(ctx, userID, "user", message); err != nil {
		return "", fmt.Errorf("save user message: %w", err)
	}
	if err := s.store.SaveMessage(ctx, userID, "assistant", response); err != nil {
		return "", fmt.Errorf("save assistant message: %w", err)
	}

	return response, nil
}

func (s *Service) runRefinementLoop(
	ctx context.Context,
	modelProvider provider.Provider,
	conversation []provider.Message,
	originalUserMessage string,
) (string, error) {
	const maxRounds = 4

	working := append([]provider.Message(nil), conversation...)
	finalResponse := ""
	for round := 1; round <= maxRounds; round++ {
		response, err := modelProvider.Chat(ctx, working)
		if err != nil {
			return "", fmt.Errorf("provider chat: %w", err)
		}
		response = strings.TrimSpace(response)
		if response == "" {
			response = "I could not generate a response right now. Please try again."
		}
		finalResponse = response

		isFinal, feedback := evaluateResponseFinality(ctx, modelProvider, originalUserMessage, response)
		if isFinal || round == maxRounds {
			break
		}

		working = append(working, provider.Message{Role: "assistant", Content: response})
		working = append(working, provider.Message{
			Role:    "user",
			Content: buildRefinementPrompt(originalUserMessage, feedback, round+1, maxRounds),
		})
	}
	return finalResponse, nil
}

type responseFinality struct {
	Final    bool   `json:"final"`
	Feedback string `json:"feedback"`
}

func evaluateResponseFinality(
	ctx context.Context,
	modelProvider provider.Provider,
	originalRequest string,
	candidate string,
) (bool, string) {
	judgePrompt := `You are a strict response-quality judge.
Decide whether the candidate answer fully and correctly addresses the user request.
Return JSON only:
{"final":true|false,"feedback":"short actionable improvement note"}
Rules:
- final=true only when the answer is complete, directly useful, and not missing obvious requested parts.
- If final=true, feedback can be empty.
- If final=false, feedback must be specific and concise.
- Never include markdown fences or explanations outside JSON.`

	raw, err := modelProvider.Chat(ctx, []provider.Message{
		{Role: "system", Content: judgePrompt},
		{
			Role: "user",
			Content: "User request:\n" + strings.TrimSpace(originalRequest) +
				"\n\nCandidate answer:\n" + strings.TrimSpace(candidate),
		},
	})
	if err != nil {
		return true, ""
	}
	out, parseErr := parseResponseFinality(raw)
	if parseErr != nil {
		return true, ""
	}
	return out.Final, strings.TrimSpace(out.Feedback)
}

func parseResponseFinality(raw string) (responseFinality, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return responseFinality{}, fmt.Errorf("empty finality response")
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		trimmed = trimmed[start : end+1]
	}
	var out responseFinality
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return responseFinality{}, fmt.Errorf("parse finality json: %w", err)
	}
	return out, nil
}

func buildRefinementPrompt(originalRequest string, feedback string, nextRound int, maxRounds int) string {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		feedback = "Tighten correctness and completeness."
	}
	return fmt.Sprintf(
		"Revise your previous answer and produce an improved final answer.\nOriginal request:\n%s\n\nRequired improvements:\n- %s\n\nRound: %d/%d. Return only the improved answer.",
		strings.TrimSpace(originalRequest),
		feedback,
		nextRound,
		maxRounds,
	)
}

func (s *Service) buildSystemPrompt(ctx context.Context, basePrompt string) (string, error) {
	prompt := strings.TrimSpace(basePrompt)
	if prompt == "" {
		prompt = DefaultSystemPrompt
	}

	profileFields := []struct {
		key   string
		label string
	}{
		{key: "user.profile.name", label: "Name"},
		{key: "user.profile.role", label: "Role"},
		{key: "user.profile.location", label: "Location"},
		{key: "user.profile.goals", label: "Goals"},
		{key: "user.profile.response_style", label: "Preferred response style"},
		{key: "user.profile.notes", label: "Additional notes"},
	}

	var lines []string
	for _, field := range profileFields {
		value, ok, err := s.store.GetConfig(ctx, field.key)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if !ok || value == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", field.label, value))
	}

	if len(lines) == 0 {
		return prompt, nil
	}

	prompt += "\n\nUse this user profile context when relevant:"
	prompt += "\n" + strings.Join(lines, "\n")
	return prompt, nil
}
