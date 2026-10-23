package lexer_test

import (
	"errors"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ryanmab/ignored/lexer"
)

func lit(s string) lexer.Token  { return lexer.Token{Kind: lexer.ImplicitLiteral, Bytes: []byte(s)} }
func esc(s string) lexer.Token  { return lexer.Token{Kind: lexer.ExplicitLiteral, Bytes: []byte(s)} }
func rng(s string) lexer.Token  { return lexer.Token{Kind: lexer.Range, Bytes: []byte(s)} }
func note(s string) lexer.Token { return lexer.Token{Kind: lexer.Comment, Bytes: []byte(s)} }

var (
	neg  = lexer.Token{Kind: lexer.Negation}
	sep  = lexer.Token{Kind: lexer.DirectorySeparator}
	star = lexer.Token{Kind: lexer.Asterisk}
	dbl  = lexer.Token{Kind: lexer.DoubleAsterisk}
	qm   = lexer.Token{Kind: lexer.QuestionMark}
)

func TestLexing(t *testing.T) {
	cases := []struct {
		pattern string
		want    lexer.TokenStream
		invalid bool
	}{
		{pattern: `hello`, want: lexer.TokenStream{lit("hello")}},
		{pattern: `hello\world`, want: lexer.TokenStream{lit("hello"), esc("w"), lit("orld")}},
		{pattern: `hello\`, invalid: true},
		{pattern: `!foo`, want: lexer.TokenStream{neg, lit("foo")}},
		{pattern: `\!foo`, want: lexer.TokenStream{esc("!"), lit("foo")}},
		{pattern: `fo!o`, want: lexer.TokenStream{lit("fo!o")}},
		{pattern: `!fo!o`, want: lexer.TokenStream{neg, lit("fo!o")}},
		{pattern: `hello/world/`, want: lexer.TokenStream{lit("hello"), sep, lit("world"), sep}},
		{pattern: `hello/?/world/`, want: lexer.TokenStream{lit("hello"), sep, qm, sep, lit("world"), sep}},
		{pattern: `**`, want: lexer.TokenStream{dbl}},
		{pattern: `/foo/**/*bar`, want: lexer.TokenStream{sep, lit("foo"), sep, dbl, sep, star, lit("bar")}},
		{pattern: `/*****`, want: lexer.TokenStream{sep, dbl, dbl, star}},
		{pattern: `hello/[a-zA-Z0-9]/world`, want: lexer.TokenStream{lit("hello"), sep, rng("a-zA-Z0-9"), sep, lit("world")}},
		{pattern: `hello/[a-/world`, invalid: true},
		{pattern: `/hello/[a-zA-Z0-9]/world/`, want: lexer.TokenStream{sep, lit("hello"), sep, rng("a-zA-Z0-9"), sep, lit("world"), sep}},
		{pattern: `# Hello World`, want: lexer.TokenStream{note(" Hello World")}},
		{pattern: `!deep_keep.log`, want: lexer.TokenStream{neg, lit("deep_keep.log")}},
		{pattern: `foo `, want: lexer.TokenStream{lit("foo ")}},
		{pattern: `foo \ `, want: lexer.TokenStream{lit("foo "), esc(" ")}},
		{pattern: ``, want: lexer.TokenStream{}},
		{pattern: `   `, want: lexer.TokenStream{lit("   ")}},
		{pattern: "file\u00e9.txt", want: lexer.TokenStream{lit("file\xc3\xa9.txt")}},
	}
	for i, tc := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			got, err := lexer.Analyse(tc.pattern)
			if tc.invalid {
				var lexErr *lexer.Error
				if !errors.As(err, &lexErr) || lexErr.Pattern != tc.pattern {
					t.Fatalf("Analyse(%q) = %v, %v; want *lexer.Error for the pattern", tc.pattern, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Analyse(%q) failed: %v", tc.pattern, err)
			}
			if len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("Analyse(%q) = %#v; want %#v", tc.pattern, got, tc.want)
			}
			if got.String() != tc.pattern {
				t.Fatalf("round trip of %q gave %q", tc.pattern, got.String())
			}
		})
	}
}

func caseName(i int) string {
	return "case_" + strconv.Itoa(i+1)
}

const fuzzLiteral = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"

// fuzzPattern draws a pattern from the crate's gitignore fuzzing strategy.
func fuzzPattern(r *rand.Rand) string {
	components := []string{"*", "**", "?", "[abc]", "[0-9]", `\!`, `\#`, `\ `, `\\`}
	var parts []string
	for n := 1 + r.Intn(9); n > 0; n-- {
		if r.Intn(len(components)+1) == 0 {
			var b strings.Builder
			for l := 1 + r.Intn(49); l > 0; l-- {
				b.WriteByte(fuzzLiteral[r.Intn(len(fuzzLiteral))])
			}
			parts = append(parts, b.String())
		} else {
			parts = append(parts, components[r.Intn(len(components))])
		}
	}
	s := strings.Join(parts, "")
	switch r.Intn(7) {
	case 0:
		s = "# " + s
	case 1:
		s = "!" + s
	case 2:
		s = "/" + s
	}
	if r.Intn(2) == 0 {
		s += "/"
	}
	return s
}

func TestLexingNeverPanics(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		pattern := fuzzPattern(r)
		if _, err := lexer.Analyse(pattern); err != nil {
			t.Fatalf("failed to lex pattern %q: %v", pattern, err)
		}
	}
}
