package provider

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Provider interface {
	Chat(ctx context.Context, messages []Message) (string, error)
	IsAvailable(ctx context.Context) (bool, error)
}
