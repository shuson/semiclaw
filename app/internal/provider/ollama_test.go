package provider

import "testing"

func TestParseChatResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expect  string
		wantErr bool
	}{
		{
			name:   "message content field",
			input:  `{"message":{"content":"hello"}}`,
			expect: "hello",
		},
		{
			name:   "response field",
			input:  `{"response":"fallback content"}`,
			expect: "fallback content",
		},
		{
			name:    "invalid payload",
			input:   `{"no_content":true}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := parseChatResponse([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseChatResponse() error = %v", err)
			}
			if out != tt.expect {
				t.Fatalf("parseChatResponse() = %q, want %q", out, tt.expect)
			}
		})
	}
}
