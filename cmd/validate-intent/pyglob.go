package main

// A Python-compatible glob.
//
// Go's filepath.Glob is *not* a drop-in for Python's glob.glob, and the
// differences are silent rather than loud:
//
//   - Python's `*` never matches a dotfile; Go's does. `examples/*.json` would
//     therefore pick up a stray `.draft.json` in Go and not in Python.
//   - Go treats `\` as an escape character inside a pattern; Python's fnmatch
//     treats it as a literal.
//   - Go rejects an unterminated `[` with ErrBadPattern; Python treats it as a
//     literal `[`.
//   - Go spells character-class negation `[^...]`; Python spells it `[!...]`.
//   - Python's `**` (recursive=True) has no Go equivalent at all.
//
// The first four are reproduced here. The fifth is refused loudly in main.go
// rather than approximated — see hasRecursiveComponent.
//
// Ordering is not this file's problem: the reference sorts the whole result
// (`sorted(glob.glob(...))` at bin/validate-intent:464), so only the *set* of
// matches has to agree. ExpandFiles does the sort.

import (
	"os"
	"sort"
	"strings"
)

// ExpandFiles is the port of `_expand_files` (bin/validate-intent:457-464):
// expand the pattern, sort, and drop anything that is not a regular file.
//
// Every glob-expanding mode goes through here so a pattern like `intents/*`
// never hands a *directory* to a reader, which would report it as an unreadable
// file rather than skipping it.
func ExpandFiles(pattern string) []string {
	matches := PyGlob(pattern)
	sort.Strings(matches) // byte order == code point order for UTF-8, as in Python
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		if isFile(match) {
			files = append(files, match)
		}
	}
	return files
}

// PyGlob mirrors glob.glob(pattern) for the non-recursive pattern syntax.
func PyGlob(pattern string) []string {
	return globInternal(pattern, false)
}

// hasRecursiveComponent reports whether any path component is exactly `**`,
// the one construct where Python's recursive glob has no Go counterpart.
//
// Only a whole-component `**` is recursive in Python; `a**b` collapses to a
// plain `a*b`-style wildcard, which the matcher here already handles.
func hasRecursiveComponent(pattern string) bool {
	for _, component := range strings.Split(pattern, "/") {
		if component == "**" {
			return true
		}
	}
	return false
}

func hasMagic(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func globInternal(pathname string, dirOnly bool) []string {
	dirname, basename := pySplit(pathname)

	if !hasMagic(pathname) {
		if basename != "" {
			if lexists(pathname) {
				return []string{pathname}
			}
			return nil
		}
		// A pattern ending in a separator matches directories only.
		if isDir(dirname) {
			return []string{pathname}
		}
		return nil
	}

	if dirname == "" {
		return joinAll("", globInDir("", basename, dirOnly))
	}

	dirs := []string{dirname}
	if dirname != pathname && hasMagic(dirname) {
		dirs = globInternal(dirname, true)
	}

	var out []string
	for _, dir := range dirs {
		out = append(out, joinAll(dir, globInDir(dir, basename, dirOnly))...)
	}
	return out
}

func joinAll(dir string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, pyJoin(dir, name))
	}
	return out
}

// globInDir lists the names inside dir that match basename, applying Python's
// rule that a pattern not itself starting with `.` never matches a dotfile.
func globInDir(dir, basename string, dirOnly bool) []string {
	if !hasMagic(basename) {
		// `_glob0`: a literal component just has to exist.
		if basename == "" {
			if isDir(dir) {
				return []string{basename}
			}
			return nil
		}
		if lexists(pyJoin(dir, basename)) {
			return []string{basename}
		}
		return nil
	}

	listing := dir
	if listing == "" {
		listing = "."
	}
	entries, err := os.ReadDir(listing)
	if err != nil {
		return nil // an unreadable directory contributes no matches, as in Python
	}

	allowHidden := isHidden(basename)
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !allowHidden && isHidden(name) {
			continue
		}
		if dirOnly && !isDir(pyJoin(dir, name)) {
			continue
		}
		if fnmatch(name, basename) {
			names = append(names, name)
		}
	}
	return names
}

// --------------------------------------------------------------------------- //
// os.path helpers
// --------------------------------------------------------------------------- //

// pySplit is os.path.split: everything up to and including the last separator
// becomes the head, with trailing separators stripped unless the head is all
// separators (so "/a" splits to "/" and "a", not "" and "a").
func pySplit(p string) (head, tail string) {
	i := strings.LastIndexByte(p, '/') + 1
	head, tail = p[:i], p[i:]
	if head != "" && strings.Trim(head, "/") != "" {
		head = strings.TrimRight(head, "/")
	}
	return head, tail
}

// pyJoin is os.path.join for POSIX paths.
func pyJoin(a, b string) string {
	if strings.HasPrefix(b, "/") {
		return b
	}
	if a == "" || strings.HasSuffix(a, "/") {
		return a + b
	}
	return a + "/" + b
}

// lexists is os.path.lexists — true even for a broken symlink, which is why it
// does not follow links. (Such an entry is then dropped by ExpandFiles' isFile
// check, exactly as in Python.)
func lexists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isDir(path string) bool {
	if path == "" {
		path = "."
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isFile is os.path.isfile: follows symlinks, and is true only for a regular
// file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// --------------------------------------------------------------------------- //
// fnmatch
// --------------------------------------------------------------------------- //

// fnmatch reports whether name matches pattern under Python's fnmatch rules:
// `*` matches any run of characters, `?` matches exactly one, `[seq]` and
// `[!seq]` are character classes, and every other character — backslash
// included — is a literal.
func fnmatch(name, pattern string) bool {
	return matchHere([]rune(name), []rune(pattern))
}

func matchHere(name, pattern []rune) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Collapse runs of `*` and try every split point. Patterns here are
			// single path components, so the quadratic worst case is bounded by
			// a filename's length.
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchHere(name[i:], pattern) {
					return true
				}
			}
			return false

		case '?':
			if len(name) == 0 {
				return false
			}
			name, pattern = name[1:], pattern[1:]

		case '[':
			class, rest, ok := parseClass(pattern)
			if !ok {
				// An unterminated `[` is a literal `[` in Python.
				if len(name) == 0 || name[0] != '[' {
					return false
				}
				name, pattern = name[1:], pattern[1:]
				continue
			}
			if len(name) == 0 || !class.matches(name[0]) {
				return false
			}
			name, pattern = name[1:], rest

		default:
			if len(name) == 0 || name[0] != pattern[0] {
				return false
			}
			name, pattern = name[1:], pattern[1:]
		}
	}
	return len(name) == 0
}

type charClass struct {
	negated bool
	ranges  [][2]rune
}

func (c charClass) matches(r rune) bool {
	found := false
	for _, span := range c.ranges {
		if r >= span[0] && r <= span[1] {
			found = true
			break
		}
	}
	return found != c.negated
}

// parseClass reads a `[...]` class from the front of pattern. It mirrors
// fnmatch.translate's scan: an optional leading `!` negates, a `]` in first
// position is a literal member, and the class runs to the next `]`.
func parseClass(pattern []rune) (class charClass, rest []rune, ok bool) {
	i := 1 // past '['
	if i < len(pattern) && pattern[i] == '!' {
		class.negated = true
		i++
	}
	start := i
	if i < len(pattern) && pattern[i] == ']' {
		i++ // a ']' here is a member, not the terminator
	}
	for i < len(pattern) && pattern[i] != ']' {
		i++
	}
	if i >= len(pattern) {
		return charClass{}, nil, false
	}

	body := pattern[start:i]
	for j := 0; j < len(body); j++ {
		// `-` is a range only between two members; leading or trailing it is a
		// literal.
		if j+2 < len(body) && body[j+1] == '-' {
			class.ranges = append(class.ranges, [2]rune{body[j], body[j+2]})
			j += 2
			continue
		}
		class.ranges = append(class.ranges, [2]rune{body[j], body[j]})
	}
	return class, pattern[i+1:], true
}
