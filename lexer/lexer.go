// Package lexer performs lexical analysis on .gitignore patterns.
//
// A pattern becomes a TokenStream whose tokens reproduce the original pattern byte for byte, so
// any stream converts back into the pattern that produced it. The evaluator package compiles
// streams into regular expressions for matching paths.
package lexer

import "unicode/utf8"

// Kind identifies the type of a Token.
type Kind int

// Token kinds, in the order the lexer defines them.
const (
	// Negation is a leading "!".
	Negation Kind = iota
	// ExplicitLiteral is one byte escaped with a backslash, for example `\x`.
	ExplicitLiteral
	// ImplicitLiteral is a run of ordinary bytes, for example `hello`.
	ImplicitLiteral
	// DirectorySeparator is "/".
	DirectorySeparator
	// Asterisk is a single "*".
	Asterisk
	// DoubleAsterisk is "**".
	DoubleAsterisk
	// QuestionMark is "?".
	QuestionMark
	// Range is the body of a bracket expression, for example `a-z` in `[a-z]`.
	Range
	// Comment is the text after a leading "#".
	Comment
)

// Token is one lexical unit of a pattern. Bytes holds the payload of ExplicitLiteral,
// ImplicitLiteral, Range and Comment tokens and is nil for the other kinds.
type Token struct {
	Kind  Kind
	Bytes []byte
}

// AsStr returns the token's text: the fixed text of a symbol token, or the payload of the other
// kinds ("" when the payload is not valid UTF-8). Escapes, brackets and the comment marker are
// not included.
func (t Token) AsStr() string {
	switch t.Kind {
	case Negation:
		return "!"
	case DirectorySeparator:
		return "/"
	case Asterisk:
		return "*"
	case DoubleAsterisk:
		return "**"
	case QuestionMark:
		return "?"
	}
	if !utf8.Valid(t.Bytes) {
		return ""
	}
	return string(t.Bytes)
}

// TokenStream is the sequence of tokens lexed from one pattern.
type TokenStream []Token

// String converts the stream back into the pattern that produced it.
func (s TokenStream) String() string {
	var out []byte
	for _, t := range s {
		switch t.Kind {
		case ExplicitLiteral:
			out = append(out, '\\')
			out = append(out, t.AsStr()...)
		case Range:
			out = append(out, '[')
			out = append(out, t.AsStr()...)
			out = append(out, ']')
		case Comment:
			out = append(out, '#')
			out = append(out, t.AsStr()...)
		default:
			out = append(out, t.AsStr()...)
		}
	}
	return string(out)
}

// Error reports a pattern that is not valid syntactically: one ending in a lone backslash or
// holding an unterminated bracket expression.
type Error struct {
	Pattern string
}

// Error returns the crate's Display text for a pattern the lexer rejects.
func (e *Error) Error() string {
	return "Invalid pattern: " + e.Pattern
}

// Analyse lexes pattern byte by byte. It fails with *Error for a trailing backslash or an
// unterminated bracket expression; the empty pattern gives an empty stream.
func Analyse(pattern string) (TokenStream, error) {
	tokens := TokenStream{}
	b := []byte(pattern)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == '\\':
			if i+1 >= len(b) {
				return nil, &Error{Pattern: pattern}
			}
			i++
			tokens = append(tokens, Token{Kind: ExplicitLiteral, Bytes: []byte{b[i]}})
		case len(tokens) == 0 && c == '#':
			tokens = append(tokens, Token{Kind: Comment, Bytes: append([]byte{}, b[i+1:]...)})
			return tokens, nil
		case len(tokens) == 0 && c == '!':
			tokens = append(tokens, Token{Kind: Negation})
		case c == '/':
			tokens = append(tokens, Token{Kind: DirectorySeparator})
		case c == '?':
			tokens = append(tokens, Token{Kind: QuestionMark})
		case c == '*':
			if i+1 < len(b) && b[i+1] == '*' {
				i++
				tokens = append(tokens, Token{Kind: DoubleAsterisk})
			} else {
				tokens = append(tokens, Token{Kind: Asterisk})
			}
		case c == '[':
			body := []byte{}
			for {
				i++
				if i >= len(b) {
					return nil, &Error{Pattern: pattern}
				}
				if b[i] == ']' {
					break
				}
				body = append(body, b[i])
			}
			tokens = append(tokens, Token{Kind: Range, Bytes: body})
		default:
			if n := len(tokens); n > 0 && tokens[n-1].Kind == ImplicitLiteral {
				tokens[n-1].Bytes = append(tokens[n-1].Bytes, c)
			} else {
				tokens = append(tokens, Token{Kind: ImplicitLiteral, Bytes: []byte{c}})
			}
		}
	}
	return tokens, nil
}
