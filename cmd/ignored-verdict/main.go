// Command ignored-verdict answers ignore questions with the evaluator package.
//
//	ignored-verdict ROOT < PATHS           one verdict line per path
//	ignored-verdict --trace ROOT < PATHS   the same, each verdict preceded by its log records
//	ignored-verdict --check FILE...        one diagnostic line per pattern file
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ryanmab/ignored/evaluator"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func usage(stderr io.Writer) int {
	fmt.Fprintln(stderr, "usage: ignored-verdict [--trace] ROOT < PATHS | ignored-verdict --check FILE...")
	return 2
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) > 0 && args[0] == "--check":
		if len(args) == 1 {
			return usage(stderr)
		}
		return check(args[1:], stdout)
	case len(args) > 0 && args[0] == "--trace":
		if len(args) != 2 {
			return usage(stderr)
		}
		return verdicts(args[1], true, stdin, stdout, stderr)
	case len(args) == 1:
		return verdicts(args[0], false, stdin, stdout, stderr)
	}
	return usage(stderr)
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`).Replace(s)
}

func verdicts(root string, trace bool, stdin io.Reader, stdout, stderr io.Writer) int {
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return usage(stderr)
	}
	var records []string
	if trace {
		evaluator.SetLogger(func(level, message string) {
			records = append(records, level+" "+escape(message))
		})
		defer evaluator.SetLogger(nil)
	}
	root = strings.TrimRight(root, "/")
	ev := evaluator.NewEvaluator()
	out := bufio.NewWriter(stdout)
	in := bufio.NewReader(stdin)
	anyIgnored := false
	for {
		line, err := in.ReadString('\n')
		if line == "" && err != nil {
			break
		}
		if strings.HasSuffix(line, "\n") {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		}
		if !utf8.ValidString(line) {
			out.Flush()
			fmt.Fprintln(stderr, "ignored-verdict: standard input is not valid UTF-8")
			return 2
		}
		if line == "" {
			continue
		}
		ignored := ev.IsIgnored(root + "/" + line)
		anyIgnored = anyIgnored || ignored
		for _, r := range records {
			fmt.Fprintln(out, r)
		}
		records = records[:0]
		word := "kept"
		if ignored {
			word = "ignored"
		}
		fmt.Fprintln(out, word+" "+line)
	}
	if err := out.Flush(); err != nil {
		return 2
	}
	if anyIgnored {
		return 0
	}
	return 1
}

func check(files []string, stdout io.Writer) int {
	out := bufio.NewWriter(stdout)
	anyInvalid := false
	for _, file := range files {
		number, globs, err := checkFile(file)
		if err != nil {
			anyInvalid = true
			fmt.Fprintf(out, "invalid %s:%d: %s\n", file, number, escape(err.Error()))
			continue
		}
		fmt.Fprintf(out, "ok %s %d\n", file, globs)
	}
	if err := out.Flush(); err != nil {
		return 2
	}
	if anyInvalid {
		return 1
	}
	return 0
}

// checkFile compiles every line of one pattern file, as the evaluator reads an ignore file, and
// returns the number of non-empty globs, or the first failing line (0 when the file cannot be
// read) and its error.
func checkFile(file string) (int, int, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return 0, 0, &evaluator.FileError{File: file, Err: err}
	}
	globs := 0
	for i, line := range patternLines(content) {
		glob, err := evaluator.NewGlob(line)
		if err != nil {
			return i + 1, 0, err
		}
		if !glob.IsEmpty() {
			globs++
		}
	}
	return 0, globs, nil
}

// patternLines splits on "\n", dropping one "\r" before it, with no final empty line; a line that
// is not valid UTF-8 reads as "".
func patternLines(data []byte) []string {
	var lines []string
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = bytes.TrimSuffix(data[:i], []byte{'\r'}), data[i+1:]
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
