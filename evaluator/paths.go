package evaluator

import "strings"

// The helpers below follow Rust's std::path on Unix, which the crate uses to split, join and
// compare paths.

// pathComponents splits p into components: a leading "/" for an absolute path, a leading "."
// for a path that starts with one, and every other non-empty, non-"." segment.
func pathComponents(p string) []string {
	var comps []string
	rest := p
	absolute := strings.HasPrefix(p, "/")
	if absolute {
		comps = append(comps, "/")
		rest = p[1:]
	}
	for i, seg := range strings.Split(rest, "/") {
		switch {
		case seg == "":
		case seg == ".":
			if i == 0 && !absolute {
				comps = append(comps, ".")
			}
		default:
			comps = append(comps, seg)
		}
	}
	return comps
}

// pathJoin appends c to p like PathBuf::push: an absolute c replaces p.
func pathJoin(p, c string) string {
	if strings.HasPrefix(c, "/") {
		return c
	}
	if p != "" && !strings.HasSuffix(p, "/") {
		return p + "/" + c
	}
	return p + c
}

func joinComponents(comps []string) string {
	out := ""
	for _, c := range comps {
		out = pathJoin(out, c)
	}
	return out
}

// pathStartsWith reports whether base's components are a prefix of p's.
func pathStartsWith(p, base string) bool {
	pc, bc := pathComponents(p), pathComponents(base)
	if len(bc) > len(pc) {
		return false
	}
	for i := range bc {
		if pc[i] != bc[i] {
			return false
		}
	}
	return true
}

func trimTrailing(s string) string {
	for {
		switch {
		case len(s) > 1 && strings.HasSuffix(s, "/"):
			s = s[:len(s)-1]
		case len(s) > 2 && strings.HasSuffix(s, "/."):
			s = s[:len(s)-2]
		default:
			return s
		}
	}
}

// pathParent is Path::parent, with "" where Rust has no parent.
func pathParent(p string) string {
	s := trimTrailing(p)
	if s == "" || s == "/" {
		return ""
	}
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return ""
	}
	if i == 0 {
		return "/"
	}
	return trimTrailing(s[:i])
}
