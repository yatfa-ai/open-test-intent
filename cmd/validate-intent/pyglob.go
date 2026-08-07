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
// All five are reproduced here. The fifth was refused loudly in main.go rather
// than approximated until this slice, and the *approximation* is still
// forbidden: a `**` downgraded to a single `*` would quietly check a smaller
// set of files and still report a clean pass. What landed instead is the real
// recursive walk — see globRecursive and rListDir.
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

// PyGlob mirrors `glob.glob(pattern, recursive=True)` — the exact call the
// reference makes at bin/validate-intent:464.
func PyGlob(pattern string) []string {
	matches := globInternal(pattern, false)

	// `iglob`'s leading-empty-string skip (CPython Lib/glob.py). A pattern
	// whose FIRST two characters are `**` reaches globRecursive with no
	// directory to join onto, so its zero-segment match surfaces as a literal
	// "" rather than as a path — `glob.glob('**')` must not answer with an
	// empty string. Only an empty leading element is dropped; anything else is
	// a real match and stays.
	if pattern == "" || strings.HasPrefix(pattern, "**") {
		if len(matches) > 0 && matches[0] == "" {
			matches = matches[1:]
		}
	}
	return matches
}

// hasRecursiveComponent reports whether any path component is exactly `**`,
// the discriminator between Python's recursive glob and an ordinary wildcard.
//
// Only a whole-component `**` is recursive in Python; `a**b` collapses to a
// plain `a*b`-style wildcard, which fnmatch below already handles. That is why
// `spec/re**s/*.json` finds only `spec/requests/c.json` and does not descend.
//
// globInternal calls this on a single path component, where it is exactly
// CPython's `_isrecursive`; it also reads correctly on a whole pattern, which
// is how main.go used it while `**` was still refused.
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
		return joinAll("", globComponent("", basename, dirOnly))
	}

	dirs := []string{dirname}
	if dirname != pathname && hasMagic(dirname) {
		dirs = globInternal(dirname, true)
	}

	var out []string
	for _, dir := range dirs {
		out = append(out, joinAll(dir, globComponent(dir, basename, dirOnly))...)
	}
	return out
}

// globComponent expands ONE pattern component inside dir, returning names
// relative to dir. It is CPython's choice between `_glob2` (a whole-component
// `**`) and `_glob1`/`_glob0` (everything else).
func globComponent(dir, basename string, dirOnly bool) []string {
	if hasRecursiveComponent(basename) {
		return globRecursive(dir, dirOnly)
	}
	return globInDir(dir, basename, dirOnly)
}

// globRecursive is `_glob2`: the whole-component `**`.
//
// The empty name comes FIRST, and it is the case a hand-rolled recursion almost
// always drops because `**` reads like "at least one directory": `**` matches
// ZERO segments, which is the only reason `spec/**/a.json` finds `spec/a.json`.
// Joining "" onto dir is what produces the `spec/` prefix the caller then globs
// `a.json` inside of.
//
// It is emitted only when dir actually exists AND is a directory, so `nope/**`
// stays a no-match rather than becoming a match on `nope/`. CPython 3.13 gained
// exactly this guard (`if not dirname or _isdir(dirname, dir_fd)`); older
// versions yield unconditionally.
//
// Pinning it takes a pattern whose LAST component is the recursive one. A
// `nope/**/*.json` shape cannot show it: the trailing `*.json` finds nothing
// inside a directory that does not exist, so it answers empty with or without
// the guard. ExpandFiles' isFile filter likewise hides the difference at the
// CLI. See the `nope/**` and `spec/a.json/**` cases in pyglob_test.go — they
// are the only thing standing between this guard and a well-meaning deletion.
func globRecursive(dir string, dirOnly bool) []string {
	matches := []string{}
	if dir == "" || isDir(dir) {
		matches = append(matches, "")
	}
	return append(matches, rListDir(dir, dirOnly)...)
}

// rListDir is `_rlistdir`: every descendant of dir, named relative to dir.
//
// Three properties here are load-bearing, and the natural Go rewrite —
// filepath.WalkDir plus a suffix filter — loses two of them while producing the
// same NUMBER of paths as Python on a plausible tree. Count is not evidence
// here; only the sorted path list is.
//
//   - The hidden rule applies at EVERY level of the descent, not just to the
//     final component. A hidden directory is neither yielded nor entered, so
//     nothing beneath it is reachable — `spec/.secret/f.json` is invisible to
//     `spec/**/*.json`. (Naming the component literally still reaches it:
//     that goes through globInDir's `_glob0` path, not this one.)
//
//   - listDir classifies with os.Stat, which FOLLOWS symlinks, exactly as
//     CPython's `entry.is_dir()` does. fs.WalkDir does not follow them, and a
//     port built on it silently skips every test file under a symlinked
//     directory while still reporting a confident clean pass.
//
//   - There is deliberately no visited-inode guard. CPython has none either, so
//     a symlink loop must terminate the way it does there: when the OS refuses
//     the too-deep path with ELOOP and the failed read contributes no names.
//     Cutting the walk off earlier would be its own silent divergence.
func rListDir(dir string, dirOnly bool) []string {
	var out []string
	for _, name := range listDir(dir, dirOnly) {
		if isHidden(name) {
			continue
		}
		out = append(out, name)

		child := name
		if dir != "" {
			child = pyJoin(dir, name)
		}
		for _, descendant := range rListDir(child, dirOnly) {
			out = append(out, pyJoin(name, descendant))
		}
	}
	return out
}

// listDir is `_iterdir`: the names directly inside dir, restricted to
// directories when dirOnly. Anything unreadable — a missing path, a file, a
// symlink loop that has run out of the kernel's link budget — contributes no
// names rather than failing the run, as in Python.
func listDir(dir string, dirOnly bool) []string {
	listing := dir
	if listing == "" {
		listing = "."
	}
	entries, err := os.ReadDir(listing)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		// isDir follows symlinks, as os.DirEntry.is_dir() does in Python: a
		// symlink to a directory IS a directory to the glob.
		if dirOnly && !isDir(pyJoin(dir, name)) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func joinAll(dir string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, pyJoin(dir, name))
	}
	return out
}

// globInDir is `_glob1`/`_glob0`: the names inside dir that match basename,
// applying Python's rule that a pattern not itself starting with `.` never
// matches a dotfile.
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

	allowHidden := isHidden(basename)
	var names []string
	for _, name := range listDir(dir, dirOnly) {
		if !allowHidden && isHidden(name) {
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
