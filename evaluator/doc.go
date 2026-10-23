// Package evaluator evaluates paths against .gitignore files in a directory and its parent
// directories, the repository's .git/info/exclude and the user's global excludes file.
//
// The format and its precedence rules are described in the git documentation
// (https://git-scm.com/docs/gitignore). Glob compiles one pattern, File holds one parsed ignore
// file and Evaluator walks a path and caches the files it reads.
package evaluator
