package evaluator

import (
	"strings"
	"unicode/utf8"

	"github.com/ryanmab/ignored/lexer"
)

// Glob is one pattern read from an ignore file, compiled to a regular expression. A Glob whose
// pattern is empty or a comment is empty: it matches nothing.
//
// The regular expression does not apply a leading "!": a negated Glob reports matching paths as
// not ignored.
type Glob struct {
	rx      *rustRegex
	pattern string
	negated bool
}

// InvalidPatternError reports a pattern that is not valid syntactically. Source is the lexer's
// error when the lexer rejected the pattern, nil otherwise.
type InvalidPatternError struct {
	Pattern string
	Source  *lexer.Error
}

// Error returns the crate's Display text for an invalid pattern.
func (e *InvalidPatternError) Error() string { return "Invalid pattern: " + e.Pattern }

// Unwrap returns the lexer error that rejected the pattern.
func (e *InvalidPatternError) Unwrap() error {
	if e.Source == nil {
		return nil
	}
	return e.Source
}

// InvalidRegexError reports a pattern whose translated regular expression the crate's regex
// dialect rejects. Cause is the regex compiler's own message.
type InvalidRegexError struct {
	Pattern string
	Regex   string
	Cause   string

	compile *rustRegexError
}

// Error returns the crate's Display text for a pattern whose expression does not compile.
func (e *InvalidRegexError) Error() string {
	return "Pattern (" + e.Pattern + ") did not compile to a valid regular expression: " + e.Cause
}

// NewGlob compiles one ignore-file pattern. The empty pattern gives an empty Glob.
func NewGlob(pattern string) (*Glob, error) {
	if pattern == "" {
		return &Glob{}, nil
	}
	tokens, err := lexer.Analyse(pattern)
	if err != nil {
		lexErr, _ := err.(*lexer.Error)
		return nil, &InvalidPatternError{Pattern: pattern, Source: lexErr}
	}
	return GlobFromTokens(tokens)
}

// GlobFromTokens compiles a token stream. An empty stream or a lone comment gives an empty Glob;
// a negation or comment token after the first position makes the stream invalid.
func GlobFromTokens(tokens lexer.TokenStream) (*Glob, error) {
	if len(tokens) == 0 || (len(tokens) == 1 && tokens[0].Kind == lexer.Comment) {
		return &Glob{}, nil
	}
	pattern := tokens.String()
	text, negated, ok := translate(tokens)
	if !ok {
		return nil, &InvalidPatternError{Pattern: pattern}
	}
	rx, cerr := compileRustRegex(text)
	if cerr != nil {
		return nil, &InvalidRegexError{Pattern: pattern, Regex: text, Cause: cerr.Error(), compile: cerr}
	}
	emit("TRACE", "Converted pattern: "+rustDebugStr(pattern)+" into regex: Regex("+rustDebugStr(text)+
		") (with negation: "+rustBool(negated)+")")
	return &Glob{rx: rx, pattern: pattern, negated: negated}, nil
}

const (
	sepClass     = `[\\/]`
	notSepClass  = `[^\\/]`
	anyDirsInner = `[\\/]([^\\/]+[\\/])*`
)

// translate builds the regular-expression text of a token stream.
func translate(stream lexer.TokenStream) (text string, negated bool, ok bool) {
	tokens := stream
	i := 0
	if tokens[0].Kind == lexer.Negation {
		negated = true
		i = 1
	}
	var re strings.Builder
	relative, dirOnly := false, false
	peek := func() (lexer.Token, bool) {
		if i < len(tokens) {
			return tokens[i], true
		}
		return lexer.Token{}, false
	}
	nextIf := func(kind lexer.Kind) bool {
		if t, has := peek(); has && t.Kind == kind {
			i++
			return true
		}
		return false
	}
	for i < len(tokens) {
		tok := tokens[i]
		i++
		_, hasNext := peek()
		if tok.Kind == lexer.DirectorySeparator && hasNext {
			relative = true
			if re.Len() == 0 {
				continue
			}
		} else if tok.Kind == lexer.DirectorySeparator && !hasNext {
			dirOnly = true
		}
		switch tok.Kind {
		case lexer.ExplicitLiteral:
			re.WriteString(regexEscape(tok.AsStr()))
		case lexer.ImplicitLiteral:
			literal := tok.AsStr()
			if !hasNext {
				literal = trimEndRust(literal)
			}
			re.WriteString(regexEscape(literal))
		case lexer.Range:
			re.WriteString("[" + tok.AsStr() + "]")
		case lexer.Comment, lexer.Negation:
			return "", false, false
		case lexer.Asterisk:
			re.WriteString(notSepClass + "+")
		case lexer.DoubleAsterisk:
			re.WriteString(".*")
		case lexer.DirectorySeparator:
			if nextIf(lexer.DoubleAsterisk) {
				if _, more := peek(); !more {
					re.WriteString(sepClass + ".*")
					i = len(tokens)
					break
				}
				if nextIf(lexer.DirectorySeparator) {
					re.WriteString(anyDirsInner)
				}
				break
			}
			re.WriteString(sepClass)
		case lexer.QuestionMark:
			re.WriteString(notSepClass)
		}
	}
	text = re.String()
	if relative {
		text = "^" + text
	} else {
		text = `(?:^|[\\/])` + text
	}
	if !dirOnly {
		text += `(?:$|[\\/])`
	}
	return text, negated, true
}

// regexEscape escapes the regex meta characters of s.
func regexEscape(s string) string {
	var b strings.Builder
	for _, c := range s {
		if isMetaCharacter(c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func trimEndRust(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if !isRustWhitespace(r) {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}

// IsEmpty reports whether the Glob came from an empty or comment pattern.
func (g *Glob) IsEmpty() bool { return g.rx == nil }

// IsIgnored matches path, which is taken as "" when it is not valid UTF-8. matched is false when
// the Glob is empty or does not match; otherwise ignored is true, or false for a negated Glob.
func (g *Glob) IsIgnored(path string) (ignored bool, matched bool) {
	if g.rx == nil {
		return false, false
	}
	if !utf8.ValidString(path) {
		path = ""
	}
	if !g.rx.match(path) {
		emit("TRACE", path+" did not match Some("+rustDebugStr(g.pattern)+") (via regular expression: "+
			g.rx.text+")")
		return false, false
	}
	emit("DEBUG", path+" matched Some("+rustDebugStr(g.pattern)+") (via regular expression: "+g.rx.text+
		"). Is ignored: "+rustBool(!g.negated))
	return !g.negated, true
}

// String returns the pattern the Glob was built from, or "None" for an empty Glob.
func (g *Glob) String() string {
	if g.rx == nil {
		return "None"
	}
	return g.pattern
}

// Regex returns the text of the Glob's regular expression, or "" for an empty Glob.
func (g *Glob) Regex() string {
	if g.rx == nil {
		return ""
	}
	return g.rx.text
}
