package provider

import "testing"

func TestParseOpenAIChatResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expect  string
		wantErr bool
	}{
		{
			name:   "message content",
			input:  `{"choices":[{"message":{"content":"hello"}}]}`,
			expect: "hello",
		},
		{
			name:   "text fallback",
			input:  `{"choices":[{"text":"fallback content"}]}`,
			expect: "fallback content",
		},
		{
			name:    "missing choices",
			input:   `{"choices":[]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := parseOpenAIChatResponse([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseOpenAIChatResponse() error = %v", err)
			}
			if out != tt.expect {
				t.Fatalf("parseOpenAIChatResponse() = %q, want %q", out, tt.expect)
			}
		})
	}
}
