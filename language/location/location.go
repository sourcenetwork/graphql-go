package location

import (
	"regexp"
	"unicode/utf8"

	"github.com/sourcenetwork/graphql-go/language/source"
)

type SourceLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// GetLocation maps a byte offset to a 1-based line and column. position is a
// byte offset (matching token Start/End), but the column counts runes, so it
// matches the character column an editor shows.
func GetLocation(s *source.Source, position int) SourceLocation {
	body := []byte{}
	if s != nil {
		body = s.Body
	}
	line := 1
	lineStart := 0
	lineRegexp := regexp.MustCompile("\r\n|[\n\r]")
	matches := lineRegexp.FindAllIndex(body, -1)
	for _, match := range matches {
		matchIndex := match[0]
		if matchIndex < position {
			line++
			lineStart = match[1]
			continue
		} else {
			break
		}
	}
	// Clamp to body so a position past the end (e.g. EOF) does not panic.
	end := position
	if end > len(body) {
		end = len(body)
	}
	column := 1
	if lineStart < end {
		column = utf8.RuneCount(body[lineStart:end]) + 1
	}
	return SourceLocation{Line: line, Column: column}
}
