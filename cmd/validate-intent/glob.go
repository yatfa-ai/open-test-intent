package main

// Path expansion for every mode that takes FILE/glob arguments.
//
// Go's filepath.Glob is not usable here, and the reasons are behavioural rather
// than stylistic:
//
//   - it has no recursive `**`, which the validator documents and adopters use
//     to point at a whole spec tree;
//   - its `*` matches dotfiles, so `examples/*.json` would pick up an editor's
//     `.draft.json` swap file and validate it;
//   - it treats `\` as an escape inside a pattern, so a path containing one
//     cannot be named literally;
//   - it rejects an unterminated `[` with ErrBadPattern instead of reading it
//     as the literal bracket a real filename can contain.
//
// So the matcher is written here. The rules it implements are the ones stated
// in `--help` and exercised by the fixture corpus; there is no other authority
// for them and none is needed.
//
//	*        any run of characters within one path component
//	?        exactly one character
//	[abc]    a character class; [!abc] negates it; an unterminated [ is literal
//	**       as a WHOLE component, every descendant directory including none
//	         (so `spec/**/a.json` finds `spec/a.json`)
//	         — a `**` with anything else in the component is a plain wildcard
//	.        a pattern not itself starting with `.` never matches a dotfile,
//	         at any level of a `**` descent
//
// Ordering is settled by ExpandFiles, which sorts, so nothing below has to
// produce matches in any particular order.

import (
	"os"
	"sort"
	"strings"
	"unicode/utf8"
)

// ExpandFiles expands one pattern, sorts the matches, and drops anything that
// is not a regular file.
//
// Every glob-expanding mode goes through here, so a pattern like `intents/*`
// never hands a DIRECTORY to a reader — which would then report it as an
// unreadable file rather than skipping it.
func ExpandFiles(pattern string) []string {
	matches := Glob(pattern)
	sort.Strings(matches)
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		if isFile(match) {
			files = append(files, match)
		}
	}
	return files
}

// Glob returns every path matching pattern, unsorted and unfiltered.
func Glob(pattern string) []string {
	matches := globPath(pattern, false)

	// A pattern that STARTS with `**` reaches globRecursive with no directory
	// to join onto, so its zero-segment match would surface as a literal "" —
	// which is not a path. Only a leading empty element is dropped; every other
	// match is real.
	if pattern == "" || strings.HasPrefix(pattern, "**") {
		if len(matches) > 0 && matches[0] == "" {
			matches = matches[1:]
		}
	}
	return matches
}

// isRecursiveComponent reports whether a path component is exactly `**`.
//
// Only a whole component is recursive: `a**b` is a plain wildcard, which is why
// `spec/re**s/*.json` finds `spec/requests/c.json` and does not descend.
func isRecursiveComponent(pattern string) bool {
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

func globPath(pathname string, dirOnly bool) []string {
	dirname, basename := splitPath(pathname)

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
		dirs = globPath(dirname, true)
	}

	var out []string
	for _, dir := range dirs {
		out = append(out, joinAll(dir, globComponent(dir, basename, dirOnly))...)
	}
	return out
}

// globComponent expands ONE pattern component inside dir, returning names
// relative to dir.
func globComponent(dir, basename string, dirOnly bool) []string {
	if isRecursiveComponent(basename) {
		return globRecursive(dir, dirOnly)
	}
	return globInDir(dir, basename, dirOnly)
}

// globRecursive expands a whole-component `**`.
//
// The EMPTY name comes first, and it is the case a hand-rolled recursion
// usually drops because `**` reads like "at least one directory": `**` matches
// ZERO segments, which is the only reason `spec/**/a.json` finds
// `spec/a.json`. Joining "" onto dir produces the `spec/` prefix the caller
// then globs `a.json` inside of.
//
// It is emitted only when dir exists and is a directory, so `nope/**` stays a
// no-match rather than becoming a match on `nope/`. Pinning that guard needs a
// pattern whose LAST component is the recursive one — a `nope/**/*.json` shape
// answers empty either way, and ExpandFiles' isFile filter hides the difference
// at the CLI. See the `nope/**` cases in glob_test.go.
func globRecursive(dir string, dirOnly bool) []string {
	matches := []string{}
	if dir == "" || isDir(dir) {
		matches = append(matches, "")
	}
	return append(matches, descendants(dir, dirOnly)...)
}

// descendants lists every descendant of dir, named relative to dir.
//
// Three properties here are load-bearing, and the obvious rewrite —
// filepath.WalkDir plus a suffix filter — loses two of them while producing a
// plausible number of paths on a plausible tree. Count is not evidence; only
// the sorted path list is.
//
//   - The hidden rule applies at EVERY level, not only to the final component.
//     A hidden directory is neither yielded nor entered, so `spec/**/*.json`
//     cannot see `spec/.secret/f.json`. Naming the component literally still
//     reaches it — that goes through globInDir, not here.
//
//   - listDir classifies with os.Stat, which FOLLOWS symlinks. fs.WalkDir does
//     not, and a matcher built on it silently skips every test file under a
//     symlinked directory while still reporting a confident clean pass.
//
//   - There is deliberately no visited-inode guard. A symlink loop terminates
//     when the OS refuses the too-deep path and the failed read contributes no
//     names. Cutting the walk off earlier would be a silent divergence from the
//     set of files the user can actually name.
func descendants(dir string, dirOnly bool) []string {
	var out []string
	for _, name := range listDir(dir, dirOnly) {
		if isHidden(name) {
			continue
		}
		out = append(out, name)

		child := name
		if dir != "" {
			child = joinPath(dir, name)
		}
		for _, descendant := range descendants(child, dirOnly) {
			out = append(out, joinPath(name, descendant))
		}
	}
	return out
}

// listDir lists the names directly inside dir, restricted to directories when
// dirOnly. Anything unreadable — a missing path, a plain file, a symlink loop
// that has run out of the kernel's link budget — contributes no names rather
// than failing the run.
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
		// isDir follows symlinks: a symlink to a directory IS a directory here,
		// so `spec/**` descends through one.
		if dirOnly && !isDir(joinPath(dir, name)) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func joinAll(dir string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, joinPath(dir, name))
	}
	return out
}

// globInDir returns the names inside dir matching basename, applying the rule
// that a pattern not itself starting with `.` never matches a dotfile.
func globInDir(dir, basename string, dirOnly bool) []string {
	if !hasMagic(basename) {
		// A literal component just has to exist.
		if basename == "" {
			if isDir(dir) {
				return []string{basename}
			}
			return nil
		}
		if lexists(joinPath(dir, basename)) {
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
		if matchName(name, basename) {
			names = append(names, name)
		}
	}
	return names
}

// --------------------------------------------------------------------------- //
// path helpers
// --------------------------------------------------------------------------- //

// splitPath splits at the last separator. Trailing separators are stripped from
// the head unless the head is all separators, so "/a" splits to "/" and "a"
// rather than to "" and "a".
func splitPath(p string) (head, tail string) {
	i := strings.LastIndexByte(p, '/') + 1
	head, tail = p[:i], p[i:]
	if head != "" && strings.Trim(head, "/") != "" {
		head = strings.TrimRight(head, "/")
	}
	return head, tail
}

// joinPath joins two POSIX path fragments.
func joinPath(a, b string) string {
	if strings.HasPrefix(b, "/") {
		return b
	}
	if a == "" || strings.HasSuffix(a, "/") {
		return a + b
	}
	return a + "/" + b
}

// lexists does not follow symlinks, so a broken link still counts as present.
// ExpandFiles then drops it, because it is not a regular file.
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

// isFile follows symlinks and is true only for a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// --------------------------------------------------------------------------- //
// the matcher
// --------------------------------------------------------------------------- //

// matchName reports whether name matches a single-component pattern.
func matchName(name, pattern string) bool {
	return matchRunes(pathRunes(name), pathRunes(pattern))
}

// pathRunes decodes a path fragment into characters, keeping each byte of an
// ill-formed sequence distinct.
//
// A filename is a byte string and need not be valid UTF-8. Plain []rune maps
// EVERY such byte to U+FFFD, which would make `x\xe9.json` and `x\xff.json`
// compare equal — to `?`, and to each other — so two different files would
// match a pattern intended for one and be reported under one name. Each
// undecodable byte becomes a distinct negative sentinel instead: it still
// counts as exactly one character for `?`, it compares equal only to the same
// byte, and it can never collide with a real code point or fall inside a
// character-class range.
func pathRunes(s string) []rune {
	if utf8.ValidString(s) {
		return []rune(s)
	}
	runes := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			runes = append(runes, -rune(s[i]))
			i++
			continue
		}
		runes = append(runes, r)
		i += size
	}
	return runes
}

func matchRunes(name, pattern []rune) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Collapse runs of `*` and try every split point. A pattern here is
			// one path component, so the quadratic worst case is bounded by a
			// filename's length.
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchRunes(name[i:], pattern) {
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
			class, rest, ok := parseCharClass(pattern)
			if !ok {
				// An unterminated `[` is a literal `[`.
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

// parseCharClass reads a `[...]` class from the front of pattern: an optional
// leading `!` negates, a `]` in first position is a literal member, and the
// class runs to the next `]`.
func parseCharClass(pattern []rune) (class charClass, rest []rune, ok bool) {
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
