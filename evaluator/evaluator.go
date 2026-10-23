package evaluator

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"os/user"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// File is a parsed ignore file: the directory its patterns are relative to, its non-empty
// globs in file order, and the SHA-256 checksum of the content they were parsed from.
type File struct {
	BasePath string
	Content  []*Glob
	Checksum []byte
}

// NewFile returns a File with the given base path, globs and checksum.
func NewFile(basePath string, content []*Glob, checksum []byte) *File {
	return &File{BasePath: basePath, Content: content, Checksum: checksum}
}

// IsIgnored matches path against the file's globs; the last glob that matches decides. path is
// made relative by stripping every leading copy of BasePath and then every leading "/"; a path
// that is not valid UTF-8 is taken as "".
func (f *File) IsIgnored(path string) (ignored bool, matched bool) {
	if !utf8.ValidString(path) {
		path = ""
	}
	m := trimStartMatches(path, f.BasePath)
	m = trimStartMatches(m, "/")
	for i := len(f.Content) - 1; i >= 0; i-- {
		if ignored, matched := f.Content[i].IsIgnored(m); matched {
			return ignored, true
		}
	}
	return false, false
}

func trimStartMatches(s, prefix string) string {
	if prefix == "" {
		return s
	}
	for strings.HasPrefix(s, prefix) {
		s = s[len(prefix):]
	}
	return s
}

// Evaluator decides whether paths are ignored by the .gitignore files on their way from the
// filesystem root, the repository's .git/info/exclude and the user's global excludes file. It
// caches what it reads and is safe for concurrent use.
type Evaluator struct {
	filesMu sync.Mutex
	files   map[string]*File

	configMu sync.Mutex
	configs  map[string]*configFile

	rootsMu sync.RWMutex
	roots   []string
}

// NewEvaluator returns an Evaluator with empty caches.
func NewEvaluator() *Evaluator {
	return &Evaluator{files: map[string]*File{}, configs: map[string]*configFile{}}
}

// IsIgnored reports whether path is ignored.
func (e *Evaluator) IsIgnored(path string) bool {
	gitRoot, hasRoot, ignored, decided := e.evaluateGitignoreFiles(path)
	if decided {
		emit("DEBUG", path+" is ignored by .gitignore: "+rustBool(ignored))
		return ignored
	}
	if hasRoot {
		if ignored, ok := e.evaluateLocalExclude(gitRoot, path); ok {
			return ignored
		}
	}
	if ignored, ok := e.evaluateGlobalExclude(gitRoot, hasRoot, path); ok {
		return ignored
	}
	return false
}

func (e *Evaluator) evaluateGitignoreFiles(path string) (root string, hasRoot bool, ignored bool, decided bool) {
	root, hasRoot = e.closestRoot(path)
	parts := pathComponents(path)
	offset := 1
	if hasRoot {
		offset = len(pathComponents(root))
	}
	inRoot := hasRoot
	for i := offset; i < len(parts); i++ {
		base := joinComponents(parts[:i])
		if e.recordRoot(base) && (!hasRoot || root != base) {
			if inRoot {
				decided = false
				emit("DEBUG", "Encountered recursive git root at: "+base)
			} else {
				inRoot = true
				emit("DEBUG", "Encountered git root at: "+base)
			}
			root, hasRoot = base, true
		} else if !inRoot {
			continue
		}
		candidate := pathJoin(base, ".gitignore")
		file, err := e.getOrParse(candidate, "", false)
		if err != nil {
			emit("ERROR", "Failed to read .gitignore file at "+candidate+": "+rustErrorDebug(err))
			continue
		}
		if file == nil {
			continue
		}
		parent := pathJoin(joinComponents(parts[:i+1]), "")
		if ign, ok := file.IsIgnored(parent); ok && ign {
			emit("DEBUG", parent+" is ignored so "+path+" is ignored by association.")
			return root, hasRoot, true, true
		}
		if ign, ok := file.IsIgnored(path); ok {
			ignored, decided = ign, true
		}
	}
	return root, hasRoot, ignored, decided
}

func (e *Evaluator) evaluateLocalExclude(root, path string) (bool, bool) {
	exclude, ok := e.excludePath(root)
	if !ok {
		return false, false
	}
	file, err := e.getOrParse(exclude, root, true)
	if err != nil {
		emit("ERROR", "Failed to read .gitignore file at "+exclude+": "+rustErrorDebug(err))
		return false, false
	}
	if file == nil {
		return false, false
	}
	if ignored, ok := file.IsIgnored(path); ok {
		emit("DEBUG", exclude+" is ignored by "+path+": "+rustBool(ignored))
		return ignored, true
	}
	return false, false
}

func (e *Evaluator) evaluateGlobalExclude(root string, hasRoot bool, path string) (bool, bool) {
	exclude, ok, err := e.globalExcludePath()
	if err != nil || !ok {
		// A git config that cannot be read ends the global lookup with no excludes file and, as in the crate,
		// with no record: the log contract has none for this point.
		return false, false
	}
	file, err := e.getOrParse(exclude, root, hasRoot)
	if err != nil {
		emit("ERROR", "Failed to read global .gitignore file at "+rustDebugOptPath(exclude, true)+": "+
			rustErrorDebug(err))
		return false, false
	}
	if file == nil {
		return false, false
	}
	if ignored, ok := file.IsIgnored(path); ok {
		emit("DEBUG", path+" is ignored by "+rustDebugOptPath(exclude, true)+": "+rustBool(ignored))
		return ignored, true
	}
	return false, false
}

// getOrParse returns the parsed ignore file at path, nil when it does not exist. A cached file
// is reused while its checksum is unchanged, keeping the base it was parsed with; a changed file
// is parsed again against the base of this call.
func (e *Evaluator) getOrParse(path, base string, hasBase bool) (*File, error) {
	if !pathExists(path) {
		return nil, nil
	}
	e.filesMu.Lock()
	defer e.filesMu.Unlock()
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, &FileError{File: path, Err: err}
	}
	sum := sha256.Sum256(content)
	cached, seen := e.files[path]
	if seen && bytes.Equal(cached.Checksum, sum[:]) {
		return cached, nil
	}
	if !hasBase {
		base = pathParent(path)
	}
	file, err := parseIgnoreFile(base, content, sum[:])
	if err != nil {
		return nil, err
	}
	e.files[path] = file
	if seen {
		// The call that notices a change stores the new parse but still answers with the one it
		// replaced; later calls use the new one.
		return cached, nil
	}
	return file, nil
}

// parseIgnoreFile compiles every line of an ignore file; one invalid line makes the whole file
// invalid.
func parseIgnoreFile(base string, content []byte, checksum []byte) (*File, error) {
	globs := []*Glob{}
	for _, line := range splitLines(content) {
		glob, err := NewGlob(line)
		if err != nil {
			return nil, err
		}
		if !glob.IsEmpty() {
			globs = append(globs, glob)
		}
	}
	return NewFile(base, globs, checksum), nil
}

// splitLines splits like Rust's BufRead::lines: on "\n", dropping one "\r" before it, with no
// final empty line; a line that is not valid UTF-8 reads as "".
func splitLines(data []byte) []string {
	var lines []string
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
			line = bytes.TrimSuffix(line, []byte{'\r'})
		} else {
			line, data = data, nil
		}
		if !utf8.Valid(line) {
			line = nil
		}
		lines = append(lines, string(line))
	}
	return lines
}

func (e *Evaluator) recordRoot(dir string) bool {
	if !pathExists(pathJoin(dir, ".git")) {
		return false
	}
	e.rootsMu.Lock()
	defer e.rootsMu.Unlock()
	for _, r := range e.roots {
		if r == dir {
			return true
		}
	}
	e.roots = append(e.roots, dir)
	return true
}

// closestRoot returns the deepest already recorded git root that contains path.
func (e *Evaluator) closestRoot(path string) (string, bool) {
	e.rootsMu.RLock()
	defer e.rootsMu.RUnlock()
	best, found := "", false
	for _, r := range e.roots {
		if !pathStartsWith(path, r) {
			continue
		}
		if !found || pathStartsWith(r, best) {
			best, found = r, true
		}
	}
	return best, found
}

func (e *Evaluator) excludePath(root string) (string, bool) {
	e.rootsMu.RLock()
	known := false
	for _, r := range e.roots {
		if r == root {
			known = true
			break
		}
	}
	e.rootsMu.RUnlock()
	if !known {
		emit("WARN", "Local exclude path was requested for git root which has not been recorded: "+root)
		return "", false
	}
	exclude := pathJoin(root, ".git/info/exclude")
	if !pathExists(exclude) {
		return "", false
	}
	return exclude, true
}

type configFile struct {
	exclude    string
	hasExclude bool
	checksum   []byte
}

// whitespace is Unicode White_Space, which the crate's config regex means by \s.
const whitespace = `\t\n\x{0B}\x{0C}\r \x{85}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}`

var excludesFileRe = regexp.MustCompile(`(?i)excludesfile[` + whitespace + `]*=[` + whitespace + `"]*(?P<path>[^` +
	whitespace + `"]+)`)

func xdgConfigHome() (string, bool) {
	if v, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && v != "" && utf8.ValidString(v) {
		return v, true
	}
	if home, ok := homeDir(); ok {
		return pathJoin(home, ".config"), true
	}
	return "", false
}

// homeDir is HOME when it is set and not empty, otherwise the home directory of the account
// running the process.
func homeDir() (string, bool) {
	if v, ok := os.LookupEnv("HOME"); ok && v != "" {
		return v, true
	}
	u, err := user.Current()
	if err != nil {
		return "", false
	}
	return u.HomeDir, true
}

// globalExcludePath finds the global excludes file: core.excludesfile from the first git config
// file that sets it, else the default under the XDG config directory.
func (e *Evaluator) globalExcludePath() (string, bool, error) {
	var candidates []struct {
		path string
		ok   bool
	}
	xdg, xdgOK := xdgConfigHome()
	candidates = append(candidates, struct {
		path string
		ok   bool
	}{pathJoin(xdg, "git/config"), xdgOK})
	home, homeOK := homeDir()
	candidates = append(candidates, struct {
		path string
		ok   bool
	}{pathJoin(home, ".gitconfig"), homeOK})
	for _, c := range candidates {
		emit("DEBUG", "Attempting read of git config file potentially in: "+rustDebugOptPath(c.path, c.ok))
		if !c.ok {
			continue
		}
		exclude, hasExclude, done, err := e.readConfig(c.path)
		if err != nil {
			return "", false, err
		}
		if done {
			return exclude, hasExclude, nil
		}
	}
	xdg, xdgOK = xdgConfigHome()
	def := pathJoin(xdg, "git/ignore")
	emit("DEBUG", "No valid core.excludesfile config found in any git config file. Using default path: "+
		rustDebugOptPath(def, xdgOK))
	return def, xdgOK, nil
}

// readConfig consults one git config file. done is true when the search ends here: the file is
// cached unchanged (whatever it set) or it sets core.excludesfile.
func (e *Evaluator) readConfig(path string) (exclude string, hasExclude bool, done bool, err error) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	content, rerr := os.ReadFile(path)
	if rerr != nil {
		if errors.Is(rerr, fs.ErrNotExist) {
			return "", false, false, nil
		}
		return "", false, false, &FileError{File: path, Err: rerr}
	}
	sum := sha256.Sum256(content)
	if cached, ok := e.configs[path]; ok && bytes.Equal(cached.checksum, sum[:]) {
		emit("DEBUG", "Using existing cached core.excludesfile set as: "+
			rustDebugOptPath(cached.exclude, cached.hasExclude))
		return cached.exclude, cached.hasExclude, true, nil
	}
	cf, perr := readGitConfig(path, content, sum[:])
	if perr != nil {
		// A git config that is not UTF-8 sets no core.excludesfile: the crate moves on to the next config file
		// without a record or an error, and so does this search.
		return "", false, false, nil
	}
	e.configs[path] = cf
	if cf.hasExclude {
		emit("DEBUG", "Git config file set core.excludesfile as: "+rustDebugOptPath(cf.exclude, true))
		return cf.exclude, true, true, nil
	}
	return "", false, false, nil
}

// readGitConfig extracts core.excludesfile from a git config file: the path of the last
// assignment the crate's pattern finds, if any.
func readGitConfig(path string, content []byte, checksum []byte) (*configFile, error) {
	if !utf8.Valid(content) {
		return nil, &FileError{File: path, Err: errInvalidUTF8}
	}
	cf := &configFile{checksum: checksum}
	matches := excludesFileRe.FindAllStringSubmatch(string(content), -1)
	if n := len(matches); n > 0 {
		cf.exclude, cf.hasExclude = matches[n-1][1], true
	}
	emit("TRACE", "Config file at "+path+" defines core.excludesfile as: "+rustDebugOptPath(cf.exclude, cf.hasExclude))
	return cf, nil
}

// pathExists reports whether path can be stat'ed. Any stat error, a permission error included, counts as a
// missing file, as it does in the crate's existence check.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
