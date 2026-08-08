package main

// File reading, schema loading, and the Python-flavoured error prose that goes
// with them.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// CheckFile is the port of `check_file` (bin/validate-intent:219-247).
//
// The 4-tuple shape is kept deliberately. `kind` is unused by adopter text mode
// — it renders a parse failure and a read failure as the same prose — but a
// consumer of the later --json slice needs to tell them apart, and once the
// result has been flattened to text the distinction is gone for good.
func CheckFile(path string, schema *Schema) (valid bool, errs []string, parseError string, kind string) {
	data, err := os.ReadFile(pyFSEncode(path))
	if err != nil {
		return false, nil, "could not read/parse JSON: " + pyOSError(err), KindRead
	}
	// Python opens with encoding="utf-8", so undecodable bytes raise
	// UnicodeDecodeError during the read and never reach the parser. Go would
	// silently substitute U+FFFD instead, turning a read failure into a
	// successful parse of different text — check explicitly.
	if !utf8.Valid(data) {
		return false, nil, "could not read/parse JSON: " + pyUnicodeDecodeError(data), KindRead
	}
	instance, err := DecodeOrdered(data)
	if err != nil {
		return false, nil, "could not read/parse JSON: " + err.Error(), KindParse
	}
	errs = schema.Validate(instance)
	return len(errs) == 0, errs, "", ""
}

// readSourceText reads a test source file the way check_source_file does
// (bin/validate-intent:434-438).
//
// The error prefix differs from CheckFile's on purpose: the reference says
// "could not read file: %s" here and "could not read/parse JSON: %s" there, and
// both strings are part of the output contract.
//
// On Python's universal-newline translation, which this deliberately does NOT
// implement: open(..., encoding="utf-8") rewrites "\r\n" and a lone "\r" to
// "\n" before the caller sees the text. That is provably unobservable here —
// the only consumer is pySplitlines, which already treats "\r\n" as a single
// terminator and "\r" as a terminator in its own right, so the line sequence is
// identical either way. Implementing it would be a second thing to keep correct
// for no change in behaviour; leaving it out silently would be a divergence
// nobody had checked. It is checked, and it is out.
func readSourceText(path string) (text string, readError string) {
	data, err := os.ReadFile(pyFSEncode(path))
	if err != nil {
		return "", "could not read file: " + pyOSError(err)
	}
	if !utf8.Valid(data) {
		// Python decodes during the read, so undecodable bytes are a *read*
		// failure and the file never reaches the extractor. Go would silently
		// substitute U+FFFD and happily scan the result.
		return "", "could not read file: " + pyUnicodeDecodeError(data)
	}
	return string(data), ""
}

// SchemaPath is the port of the SCHEMA_PATH constant
// (bin/validate-intent:76-77): the repo root is the parent of the directory
// holding the executable, so a Go binary built into `bin/` resolves the schema
// to exactly the path the Python script does — which is what lets the
// "could not load schema" diagnostic be byte-identical too.
//
// This path is where the schema is LOOKED FOR, which is no longer the same
// claim as where it is FOUND. See LoadSchema.
//
// The executable's own path is bytes like any other, so it is decoded here —
// os.Executable() returns whatever the kernel holds, which on an install
// directory whose name is not valid UTF-8 is not a UTF-8 string. Decoding it
// puts this path in the same space as every other path in the port (see
// pyfspath.go), which is what the two consumers below need:
//
//   - loadSchemaFrom re-encodes it for the syscall, so the file is still
//     opened. That is the round trip, not a formality.
//   - pyOSError reprs it for the `[Errno ...]` clause of the "could not load
//     schema" diagnostic, and PyReprString on a DECODED path produces the
//     reference's own `\udce9` escapes.
//
// What decoding does NOT fix, and the comment here used to claim it did: the
// other half of that same diagnostic interpolates this string with a bare `%s`
// (schemaLoadError in main.go), and Go writes a surrogate as its three WTF-8
// bytes where CPython's stderr — `backslashreplace` by default, and NOT the
// handler this port models for stdout — writes six ASCII characters. That is a
// real, measured divergence. It is declared as excluded group 7 in
// tests/parity/run_parity.sh and pinned, both halves, in its section 16e.
//
// (There was never a U+FFFD on this path to fix, either: filepath.Dir does not
// iterate runes, so an undecoded byte reached the diagnostic intact. It just
// reached it as one raw byte instead of three.)
func SchemaPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(filepath.Dir(pyFSDecodeString(exe)))
	return filepath.Join(root, "schemas", "open-test-intent.v1.json"), nil
}

// EmbeddedSchemaLabel is what LoadSchema reports as the schema's origin when it
// falls back to the compiled-in copy. It is not a path and is not meant to look
// like one: it appears only in a diagnostic about the EMBEDDED schema, and
// printing a filesystem path there would send a reader to a file that had
// nothing to do with the failure.
const EmbeddedSchemaLabel = "<embedded schema>"

// SchemaSource is what a load RESOLVED TO: where the schema came from, and what
// the bytes that came from there digest to.
//
// It exists because those two facts were previously either discarded or
// unobtainable. loadSchemaFrom already returned the origin, but main.go read it
// only inside the error branch — a run that exited 0 never said which schema
// produced the verdict — and the bytes were dropped on the floor after decoding,
// so the digest could not be asked for at all. The only digest the binary could
// report was opentestintent.SchemaSHA256, a pure fold of the COMPILED-IN copy
// that by construction cannot know a schema beside the executable won.
//
// Carried as a struct rather than a third and fourth return value because the
// two are one answer and are always right or wrong together: an origin without
// the digest of what was read there is exactly the claim that was already
// available and already insufficient.
//
//   - Origin is the absolute path the schema was read from, or
//     EmbeddedSchemaLabel when the file was ABSENT and the compiled-in copy was
//     substituted. It is the same string the "could not load schema" diagnostic
//     names, so the failing and succeeding reports point at the same place.
//
//   - SHA256 is opentestintent.SHA256Hex of the bytes actually loaded — the file's
//     bytes when disk won, the embedded bytes when it did not. It is EMPTY, and
//     only empty, when the read itself failed: there were no bytes, and "" is
//     not the digest of anything (the digest of zero bytes is a real, different
//     64-hex value). A caller printing this field must not print it blank.
type SchemaSource struct {
	Origin string
	SHA256 string
}

// LoadSchema is the port of `load_schema` (bin/validate-intent:250-252). It
// returns the schema's origin alongside the error so the caller can render the
// reference's diagnostic verbatim.
//
// It does one thing the reference does not: CompileSchema translates and
// compiles every `pattern` keyword up front, and refuses the whole schema if
// any of them cannot be given Python's exact meaning under Go's RE2 engine.
// That refusal is a Go-only exit 2 — Python would have loaded the schema fine —
// and it is deliberate. The alternative is a binary that loads happily and then
// disagrees with the reference about whether a document is valid. See
// pypattern.go for what is accepted, what is refused, and why.
//
// # The embedded fallback, and why it is a fallback
//
// SchemaPath derives its answer from the executable's own location. That is
// right for a Python script that always lives at <repo>/bin/ and wrong for a
// distributable binary: installed at /usr/local/bin/validate-intent it computed
// /usr/local/schemas/open-test-intent.v1.json, found nothing, and exited 2 in
// every working mode — adopter, --source, self-test, --json. Only --help
// worked. A released binary that installs cleanly and then fails at everything
// it exists to do (SPGD-131).
//
// The obvious fix — embed the schema and stop reading disk — is WRONG here, and
// the reason is worth stating because it is invisible from this file.
// tests/parity/run_parity.sh proves the validator keywords the shipped schema
// does not declare (`pattern`, numeric bounds, `items`) by copying both
// implementations into a throwaway tree carrying a schema of its choosing. Its
// compare_root and assert_pattern_refusal cases — the large majority of the
// suite's schema coverage — work ONLY because the binary reads a schema from
// beside itself. A pure embed would make it ignore every one of those synthetic
// schemas while the harness went on reporting green: coverage deleted without a
// single case going red, which is this project's house defect wearing a new hat.
// (No count is given on purpose. It moves whenever cases are added, and a
// stale figure in a comment reads like evidence long after it has stopped
// being any.)
//
// So disk wins whenever there is a file to win with, and the ABSENT/UNREADABLE
// distinction carries the whole design:
//
//   - ABSENT (ENOENT) — nobody has expressed an intention about this path, so
//     the compiled-in copy is used. This is the release-asset case, and the
//     only behaviour that changes.
//
//   - PRESENT but unreadable, undecodable, malformed, or uncompilable — fail
//     exactly as before. A file that EXISTS is a deliberate override, and
//     falling back past a broken one would answer the user's own mistake with a
//     confident, clean, wrong report: a tool failure wearing the costume of a
//     content failure. Worse here than usual, because the fallback would
//     succeed — the run would go green against a schema the user never asked
//     for and cannot see.
//
// Only ENOENT counts as absent. EACCES, EISDIR and ENOTDIR all mean something
// is there, so they all fail.
//
// # What the caller gets back, and why it is more than a path
//
// The SchemaSource returned alongside the schema names the origin AND digests
// the bytes that were loaded from it, so the question "which contract did this
// run enforce?" has an answer for the first time. `--schema-source`
// (schemasource.go) is that answer's only consumer today; the verdict path uses
// the origin exactly as before, in its diagnostic.
func LoadSchema() (*Schema, SchemaSource, error) {
	path, err := SchemaPath()
	if err != nil {
		return nil, SchemaSource{}, err
	}
	return loadSchemaFrom(path)
}

// loadSchemaFrom is LoadSchema with the path injected, so the absent/present
// rule above can be asserted directly in a unit test rather than only through a
// harness case. That matters more than the usual testability argument: the
// harness's present-but-unreadable case is chmod 000, which does nothing when
// the suite runs as root, and it SKIPs rather than fails. Verifying "a broken
// schema does not fall back" by watching a skipped case not go red would be a
// vacuous check of the fix for a vacuous check.
//
// The digest is taken from `data` at the moment the bytes are in hand and before
// anything is done with them, so it is provably the digest of what was decoded
// and compiled rather than of a second read that could see a different file.
//
// `path` arrives DECODED (SchemaPath ran the executable's own path through
// os.fsdecode), so it is re-encoded here like every other syscall argument —
// see pyfspath.go. Without that, an install directory whose name is not valid
// UTF-8 would be named correctly in the diagnostic and then not opened.
func loadSchemaFrom(path string) (*Schema, SchemaSource, error) {
	data, readErr := os.ReadFile(pyFSEncode(path))
	if readErr != nil {
		if !errors.Is(readErr, fs.ErrNotExist) {
			// Present, but we could not have it. Not our call to substitute.
			// No bytes, so no digest: see SchemaSource on why that field is
			// empty here rather than the digest of an empty input.
			return nil, SchemaSource{Origin: path}, errors.New(pyOSError(readErr))
		}
		data = opentestintent.SchemaJSON()
		path = EmbeddedSchemaLabel
	}
	source := SchemaSource{Origin: path, SHA256: opentestintent.SHA256Hex(data)}
	if !utf8.Valid(data) {
		return nil, source, errors.New(pyUnicodeDecodeError(data))
	}
	root, err := DecodeOrdered(data)
	if err != nil {
		return nil, source, err
	}
	schema, err := CompileSchema(root)
	if err != nil {
		return nil, source, err
	}
	return schema, source, nil
}

// --------------------------------------------------------------------------- //
// Python exception prose
// --------------------------------------------------------------------------- //

// pyOSError renders an os error the way str(OSError) does in Python:
//
//	[Errno 2] No such file or directory: '/path/to/thing'
//
// Go's syscall.Errno carries the same strerror text but lower-cased, and the
// filename is repr'd rather than bare — both are reproduced here so the
// "could not load schema" and unreadable-file diagnostics match the reference
// byte for byte rather than being a documented divergence.
//
// The path is DECODED before it is repr'd. os.PathError carries back exactly
// the bytes the syscall was given, which pyFSEncode had turned back into raw
// bytes — repr'ing those directly would render `x\udce9.json` as `x\ufffd.json`
// in the one message whose whole job is to name the file that failed.
func pyOSError(err error) string {
	var pathErr *os.PathError
	var errno syscall.Errno
	if errors.As(err, &pathErr) && errors.As(err, &errno) {
		return fmt.Sprintf("[Errno %d] %s: %s",
			int(errno), capitalizeFirst(errno.Error()),
			PyReprString(pyFSDecodeString(pathErr.Path)))
	}
	return err.Error()
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
