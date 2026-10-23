package evaluator

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func caseName(i int) string { return "case_" + strconv.Itoa(i+1) }

func writeGitConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setEnv sets or, for an empty value with set false, unsets an environment variable for the test.
func setEnv(t *testing.T, key, value string, set bool) {
	t.Setenv(key, value)
	if !set {
		os.Unsetenv(key)
	}
}

type envKey struct {
	key string
	set bool
}

func TestParsingExcludesFileFromConfig(t *testing.T) {
	type tc struct {
		home, xdg *string
		env       []envKey
		want      string
	}
	s := func(v string) *string { return &v }
	cases := []tc{
		{s("[core]\n\texcludesfile = /home/excludes\n"), nil,
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", false}}, "/home/excludes"},
		{nil, s("[core]\n\texcludesfile = /xdg/excludes\n"),
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}, "/xdg/excludes"},
		{s("[core]\n\texcludesfile = /home/excludes\n"), s("[core]\n\texcludesfile = /xdg/excludes\n"),
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}, "/xdg/excludes"},
		{s("[core]\n\texcludesfile = /home/excludes\n"), s("[core]\n\texcludesfile = /xdg/excludes\n"),
			[]envKey{{"HOME", true}, {"XDG_CONFIG_HOME", true}, {"USERPROFILE", false}}, "/xdg/excludes"},
		{s("[CORE]\n# comment\n excludesfile   =   \"/home/excludes\"  \n"), s("[core]\n\texcludesfile=/xdg/excludes\n"),
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}, "/xdg/excludes"},
		{nil, s("[core]\n  excludesfile   =   /xdg/excludes   \n"),
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}, "/xdg/excludes"},
		{s("[core]\nexcludesfile = /first/path\n[core]\nexcludesfile = \"/second/path\"\n"), nil,
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", false}}, "/second/path"},
		{s("[core]\n\texcludesfile\t=\t/path/with/spaces\n"), nil,
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", false}}, "/path/with/spaces"},
	}
	for i, c := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			home, xdg := t.TempDir(), t.TempDir()
			for _, e := range c.env {
				value := home
				if e.key == "XDG_CONFIG_HOME" {
					value = xdg
				}
				setEnv(t, e.key, value, e.set)
			}
			if c.home != nil {
				writeGitConfig(t, filepath.Join(home, ".config", "git", "config"), *c.home)
			}
			if c.xdg != nil {
				writeGitConfig(t, filepath.Join(xdg, "git", "config"), *c.xdg)
			}
			path, ok, err := NewEvaluator().globalExcludePath()
			if err != nil || !ok || path != c.want {
				t.Fatalf("global excludes path = %q, %v, %v; want %q", path, ok, err, c.want)
			}
		})
	}
}

func TestHandlesDefaultsWhenExcludesFileIsNotSetInConfig(t *testing.T) {
	type tc struct {
		home, xdg *string
		env       []envKey
	}
	s := func(v string) *string { return &v }
	cases := []tc{
		{nil, nil, []envKey{{"HOME", false}, {"USERPROFILE", false}, {"XDG_CONFIG_HOME", false}}},
		{nil, nil, []envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", false}}},
		{nil, nil, []envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}},
		{s("[CORE]\n# comment\n"), nil, []envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", false}}},
		{nil, s("[CORE]\n# comment\n"), []envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}},
		{s("[CORE]\n# comment\n"), s("[CORE]\n# comment\n"),
			[]envKey{{"HOME", true}, {"USERPROFILE", true}, {"XDG_CONFIG_HOME", true}}},
		{nil, nil, []envKey{{"HOME", false}, {"USERPROFILE", false}, {"XDG_CONFIG_HOME", true}}},
	}
	for i, c := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			home, xdg := t.TempDir(), t.TempDir()
			for _, e := range c.env {
				value := home
				if e.key == "XDG_CONFIG_HOME" {
					value = xdg
				}
				setEnv(t, e.key, value, e.set)
			}
			if c.home != nil {
				writeGitConfig(t, filepath.Join(home, ".config", "git", "config"), *c.home)
			}
			if c.xdg != nil {
				writeGitConfig(t, filepath.Join(xdg, "git", "config"), *c.xdg)
			}
			path, ok, err := NewEvaluator().globalExcludePath()
			if err != nil || !ok || !strings.HasSuffix(path, "git/ignore") {
				t.Fatalf("global excludes path = %q, %v, %v; want the default ending in git/ignore", path, ok, err)
			}
		})
	}
}

func TestDoesntReparseConfigOnRepeatedCalls(t *testing.T) {
	xdg := t.TempDir()
	configPath := filepath.Join(xdg, "git", "config")
	writeGitConfig(t, configPath, "[core]\n\texcludesfile = /some/path/read/from/disk\n")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	ev := NewEvaluator()
	ev.configs[configPath] = &configFile{exclude: "/some/path/not/read/from/disk", hasExclude: true, checksum: sum[:]}
	path, ok, err := ev.globalExcludePath()
	if err != nil || !ok || path != "/some/path/not/read/from/disk" {
		t.Fatalf("global excludes path = %q, %v, %v; want the cached path", path, ok, err)
	}
}

func TestGetClosestAlreadyEncounteredGitRoot(t *testing.T) {
	cases := []struct {
		roots []string
		path  string
		want  string
	}{
		{nil, "some/path/", ""},
		{[]string{"some/path/"}, "some/path/here/1", "some/path/"},
		{[]string{"some/path/here/", "some/path/"}, "some/path/here/1", "some/path/here/"},
		{[]string{"some/path/", "some/path/here/"}, "some/path/here/1", "some/path/here/"},
		{[]string{"some/path/", "some/path/here/"}, "different/parent/some/path/here/1", ""},
		{[]string{"some/path/", "some/path/here/"}, "unrelated/path", ""},
		{[]string{"some/path/", "some/path/here/"}, "some/", ""},
	}
	for i, c := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			ev := NewEvaluator()
			ev.roots = c.roots
			got, ok := ev.closestRoot(c.path)
			if ok != (c.want != "") || got != c.want {
				t.Fatalf("closest root of %q = %q, %v; want %q", c.path, got, ok, c.want)
			}
		})
	}
}

// parseConfig reads core.excludesfile from a config file written with contents.
func parseConfig(t *testing.T, contents string) (string, bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	writeGitConfig(t, path, contents)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := readGitConfig(path, data, nil)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return cf.exclude, cf.hasExclude
}

func TestParsesExcludesfileCorrectly(t *testing.T) {
	cases := []struct {
		template string
		hasPath  bool
	}{
		{"[core]\n\texcludesfile = {path}\n", true},
		{"[core]\n", false},
		{"", false},
	}
	for i, c := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			exclude := filepath.Join(t.TempDir(), "ignore")
			got, ok := parseConfig(t, strings.ReplaceAll(c.template, "{path}", exclude))
			if ok != c.hasPath || (c.hasPath && got != exclude) {
				t.Fatalf("excludesfile = %q, %v; want %q, %v", got, ok, exclude, c.hasPath)
			}
		})
	}
}

func TestLastMatchWins(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	contents := fmt.Sprintf("\n            [core]\n                excludesfile = %s\n            [core]\n"+
		"                excludesfile = %s\n            ", first, second)
	if got, ok := parseConfig(t, contents); !ok || got != second {
		t.Fatalf("excludesfile = %q, %v; want %q", got, ok, second)
	}
}

func TestTolerantToWhitespace(t *testing.T) {
	templates := []string{"excludesfile={path}", "excludesfile = {path}", "excludesfile=    {path}", "   excludesfile = {path}"}
	for i, template := range templates {
		t.Run(caseName(i), func(t *testing.T) {
			exclude := filepath.Join(t.TempDir(), "ignore")
			line := strings.ReplaceAll(template, "{path}", exclude)
			if got, ok := parseConfig(t, "[core]\n"+line+"\n"); !ok || got != exclude {
				t.Fatalf("excludesfile = %q, %v; want %q", got, ok, exclude)
			}
		})
	}
}

const configPathChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/-"

func randomConfigPath(r *rand.Rand, max int) string {
	var b strings.Builder
	for n := 1 + r.Intn(max); n > 0; n-- {
		b.WriteByte(configPathChars[r.Intn(len(configPathChars))])
	}
	return b.String()
}

func TestFuzzParsingDoesNotPanic(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 2000; i++ {
		path := randomConfigPath(r, 50)
		include := r.Intn(2) == 0
		contents := "\n                    [user]\n                        name = test\n                "
		if include {
			contents = "\n                    [core]\n                        excludesfile = " + path + "\n                    "
		}
		got, ok := parseConfig(t, contents)
		if ok != include || (include && got != path) {
			t.Fatalf("excludesfile of %q = %q, %v", contents, got, ok)
		}
	}
}

func TestFuzzLastMatchWins(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for i := 0; i < 2000; i++ {
		var paths []string
		contents := "[core]\n"
		for n := 1 + r.Intn(4); n > 0; n-- {
			p := randomConfigPath(r, 30)
			paths = append(paths, p)
			contents += "excludesfile = " + p + "\n"
		}
		want := paths[len(paths)-1]
		if got, ok := parseConfig(t, contents); !ok || got != want {
			t.Fatalf("excludesfile of %q = %q, %v; want %q", contents, got, ok, want)
		}
	}
}

func TestParsesTypicalLinuxGitconfig(t *testing.T) {
	contents := "\n        [user]\n            name = Jane Doe\n            email = jane@example.com\n        [core]\n" +
		"            excludesfile = ~/.config/git/ignore\n        [init]\n            defaultBranch = main\n        "
	if got, ok := parseConfig(t, contents); !ok || got != "~/.config/git/ignore" {
		t.Fatalf("excludesfile = %q, %v", got, ok)
	}
}

func TestParsesTypicalWindowsGitconfig(t *testing.T) {
	contents := "\n        [core]\n            excludesfile = C:\\\\Users\\\\test\\\\gitignore_global\n        [credential]\n" +
		"            helper = manager\n        "
	if got, ok := parseConfig(t, contents); !ok || got != `C:\\Users\\test\\gitignore_global` {
		t.Fatalf("excludesfile = %q, %v", got, ok)
	}
}

func TestParsesMessyRealworldConfig(t *testing.T) {
	contents := "\n        # global config\n        [user]\n            email = foo@bar.com\n        [core]\n" +
		"            excludesfile = /first/path\n        # override later\n        [core]\n" +
		"            excludesfile = /final/path\n        [alias]\n            co = checkout\n        "
	if got, ok := parseConfig(t, contents); !ok || got != "/final/path" {
		t.Fatalf("excludesfile = %q, %v", got, ok)
	}
}

func TestHandlesLargeRealisticConfig(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "[section%d]\nkey%d = value%d\n", i, i, i)
	}
	b.WriteString("[core]\nexcludesfile = /large/test/path\n")
	if got, ok := parseConfig(t, b.String()); !ok || got != "/large/test/path" {
		t.Fatalf("excludesfile = %q, %v", got, ok)
	}
}
