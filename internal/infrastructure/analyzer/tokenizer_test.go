package analyzer

import (
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

func TestRegexpTokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []domain.Token
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []domain.Token{},
		},
		{
			name:  "hello world",
			input: "Hello World",
			expected: []domain.Token{
				{Value: "hello", Origin: "Hello", Position: 1, Offset: 0, Length: 5},
				{Value: "world", Origin: "World", Position: 2, Offset: 6, Length: 5},
			},
		},
		{
			name:  "text with punctuation",
			input: "Hello, World! How are you?",
			expected: []domain.Token{
				{Value: "hello", Origin: "Hello", Position: 1, Offset: 0, Length: 5},
				{Value: "world", Origin: "World", Position: 2, Offset: 7, Length: 5},
				{Value: "how", Origin: "How", Position: 3, Offset: 14, Length: 3},
				{Value: "are", Origin: "are", Position: 4, Offset: 18, Length: 3},
				{Value: "you", Origin: "you", Position: 5, Offset: 22, Length: 3},
			},
		},
		{
			name:  "cyrillic text",
			input: "Привет, мир!",
			expected: []domain.Token{
				{Value: "привет", Origin: "Привет", Position: 1, Offset: 0, Length: 12},
				{Value: "мир", Origin: "мир", Position: 2, Offset: 14, Length: 6},
			},
		},
		{
			name:  "mixed words and numbers",
			input: "Go 1.22 is awesome!",
			expected: []domain.Token{
				{Value: "go", Origin: "Go", Position: 1, Offset: 0, Length: 2},
				{Value: "1", Origin: "1", Position: 2, Offset: 3, Length: 1},
				{Value: "22", Origin: "22", Position: 3, Offset: 5, Length: 2},
				{Value: "is", Origin: "is", Position: 4, Offset: 8, Length: 2},
				{Value: "awesome", Origin: "awesome", Position: 5, Offset: 11, Length: 7},
			},
		},
		{
			name:     "only special characters",
			input:    "!@#$%^&*()",
			expected: []domain.Token{},
		},
		{
			name:  "mixed languages",
			input: "Hello world! Привет мир!",
			expected: []domain.Token{
				{Value: "hello", Origin: "Hello", Position: 1, Offset: 0, Length: 5},
				{Value: "world", Origin: "world", Position: 2, Offset: 6, Length: 5},
				{Value: "привет", Origin: "Привет", Position: 3, Offset: 13, Length: 12},
				{Value: "мир", Origin: "мир", Position: 4, Offset: 26, Length: 6},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RegexpTokenize(tt.input)

			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d tokens, got %d", len(tt.expected), len(got))
			}

			for i, token := range got {
				exp := tt.expected[i]
				if token.Value != exp.Value {
					t.Errorf("token[%d].Value = %q, want %q", i, token.Value, exp.Value)
				}
				if token.Origin != exp.Origin {
					t.Errorf("token[%d].Origin = %q, want %q", i, token.Origin, exp.Origin)
				}
				if token.Position != exp.Position {
					t.Errorf("token[%d].Position = %d, want %d", i, token.Position, exp.Position)
				}
				if token.Offset != exp.Offset {
					t.Errorf("token[%d].Offset = %d, want %d", i, token.Offset, exp.Offset)
				}
				if token.Length != exp.Length {
					t.Errorf("token[%d].Length = %d, want %d", i, token.Length, exp.Length)
				}
			}
		})
	}
}

func BenchmarkRegexpTokenize(b *testing.B) {
	text := "The quick brown fox jumps over the lazy dog. " +
		"Быстрая коричневая лиса прыгает через ленивую собаку."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RegexpTokenize(text)
	}
}
