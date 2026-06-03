package location

import (
	"testing"

	"github.com/sourcenetwork/graphql-go/language/source"
)

func TestGetLocation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		position int // byte offset
		expected SourceLocation
	}{
		{
			name:     "start of input",
			body:     "foo",
			position: 0,
			expected: SourceLocation{Line: 1, Column: 1},
		},
		{
			name:     "ascii column counts characters",
			body:     "foo bar",
			position: 4,
			expected: SourceLocation{Line: 1, Column: 5},
		},
		{
			name:     "second line resets column",
			body:     "foo\nbar",
			position: 4, // 'b'
			expected: SourceLocation{Line: 2, Column: 1},
		},
		{
			// The 3-byte em-dash before 'b' counts as one column, not three.
			name:     "multi-byte rune counts as one column",
			body:     "a — b", // 'b' is byte 6, char column 5
			position: 6,
			expected: SourceLocation{Line: 1, Column: 5},
		},
		{
			// Position past the end (e.g. EOF) must not panic.
			name:     "position past end of body",
			body:     "ab",
			position: 5,
			expected: SourceLocation{Line: 1, Column: 3},
		},
		{
			name:     "nil-safe with empty body",
			body:     "",
			position: 0,
			expected: SourceLocation{Line: 1, Column: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &source.Source{Body: []byte(tt.body)}
			got := GetLocation(s, tt.position)
			if got != tt.expected {
				t.Errorf("GetLocation(%q, %d) = %+v, want %+v",
					tt.body, tt.position, got, tt.expected)
			}
		})
	}
}
