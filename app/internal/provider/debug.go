package provider

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type debugProvider struct {
	base   Provider
	writer io.Writer
	mu     sync.Mutex
}

func WithDebugLogging(base Provider, writer io.Writer) Provider {
	if base == nil || writer == nil {
		return base
	}
	return &debugProvider{
		base:   base,
		writer: writer,
	}
}

func (p *debugProvider) Chat(ctx context.Context, messages []Message) (string, error) {
	p.logRequest(messages)
	response, err := p.base.Chat(ctx, messages)
	p.logResponse(response, err)
	return response, err
}

func (p *debugProvider) IsAvailable(ctx context.Context) (bool, error) {
	return p.base.IsAvailable(ctx)
}

func (p *debugProvider) logRequest(messages []Message) {
	var b strings.Builder
	b.WriteString("\n[LLM DEBUG] >>> Request ")
	b.WriteString(time.Now().Format(time.RFC3339))
	b.WriteString("\n")
	for i, msg := range messages {
		b.WriteString(fmt.Sprintf("[LLM DEBUG] [%d] role=%s\n", i, strings.TrimSpace(msg.Role)))
		b.WriteString("[LLM DEBUG] ")
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			b.WriteString("(empty)\n")
			continue
		}
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			b.WriteString(line)
			b.WriteString("\n[LLM DEBUG] ")
		}
		b.WriteString("\n")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = io.WriteString(p.writer, b.String())
}

func (p *debugProvider) logResponse(response string, err error) {
	var b strings.Builder
	b.WriteString("[LLM DEBUG] <<< Response ")
	b.WriteString(time.Now().Format(time.RFC3339))
	b.WriteString("\n")
	if err != nil {
		b.WriteString("[LLM DEBUG] error: ")
		b.WriteString(err.Error())
		b.WriteString("\n")
	} else {
		b.WriteString("[LLM DEBUG] ok\n")
	}
	trimmed := strings.TrimSpace(response)
	if trimmed != "" {
		b.WriteString("[LLM DEBUG] content:\n[LLM DEBUG] ")
		lines := strings.Split(trimmed, "\n")
		for _, line := range lines {
			b.WriteString(line)
			b.WriteString("\n[LLM DEBUG] ")
		}
		b.WriteString("\n")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = io.WriteString(p.writer, b.String())
}
