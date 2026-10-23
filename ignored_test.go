package ignored_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ryanmab/ignored"
)

// The mock project only counts as a repository once it has a .git entry.
func Example() {
	_ = os.Mkdir("tests/fixtures/mock-project/.git", 0o755)
	fmt.Println(ignored.IsIgnored("tests/fixtures/mock-project/file.tmp"))
	// Output: true
}

func ExampleIsIgnored() {
	_ = os.Mkdir("tests/fixtures/mock-project/.git", 0o755)
	isIgnored := ignored.IsIgnored("tests/fixtures/mock-project/file.tmp")
	fmt.Println(isIgnored)
	// Output: true
}

func caseName(i int) string { return "case_" + strconv.Itoa(i+1) }

// copyFixtures copies tests/fixtures into a fresh temporary directory and returns it.
func copyFixtures(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("tests/fixtures", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("tests/fixtures", path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying fixtures: %v", err)
	}
	return dst
}

// gitInit makes dir a repository root the way git init does for the evaluator: it gains a .git
// directory.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMatchesGitCheckIgnore holds, per path, the answer git check-ignore gives on the mock project,
// which the crate reproduces.
func TestMatchesGitCheckIgnore(t *testing.T) {
	cases := []struct {
		path    []string
		ignored bool
	}{
		{[]string{`root.txt`}, false},
		{[]string{`src`, `root.txt`}, false},
		{[]string{`build`, `artifact.bin`}, true},
		{[]string{`src`, `build`, `nested.bin`}, true},
		{[]string{`tmp`, `keep.txt`}, true},
		{[]string{`src`, `lib.rs`}, false},
		{[]string{`src`, `main.rs`}, false},
		{[]string{`src`, `module`, `deep.log`}, true},
		{[]string{`src`, `module`, `deep_keep.log`}, true},
		{[]string{`src`, `module`, `a.tmp`}, true},
		{[]string{`src`, `module`, `b.tmp`}, true},
		{[]string{`src`, `module`, `file1.txt`}, true},
		{[]string{`src`, `module`, `fileA.txt`}, true},
		{[]string{`fileA.txt`}, true},
		{[]string{`file1.txt`}, true},
		{[]string{`file9.txt`}, true},
		{[]string{`fileZ.txt`}, true},
		{[]string{`filex.txt`}, true},
		{[]string{`literal`, `#hash.txt`}, false},
		{[]string{`literal`, `!bang.txt`}, false},
		{[]string{`literal`, `space file.txt`}, false},
		{[]string{`vendor`, `ignored.txt`}, true},
		{[]string{`vendor`, `keep.me`}, true},
		{[]string{`foo`}, true},
		{[]string{`foo `}, false},
		{[]string{`bar`}, false},
		{[]string{`bar `}, false},
		{[]string{`baz`, `foo`, `bar`}, true},
		{[]string{`build_dir`}, false},
		{[]string{`build_dir`, `file.txt`}, false},
		{[]string{`src`, `build`, `file.txt`}, true},
		{[]string{`other`, `src`, `build`, `file.txt`}, true},
		{[]string{`a`, `b`}, false},
		{[]string{`a`, `x`, `b`}, false},
		{[]string{`a`, `x`, `y`, `b`}, false},
		{[]string{`nested`, `build`}, false},
		{[]string{`nested`, `sub`, `build`}, false},
		{[]string{`file_vs_dir`, `same`}, false},
		{[]string{`file_vs_dir`, `same_dir`, `file.txt`}, false},
		{[]string{`double_negation`, `important.tmp`}, true},
		{[]string{`.gitignore_should_be_ignored`}, false},
		{[]string{`emptydir`}, false},
		{[]string{`file1.dat`}, false},
		{[]string{`file12.dat`}, false},
		{[]string{`x`, `y`, `z`, `globfoo.txt`}, true},
		{[]string{`globdir`, `subdir`, `file.txt`}, true},
		{[]string{`a`, `b`, `c`, `globbar.txt`}, true},
		{[]string{`anchored.txt`}, true},
		{[]string{`src`, `anchored.txt`}, false},
		{[]string{`dironly`, `sub`, `file.txt`}, true},
		{[]string{`dironly`}, true},
		{[]string{`literal`, `file*.txt`}, true},
		{[]string{`literal`, `file?.txt`}, true},
		{[]string{`literal`, `file[abc].txt`}, true},
		{[]string{`literal`, `filea.txt`}, true},
		{[]string{`precedence.log`}, true},
		{[]string{`important.log`}, false},
		{[]string{`pruned`, `deep`, `keep.txt`}, true},
		{[]string{`deep`, `a`, `b`, `c`, `match.txt`}, false},
		{[]string{`deep`, `match.txt`}, false},
		{[]string{`prefix`, `a`, `b`, `suffix.log`}, true},
		{[]string{`prefix`, `suffix.log`}, true},
		{[]string{`anydepth`, `x`, `y`, `z`, `file.tmp`}, true},
		{[]string{`file.tmp`}, true},
		{[]string{`dirslash`}, false},
		{[]string{`dirslash`, `file.txt`}, false},
		{[]string{`dirslash_file`}, false},
		{[]string{`escaped`, `space\ `}, false},
		{[]string{`escaped`, `space `}, false},
		{[]string{`escaped`, `space`}, false},
		{[]string{`escaped`, `!literal.txt`}, false},
		{[]string{`escaped`, `#literal.txt`}, false},
		{[]string{`multi`, `slash`, `file.txt`}, false},
		{[]string{`reinclude`, `keep.txt`}, false},
		{[]string{`reinclude`, `other.txt`}, false},
		{[]string{`prune_dir`, `deep`, `file.txt`}, false},
		{[]string{`prune_dir`, `deep`, `important.txt`}, false},
		{[]string{`escaped`, `back\slash.txt`}, false},
		{[]string{`escaped`, `back-slash.txt`}, false},
		{[]string{`escaped`, `back\-slash.txt`}, false},
		{[]string{`charclass`, `filea.log`}, true},
		{[]string{`charclass`, `filed.log`}, true},
		{[]string{`qmark`, `file1.txt`}, true},
		{[]string{`qmark`, `file12.txt`}, false},
		{[]string{`range`, `file5.txt`}, true},
		{[]string{`range`, `filex.txt`}, true},
		{[]string{`anchored_dir`, `file.txt`}, false},
		{[]string{`sub`, `anchored_dir`, `file.txt`}, false},
		{[]string{`trailing_space`, `foo`}, true},
		{[]string{`trailing_space`, `foo `}, false},
		{[]string{`double_star_root`, `file.txt`}, false},
		{[]string{`dotfile_root`, `.hidden.txt`}, false},
		{[]string{`dotfile_dir`, `.hidden.txt`}, false},
		{[]string{`builder`}, false},
		{[]string{`build`}, true},
		{[]string{`unicode`, `fileé.txt`}, false},
		{[]string{`ignored_outside_git_root.txt`}, false},
		{[]string{`ignored_by_repo_level_exclude.txt`}, false},
	}
	for i, c := range cases {
		t.Run(caseName(i), func(t *testing.T) {
			repo := filepath.Join(copyFixtures(t), "mock-project")
			gitInit(t, repo)
			path := filepath.Join(append([]string{repo}, c.path...)...)
			if got := ignored.IsIgnored(path); got != c.ignored {
				t.Fatalf("IsIgnored(%s) = %v; git check-ignore says %v", filepath.Join(c.path...), got, c.ignored)
			}
		})
	}
}

func TestHandlesNegation(t *testing.T) {
	repo := filepath.Join(copyFixtures(t), "mock-project")
	gitInit(t, repo)
	if !ignored.IsIgnored(filepath.Join(repo, "double_negation/important.tmp")) {
		t.Fatal("double_negation/important.tmp should be ignored")
	}
	if ignored.IsIgnored(filepath.Join(repo, "important.log")) {
		t.Fatal("important.log should not be ignored")
	}
}

func TestObservesRecursiveGitRootsWhenIgnoring(t *testing.T) {
	temp := copyFixtures(t)
	repo := filepath.Join(temp, "mock-project")
	gitInit(t, temp)
	gitInit(t, repo)
	if ignored.IsIgnored(filepath.Join(repo, "ignored_outside_git_root.txt")) {
		t.Fatal("a nested repository resets the decision of the outer .gitignore")
	}
}

func TestObservesIgnoresInGitRoot(t *testing.T) {
	repo := filepath.Join(copyFixtures(t), "mock-project")
	file := filepath.Join(repo, "ignored_by_repo_level_exclude.txt")
	gitInit(t, repo)
	writeFile(t, filepath.Join(repo, ".git", "info", "exclude"), "ignored_by_repo_level_exclude.txt")
	if !ignored.IsIgnored(file) {
		t.Fatal("a path listed in .git/info/exclude should be ignored")
	}
}

func TestObservesRecursiveGitRootsWhenEvaluatingExcludesListedInRoot(t *testing.T) {
	temp := copyFixtures(t)
	repo := filepath.Join(temp, "mock-project")
	file := filepath.Join(repo, "ignored_by_repo_level_exclude.txt")
	gitInit(t, temp)
	writeFile(t, filepath.Join(temp, ".git", "info", "exclude"), "ignored_by_repo_level_exclude.txt")
	gitInit(t, repo)
	if ignored.IsIgnored(file) {
		t.Fatal("the outer repository's info/exclude must not apply inside a nested repository")
	}
}

func TestGlobalGitExcludesFromDefaultPaths(t *testing.T) {
	home, xdg := t.TempDir(), t.TempDir()
	repo := filepath.Join(t.TempDir(), "mock-project")
	writeFile(t, filepath.Join(home, ".config", "git", "ignore"), "home_ignored.txt\n")
	writeFile(t, filepath.Join(xdg, "git", "ignore"), "xdg_ignored.txt\n")
	gitInit(t, repo)

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("XDG_CONFIG_HOME")
	if !ignored.IsIgnored(filepath.Join(repo, "home_ignored.txt")) {
		t.Fatal("home_ignored.txt should be ignored by $HOME/.config/git/ignore")
	}
	if ignored.IsIgnored(filepath.Join(repo, "some_other_file.txt")) {
		t.Fatal("some_other_file.txt should not be ignored")
	}

	t.Setenv("XDG_CONFIG_HOME", xdg)
	if !ignored.IsIgnored(filepath.Join(repo, "xdg_ignored.txt")) {
		t.Fatal("xdg_ignored.txt should be ignored by $XDG_CONFIG_HOME/git/ignore")
	}
	if ignored.IsIgnored(filepath.Join(repo, "some_other_file.txt")) {
		t.Fatal("some_other_file.txt should not be ignored")
	}
}

func globalExcludesFromConfig(t *testing.T, configRel string) {
	home := t.TempDir()
	repo := filepath.Join(t.TempDir(), "mock-project")
	excludes := filepath.Join(home, "custom_global_ignore")
	writeFile(t, excludes, "ignored.txt\n")
	writeFile(t, filepath.Join(home, configRel),
		"[core]\n\texcludesfile=/some-invalid-path\n[core]\n\texcludesfile="+filepath.ToSlash(excludes))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("XDG_CONFIG_HOME")
	gitInit(t, repo)
	if !ignored.IsIgnored(filepath.Join(repo, "ignored.txt")) {
		t.Fatal("ignored.txt should be ignored by core.excludesfile")
	}
	if ignored.IsIgnored(filepath.Join(repo, "some_other_file.txt")) {
		t.Fatal("some_other_file.txt should not be ignored")
	}
}

func TestGlobalGitExcludesFromGitConfig(t *testing.T) {
	globalExcludesFromConfig(t, filepath.Join(".config", "git", "config"))
}

func TestLegacyGlobalGitExcludesFromGitConfig(t *testing.T) {
	globalExcludesFromConfig(t, ".gitconfig")
}
