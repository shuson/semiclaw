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

func TestShouldRetryWithoutThinking(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{
			name:       "unsupported think keyword",
			statusCode: 400,
			body:       `{"error":"model does not support thinking"}`,
			want:       true,
		},
		{
			name:       "unprocessable with think invalid",
			statusCode: 422,
			body:       `{"error":"invalid think option"}`,
			want:       true,
		},
		{
			name:       "bad request unrelated",
			statusCode: 400,
			body:       `{"error":"quota exceeded"}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryWithoutThinking(tt.statusCode, []byte(tt.body))
			if got != tt.want {
				t.Fatalf("shouldRetryWithoutThinking() = %v, want %v", got, tt.want)
			}
		})
	}
}
