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

func TestShouldRetryWithoutReasoning(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{
			name:       "unsupported reasoning wording",
			statusCode: 400,
			body:       `{"error":"model does not support reasoning"}`,
			want:       true,
		},
		{
			name:       "unprocessable invalid reasoning",
			statusCode: 422,
			body:       `{"error":"invalid reasoning field"}`,
			want:       true,
		},
		{
			name:       "unrelated error",
			statusCode: 400,
			body:       `{"error":"quota exceeded"}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryWithoutReasoning(tt.statusCode, []byte(tt.body))
			if got != tt.want {
				t.Fatalf("shouldRetryWithoutReasoning() = %v, want %v", got, tt.want)
			}
		})
	}
}
