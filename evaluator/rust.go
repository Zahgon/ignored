package evaluator

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

var (
	loggerMu sync.RWMutex
	logger   func(level, message string)
)

// SetLogger installs fn to receive the evaluator's log records, one call per record, in the order
// they are emitted. level is one of ERROR, WARN, INFO, DEBUG or TRACE. A nil fn stops logging.
func SetLogger(fn func(level, message string)) {
	loggerMu.Lock()
	logger = fn
	loggerMu.Unlock()
}

func emit(level, message string) {
	loggerMu.RLock()
	fn := logger
	loggerMu.RUnlock()
	if fn != nil {
		fn(level, message)
	}
}

func rustBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// rustDebugStr quotes s the way Rust's Debug implementation for str does.
func rustDebugStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch {
		case c == 0:
			b.WriteString(`\0`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '"':
			b.WriteString(`\"`)
		case rustIsGraphemeExtended(c) || !rustIsPrintable(c):
			fmt.Fprintf(&b, `\u{%x}`, c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func rustDebugOptPath(p string, ok bool) string {
	if !ok {
		return "None"
	}
	return "Some(" + rustDebugStr(p) + ")"
}

// osError describes an operating-system error the way Rust's std::io::Error does.
type osError struct {
	code    int
	kind    string
	message string
}

var osErrors = map[string]osError{
	"no such file or directory":         {2, "NotFound", "No such file or directory"},
	"permission denied":                 {13, "PermissionDenied", "Permission denied"},
	"is a directory":                    {21, "IsADirectory", "Is a directory"},
	"not a directory":                   {20, "NotADirectory", "Not a directory"},
	"too many levels of symbolic links": {40, "FilesystemLoop", "Too many levels of symbolic links"},
	"file name too long":                {36, "InvalidFilename", "File name too long"},
	"invalid argument":                  {22, "InvalidInput", "Invalid argument"},
}

func lookupOSError(err error) (osError, bool) {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	e, ok := osErrors[err.Error()]
	return e, ok
}

// rustIODisplay renders err like Rust's Display for std::io::Error.
func rustIODisplay(err error) string {
	if e, ok := lookupOSError(err); ok {
		return fmt.Sprintf("%s (os error %d)", e.message, e.code)
	}
	if errors.Is(err, errInvalidUTF8) {
		return "stream did not contain valid UTF-8"
	}
	return err.Error()
}

func rustIODebug(err error) string {
	if e, ok := lookupOSError(err); ok {
		return fmt.Sprintf("Os { code: %d, kind: %s, message: %s }", e.code, e.kind, rustDebugStr(e.message))
	}
	if errors.Is(err, errInvalidUTF8) {
		return `Error { kind: InvalidData, message: "stream did not contain valid UTF-8" }`
	}
	return rustDebugStr(err.Error())
}

var errInvalidUTF8 = errors.New("stream did not contain valid UTF-8")

// FileError reports an ignore file that exists but could not be read.
type FileError struct {
	File string
	Err  error
}

// Error returns the crate's Display text for a file that could not be read.
func (e *FileError) Error() string { return "Unable to read " + e.File + ": " + rustIODisplay(e.Err) }

// Unwrap returns the underlying read error.
func (e *FileError) Unwrap() error { return e.Err }

// rustErrorDebug renders an evaluator error like the crate's derived Debug output.
func rustErrorDebug(err error) string {
	switch e := err.(type) {
	case *InvalidPatternError:
		source := "None"
		if e.Source != nil {
			source = "Some(InvalidPattern(" + rustDebugStr(e.Source.Pattern) + "))"
		}
		return "InvalidPattern { pattern: " + rustDebugStr(e.Pattern) + ", source: " + source + " }"
	case *InvalidRegexError:
		return "InvalidRegex { pattern: " + rustDebugStr(e.Pattern) + ", regex: " + rustDebugStr(e.Regex) +
			", source: " + e.compile.debug() + " }"
	case *FileError:
		return "FileError { file: " + rustDebugStr(e.File) + ", source: " + rustIODebug(e.Err) + " }"
	}
	return rustDebugStr(err.Error())
}
