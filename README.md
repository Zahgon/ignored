[![Coverage](https://api.coveragerobot.com/v1/graph/github/ryanmab/ignored/badge.svg?token=5a7fdba2f656773ef1754d0082b3c474b7bd02150827d41f6e)](https://coveragerobot.com)
[![Go Reference](https://pkg.go.dev/badge/github.com/ryanmab/ignored.svg)](https://pkg.go.dev/github.com/ryanmab/ignored)
[![Build](https://github.com/ryanmab/ignored/actions/workflows/build.yml/badge.svg)](https://github.com/ryanmab/ignored/actions/workflows/build.yml)
![GitHub License](https://img.shields.io/github/license/ryanmab/ignored)

# Ignored

A Go implementation of the `.gitignore` file format for quickly checking whether a path is ignored by
git - without invoking the git cli.

This module aims for _full behavioural parity_ with git's handling of `.gitignore` files, including support for
all core features of the format such as negation patterns, directory-only patterns, nested `.gitignore`
hierarchies, and local and global exclusion files.

The full specification of the `.gitignore` format, along with details on file precedence and hierarchy, can be
found in the [Git documentation](https://git-scm.com/docs/gitignore#_description).

## Usage

```bash
go get github.com/ryanmab/ignored
```

### IsIgnored

The primary entry point is `ignored.IsIgnored`, which provides a convenient way to check whether a path is
ignored according to the `.gitignore` rules in the current repository.

It uses a process-wide evaluator that caches discovered `.gitignore` files and their parsed glob patterns
for improved performance across repeated calls.

```go
package main

import (
	"fmt"

	"github.com/ryanmab/ignored"
)

func main() {
	fmt.Println(ignored.IsIgnored("tests/fixtures/mock-project/file.tmp"))
}
```

### Evaluator

An `evaluator.Evaluator` provides a more flexible way to evaluate paths against `.gitignore` rules. It gives
you explicit control over caching - for example, allowing multiple evaluators with independent caches - and is
useful when the process-wide cache used by `ignored.IsIgnored` is not desirable.

```go
package main

import (
	"fmt"

	"github.com/ryanmab/ignored/evaluator"
)

func main() {
	ev := evaluator.NewEvaluator()
	fmt.Println(ev.IsIgnored("tests/fixtures/mock-project/file.tmp"))
}
```

### Command line

`cmd/ignored-verdict` answers the same questions from the command line:

```bash
go run ./cmd/ignored-verdict ROOT < PATHS          # ignored <path> or kept <path> per line
go run ./cmd/ignored-verdict --trace ROOT < PATHS  # each verdict preceded by the evaluator's log records
go run ./cmd/ignored-verdict --check FILE...       # ok FILE N, or invalid FILE:LINE: <error>
```

## Comparison

### `ignore` crate

The Rust [`ignore`](https://github.com/BurntSushi/ripgrep/tree/master/crates/ignore) library is a fantastic
directory iterator that supports filtering paths based on `.gitignore` rules.

Its primary focus is providing an efficient way to traverse directories while respecting ignore patterns,
rather than evaluating arbitrary paths in isolation. While many of its primitives can be used to evaluate
paths directly, it shines most in the context of directory traversal and filtering.

Like `ignored`, it also supports the full `.gitignore` specification, without the need to invoke the git cli.

However, `ignored` is specifically designed for evaluating arbitrary paths against `.gitignore` rules, with
behavioural parity to git, without focusing on directory traversal primitives or other features.

If you need to check whether a specific path is ignored by git, `ignored` provides an ergonomic and performant
solution without invoking the git cli.

## Contributing

Contributions are very welcome!

If you have suggestions, bug reports, or would like to contribute code, please open an issue or submit a pull request.

### Testing

This module includes a comprehensive test suite to ensure behavioural parity with the git cli and adherence
to the `.gitignore` specification.

Run the tests with:

```bash
go test ./...
```
