package omnifocus

import "testing"

func TestApplescriptQuoteString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  `""`,
		},
		{
			name:  "plain ascii",
			input: "hello world",
			want:  `"hello world"`,
		},
		{
			name:  "embedded double quote",
			input: `she said "hi"`,
			want:  `"she said \"hi\""`,
		},
		{
			name:  "embedded backslash",
			input: `path\to\thing`,
			want:  `"path\\to\\thing"`,
		},
		{
			name:  "backslash then quote",
			input: `\"`,
			want:  `"\\\""`,
		},
		{
			name:  "unicode passes through unchanged",
			input: "look 👀 here",
			want:  `"look 👀 here"`,
		},
		{
			name:  "newlines pass through unchanged",
			input: "line one\nline two",
			want:  "\"line one\nline two\"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applescriptQuoteString(tc.input)
			if got != tc.want {
				t.Errorf("applescriptQuoteString(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
