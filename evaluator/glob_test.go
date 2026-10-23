package evaluator_test

import (
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ryanmab/ignored/evaluator"
)

func caseName(i int) string { return "case_" + strconv.Itoa(i+1) }

func TestEmptyGlobs(t *testing.T) {
	for i, pattern := range []string{``, `# This is a comment`} {
		t.Run(caseName(i), func(t *testing.T) {
			glob, err := evaluator.NewGlob(pattern)
			if err != nil {
				t.Fatalf("NewGlob(%q) failed: %v", pattern, err)
			}
			if !glob.IsEmpty() {
				t.Fatalf("NewGlob(%q) is not empty", pattern)
			}
		})
	}
}

func TestValidVsInvalidPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		valid   bool
	}{
		{`foo\`, false},
		{`foo\ `, true},
	}
	for i, tc := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			_, err := evaluator.NewGlob(tc.pattern)
			if (err == nil) != tc.valid {
				t.Fatalf("NewGlob(%q) error = %v; want valid %v", tc.pattern, err, tc.valid)
			}
		})
	}
}

// snapshot reads the value recorded in one of the crate's snapshot files.
func snapshot(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "snapshots", name+".snap"))
	if err != nil {
		t.Fatalf("reading snapshot %s: %v", name, err)
	}
	parts := strings.SplitN(string(data), "---\n", 3)
	if len(parts) != 3 {
		t.Fatalf("snapshot %s has no header", name)
	}
	return strings.TrimSuffix(parts[2], "\n")
}

func TestGlobRegexesMatchSnapshot(t *testing.T) {
	patterns := []string{
		`build/`, `tmp/`, `vendor/`, `!vendor/keep.me`, `*.tmp`, `*.log`, `**/globfoo.txt`, `globdir/**`,
		`a/**/globbar.txt`, `/anchored.txt`, `dironly/`, `literal/file\*.txt`, `literal/file\?.txt`,
		`literal/file\[abc\].txt`, `precedence.log`, `!important.log`, `pruned/`, `!pruned/deep/keep.txt`,
		`double_negation/important.tmp`, `foo`, `file?.txt`, `file[abc].log`, `file[0-9].txt`,
	}
	for i, pattern := range patterns {
		t.Run(caseName(i), func(t *testing.T) {
			glob, err := evaluator.NewGlob(pattern)
			if err != nil {
				t.Fatalf("NewGlob(%q) failed: %v", pattern, err)
			}
			name := "ignored__evaluator__glob__tests__test_glob_regexes_match_snapshot_" + strconv.Itoa(i)
			if got, want := glob.Regex(), snapshot(t, name); got != want {
				t.Fatalf("regex of %q = %q; snapshot %s holds %q", pattern, got, name, want)
			}
		})
	}
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

func TestBuildingNeverPanics(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 2000; i++ {
		pattern := fuzzPattern(r)
		if _, err := evaluator.NewGlob(pattern); err != nil {
			t.Fatalf("failed to build glob from pattern %q: %v", pattern, err)
		}
	}
}
