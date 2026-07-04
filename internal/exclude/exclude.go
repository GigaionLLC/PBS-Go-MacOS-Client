// Package exclude implements .pxarexclude-style pattern matching for choosing
// which paths to skip during backup. It follows the common gitignore subset PBS
// uses: basename globs match at any depth, patterns containing a slash are
// anchored to the backup root, a leading '!' re-includes, a trailing '/' means
// directory-only, and '**' matches any number of path segments.
package exclude

import (
	"path"
	"strings"
)

type pattern struct {
	negate   bool
	dirOnly  bool
	anchored bool
	segs     []string
}

// Matcher holds an ordered pattern list; the last matching pattern wins.
type Matcher struct {
	patterns []pattern
}

// New builds a Matcher from pattern lines (blank lines and '#' comments are
// ignored). It never errors; invalid globs simply fail to match.
func New(lines []string) *Matcher {
	m := &Matcher{}
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		p := pattern{}
		if strings.HasPrefix(trimmed, "!") {
			p.negate = true
			trimmed = trimmed[1:]
		}
		if strings.HasSuffix(trimmed, "/") {
			p.dirOnly = true
			trimmed = strings.TrimSuffix(trimmed, "/")
		}
		// A slash anywhere (leading or internal) anchors to the root; a bare
		// name floats and matches by basename at any depth.
		p.anchored = strings.Contains(trimmed, "/")
		trimmed = strings.TrimPrefix(trimmed, "/")
		if trimmed == "" {
			continue
		}
		p.segs = strings.Split(trimmed, "/")
		m.patterns = append(m.patterns, p)
	}
	return m
}

// Empty reports whether the matcher has no patterns.
func (m *Matcher) Empty() bool { return m == nil || len(m.patterns) == 0 }

// Excluded reports whether the '/'-rooted path (e.g. "/sub/file.log") should be
// skipped. isDir gates directory-only patterns.
func (m *Matcher) Excluded(p string, isDir bool) bool {
	if m.Empty() {
		return false
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	excluded := false
	for _, pat := range m.patterns {
		if pat.dirOnly && !isDir {
			continue
		}
		if pat.match(segs) {
			excluded = !pat.negate
		}
	}
	return excluded
}

func (p pattern) match(segs []string) bool {
	if !p.anchored {
		// Floating single-segment: match the basename at any depth.
		if len(segs) == 0 {
			return false
		}
		ok, _ := path.Match(p.segs[0], segs[len(segs)-1])
		return ok
	}
	return matchSegs(p.segs, segs)
}

// matchSegs matches pattern segments against path segments, with "**" matching
// zero or more segments and other segments matched as globs via path.Match.
func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], segs[0]); !ok {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}
