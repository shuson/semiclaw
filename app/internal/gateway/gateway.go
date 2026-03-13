package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"semiclaw/app/internal/db"
)

type ApprovalFunc func(tool string, call ToolCall) (bool, error)

type Gateway struct {
	sessions  *SessionManager
	runtime   *AgentRuntime
	executors map[string]Executor
	store     *db.Store
	approve   ApprovalFunc
	maxSteps  int
}

func New(
	store *db.Store,
	sessions *SessionManager,
	runtime *AgentRuntime,
	executors []Executor,
	approve ApprovalFunc,
) *Gateway {
	indexed := make(map[string]Executor, len(executors))
	for _, exec := range executors {
		if exec == nil {
			continue
		}
		indexed[strings.ToLower(exec.Name())] = exec
	}
	return &Gateway{
		sessions:  sessions,
		runtime:   runtime,
		executors: indexed,
		store:     store,
		approve:   approve,
		maxSteps:  6,
	}
}

type Request struct {
	OwnerScopedID     string
	AgentName         string
	SystemPrompt      string
	Event             Event
	OriginalUserInput string
	MaxSteps          int
	SkillsPrompt      string
	UserTimezone      string
	Runtime           RuntimeMetadata
}

func (g *Gateway) HandleEvent(ctx context.Context, req Request) (Result, error) {
	session, err := g.sessions.BuildSession(
		ctx,
		req.OwnerScopedID,
		req.AgentName,
		req.SystemPrompt,
		req.SkillsPrompt,
		req.UserTimezone,
		req.Runtime,
	)
	if err != nil {
		return Result{}, err
	}

	stepLimit := g.maxSteps
	if req.MaxSteps > 0 {
		stepLimit = req.MaxSteps
	}

	feedback := make([]ToolResult, 0, stepLimit)
	actions := make([]ToolCall, 0, stepLimit)

	for step := 0; step < stepLimit; step++ {
		reasoned, rawOutput, reasonErr := g.runtime.Reason(ctx, session, req.Event, feedback)
		if reasonErr != nil {
			return Result{}, reasonErr
		}

		switch reasoned.Type {
		case "final":
			finalText := strings.TrimSpace(reasoned.Message)
			if finalText == "" {
				finalText = "I could not produce a final response."
			}
			if saveErr := g.saveConversation(ctx, req.OwnerScopedID, req.userMessageForStorage(), finalText); saveErr != nil {
				return Result{}, saveErr
			}
			return Result{Response: finalText, Actions: actions, FeedbackLoop: feedback}, nil
		case "tool_call":
			if reasoned.ToolCall == nil {
				fallback := strings.TrimSpace(reasoned.Message)
				if fallback == "" {
					fallback = "I couldn't parse the tool action payload from the model response. Please retry your request."
				}
				if strings.TrimSpace(rawOutput) != "" {
					fallback = fallback + "\n\nRaw model output:\n" + strings.TrimSpace(rawOutput)
				}
				if saveErr := g.saveConversation(ctx, req.OwnerScopedID, req.userMessageForStorage(), fallback); saveErr != nil {
					return Result{}, saveErr
				}
				return Result{Response: fallback, Actions: actions, FeedbackLoop: feedback}, nil
			}
			call := *reasoned.ToolCall
			call.Tool = strings.ToLower(strings.TrimSpace(call.Tool))
			if call.Tool == "" {
				return Result{}, fmt.Errorf("tool call missing tool name")
			}
			res, runErr := g.executeToolCall(ctx, session, call)
			if runErr != nil {
				return Result{}, runErr
			}
			actions = append(actions, call)
			feedback = append(feedback, res)
		default:
			fallback := strings.TrimSpace(reasoned.Message)
			if fallback == "" {
				fallback = "I could not produce a valid response format."
			}
			if saveErr := g.saveConversation(ctx, req.OwnerScopedID, req.userMessageForStorage(), fallback); saveErr != nil {
				return Result{}, saveErr
			}
			return Result{Response: fallback, Actions: actions, FeedbackLoop: feedback}, nil
		}
	}

	final := "I reached the maximum reasoning steps before producing a final response.\nWould you like me to continue? (reply with: continue)"
	if saveErr := g.saveConversation(ctx, req.OwnerScopedID, req.userMessageForStorage(), final); saveErr != nil {
		return Result{}, saveErr
	}
	return Result{Response: final, Actions: actions, FeedbackLoop: feedback}, nil
}

func (r Request) userMessageForStorage() string {
	if strings.TrimSpace(r.OriginalUserInput) != "" {
		return r.OriginalUserInput
	}
	return r.Event.Message
}

func (g *Gateway) executeToolCall(ctx context.Context, session SessionState, call ToolCall) (ToolResult, error) {
	policy, ok := session.ToolPolicy[call.Tool]
	if !ok || !policy.Allowed {
		return ToolResult{Tool: call.Tool, Success: false, Error: "tool not allowed by policy"}, nil
	}
	exec, ok := g.executors[call.Tool]
	if !ok {
		return ToolResult{Tool: call.Tool, Success: false, Error: "tool is not configured"}, nil
	}

	if policy.RequireUserApproval && g.approve != nil {
		approved, err := g.approve(call.Tool, call)
		if err != nil {
			return ToolResult{}, err
		}
		if !approved {
			return ToolResult{Tool: call.Tool, Success: false, Error: "tool call denied by user"}, nil
		}
	}

	res, err := exec.Execute(ctx, call.Input)
	if err != nil {
		return ToolResult{Tool: call.Tool, Success: false, Error: err.Error()}, nil
	}
	return res, nil
}

func (g *Gateway) saveConversation(ctx context.Context, ownerScopedID, userMessage, assistantMessage string) error {
	if err := g.store.SaveMessage(ctx, ownerScopedID, "user", strings.TrimSpace(userMessage)); err != nil {
		return fmt.Errorf("save user message: %w", err)
	}
	if err := g.store.SaveMessage(ctx, ownerScopedID, "assistant", strings.TrimSpace(assistantMessage)); err != nil {
		return fmt.Errorf("save assistant message: %w", err)
	}
	return nil
}

func EncodeToolCall(call ToolCall) string {
	encoded, _ := json.Marshal(call)
	return string(encoded)
}
