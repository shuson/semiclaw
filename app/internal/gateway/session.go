package gateway

import (
	"context"
	"fmt"
	"strings"

	"semiclaw/app/internal/db"
	"semiclaw/app/internal/memorymd"
)

type SessionManager struct {
	store  *db.Store
	memory *memorymd.Store
}

func NewSessionManager(store *db.Store, memory *memorymd.Store) *SessionManager {
	return &SessionManager{store: store, memory: memory}
}

func DefaultToolPolicy() map[string]ToolPermission {
	return map[string]ToolPermission{
		"shell":   {Allowed: true, RequireUserApproval: true},
		"browser": {Allowed: true, RequireUserApproval: false},
		"python":  {Allowed: true, RequireUserApproval: true},
		"file":    {Allowed: true, RequireUserApproval: true},
	}
}

func ToolPolicyForMode(mode ToolPolicyMode) map[string]ToolPermission {
	switch mode {
	case ToolPolicyModeAutomationAllowAll:
		return map[string]ToolPermission{
			"shell":   {Allowed: true, RequireUserApproval: false},
			"browser": {Allowed: true, RequireUserApproval: false},
			"python":  {Allowed: true, RequireUserApproval: false},
			"file":    {Allowed: true, RequireUserApproval: false},
		}
	case ToolPolicyModeAutomationSafe:
		return map[string]ToolPermission{
			"shell":   {Allowed: false, RequireUserApproval: false},
			"browser": {Allowed: true, RequireUserApproval: false},
			"python":  {Allowed: false, RequireUserApproval: false},
			"file":    {Allowed: false, RequireUserApproval: false},
		}
	default:
		return DefaultToolPolicy()
	}
}

func (m *SessionManager) BuildSession(
	ctx context.Context,
	ownerID, agentName, systemPrompt string,
	skillsPrompt string,
	userTimezone string,
	runtime RuntimeMetadata,
	policyMode ToolPolicyMode,
) (SessionState, error) {
	history, err := m.store.GetRecentMessages(ctx, ownerID, 20)
	if err != nil {
		return SessionState{}, fmt.Errorf("load session history: %w", err)
	}

	chatHistory := make([]ChatMessage, 0, len(history))
	for _, msg := range history {
		chatHistory = append(chatHistory, ChatMessage{Role: msg.Role, Content: msg.Content})
	}

	memoryContext := ""
	if m.memory != nil {
		lt, memErr := m.memory.GetLongTerm(agentName, 2200)
		if memErr == nil {
			memoryContext = strings.TrimSpace(lt)
		}
	}

	return SessionState{
		SessionID:     ownerID,
		AgentName:     agentName,
		SystemPrompt:  strings.TrimSpace(systemPrompt),
		History:       chatHistory,
		MemoryContext: memoryContext,
		SkillsPrompt:  strings.TrimSpace(skillsPrompt),
		UserTimezone:  strings.TrimSpace(userTimezone),
		Runtime:       runtime,
		ToolPolicy:    ToolPolicyForMode(policyMode),
	}, nil
}
