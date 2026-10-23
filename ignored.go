// Package ignored checks whether a path is ignored by git's ignore rules without running git.
//
// It reads the .gitignore files on the way to the path, the repository's .git/info/exclude and the
// user's global excludes file (core.excludesfile, or git/ignore under the XDG config directory),
// with negation, directory-only patterns, nested .gitignore hierarchies and nested repositories.
//
// IsIgnored uses one evaluator shared by the whole process, which caches the ignore files it
// reads and re-reads a file only when its content changes. Use evaluator.NewEvaluator for an
// evaluator with its own cache.
package ignored

import (
	"sync"

	"github.com/ryanmab/ignored/evaluator"
)

var (
	sharedOnce sync.Once
	shared     *evaluator.Evaluator
)

// IsIgnored reports whether path is ignored, using the process-wide shared evaluator.
func IsIgnored(path string) bool {
	sharedOnce.Do(func() { shared = evaluator.NewEvaluator() })
	return shared.IsIgnored(path)
}
