package main
import (
	"fmt"
	"regexp"
	"strings"
	"errors"
)

const (
	fnBodyStart = "FN_BODY_START"
	fnBodyEnd = "FN_BODY_END"
	fnArgsStart = "FN_ARGS_START"
	fnArgsEnd = "FN_ARGS_END"
	fnName = "FN_NAME"
	strLit = "STRING_LIT"
	keyword = "KEYWORD"
	fnKeyword = "fn"
)

var patterns = [][]string {
	{fnKeyword, keyword}, // TODO: add other keywords
	{"\"[\\s!\\w]*\"", strLit},
	{"[a-zA-Z]+", fnName},
	{"\\{", fnBodyStart},
	{"\\}", fnBodyEnd},
	{"\\(", fnArgsStart},
	{"\\)", fnArgsEnd},
	{"\\r?\\n", "NEWLINE"},
	{"\\s+", "WHITESPACE"},
}

func lex(prog string) ([]*Token, error) {

	// Build regex parts
	var parts []string
	for _, p := range patterns {
		parts = append(parts, fmt.Sprintf("(?P<%s>%s)", p[1], p[0]))
	}

	// Build regex
	regex := regexp.MustCompile(strings.Join(parts, "|"))
	names := regex.SubexpNames()

	// Track progress
	pos := 0
	line := 1
	linePos := 0
	s := prog
	var tokens []*Token
	for pos != len(prog) {
		res := regex.FindStringSubmatchIndex(s)
		if res[0] != 0 {
			// Failed to match some input - return error
			return nil, errors.New(fmt.Sprintf("Failed to match (%s) against any rule!", s[0:res[0]]))
		}

		// Get value and type
		for i := 2; i < len(res); i+=2 {
			if res[i] == -1 {
				continue
			}

			// Create token (skip whitespace and newlines)
			kind := names[i/2]
			val := s[res[i]:res[i+1]]
			if kind != "NEWLINE" && kind != "WHITESPACE" {
				t := &Token{kind, val, line, pos - linePos}
				tokens = append(tokens, t)
			}

			// Update state
			if kind == "NEWLINE" {
				line++
				linePos = pos
			}
			pos += res[i+1]
			s = s[res[i+1]:]
			break
		}
	}

	return tokens, nil
}

type Token struct {
	kind string
	val string
	line int
	pos int
}

func (t *Token) String() string {
	return fmt.Sprintf("%-14s[%v:%-2v] => '%s'", t.kind, t.line, t.pos, t.val)
}