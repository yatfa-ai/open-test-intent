package main

// File reading and schema loading.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// CheckFile reads one path and returns the verdict for it.
//
// The 4-tuple shape is deliberate. `kind` is unused by adopter text mode
// — it renders a parse failure and a read failure as the same prose — but a
// consumer of the later --json slice needs to tell them apart, and once the
// result has been flattened to text the distinction is gone for good.
//
// The read is separated from the verdict (CheckJSONBytes below) for exactly one
// caller: a self-test running on a host with no checkout, whose fixture bytes
// come out of the compiled-in corpus and were never on this filesystem. Sharing
// the verdict half rather than writing a second one is what makes an embedded
// run and an in-checkout run produce the same answer by construction — see
// fixtureSource in selftest.go.
func CheckFile(path string, schema *Schema) (valid bool, errs []string, parseError string, kind string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil, "could not read/parse JSON: " + err.Error(), KindRead
	}
	return CheckJSONBytes(data, schema)
}

// CheckJSONBytes is CheckFile with the read already done: everything that
// happens to a document after the bytes are in hand.
//
// It keeps the read-failure prefix even though it never opens anything, because
// the undecodable-bytes case below IS a read failure — the file could not be
// turned into text, so nothing was ever parsed.
func CheckJSONBytes(data []byte, schema *Schema) (valid bool, errs []string, parseError string, kind string) {
	// PROTOCOL.md §1.1 makes UTF-8 part of the definition of a JSON text, and
	// forbids repairing input that is not. Checked explicitly because []byte to
	// string in Go is not a decode: the bytes would flow on and every
	// undecodable one would surface as U+FFFD at the first place something
	// iterated characters, turning a read failure into a successful parse of
	// text the author never wrote.
	if !utf8.Valid(data) {
		return false, nil, "could not read/parse JSON: " + errNotUTF8, KindRead
	}
	instance, err := DecodeJSON(data)
	if err != nil {
		return false, nil, "could not read/parse JSON: " + err.Error(), KindParse
	}
	errs = schema.Validate(instance)
	return len(errs) == 0, errs, "", ""
}

// readSourceText reads a test source file for --source mode.
//
// The error prefix differs from CheckFile's on purpose: "could not read file"
// here, "could not read/parse JSON" there. This mode never parses the file as
// JSON — only the annotation payloads inside it — so borrowing the other prefix
// would name a step that did not happen.
//
// Line endings are left exactly as they are on disk. SplitLines already treats
// CRLF as one terminator and a lone CR as a terminator in its own right, so
// normalising them first would change nothing and be a second thing to keep
// correct.
func readSourceText(path string) (text string, readError string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "could not read file: " + err.Error()
	}
	return decodeSourceText(data)
}

// decodeSourceText is readSourceText with the read already done — the decode
// half, shared with the self-test's embedded corpus for the reason CheckFile
// and CheckJSONBytes are split.
func decodeSourceText(data []byte) (text string, readError string) {
	if !utf8.Valid(data) {
		// Undecodable bytes are a READ failure and the file never reaches the
		// extractor. Scanning them anyway would mean scanning U+FFFD in place
		// of every one, and reporting annotations from text nobody wrote.
		return "", "could not read file: " + errNotUTF8
	}
	return string(data), ""
}

// SchemaPath is where a run LOOKS FOR the schema: <exe>/../schemas/open-test-intent.v1.json.
//
// Deriving it from the executable's own location is what makes a checkout work
// without configuration — a binary built into bin/ finds the schema beside it.
// It is where the schema is looked for, which is not the same claim as where it
// is FOUND: see LoadSchema for the fallback and the rule governing it.
func SchemaPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(filepath.Dir(exe))
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

// LoadSchema loads the schema this run will enforce. It returns the origin
// alongside the error so the caller's diagnostic can name the file that failed.
//
// CompileSchema compiles every `pattern` keyword up front and refuses the whole
// schema if one of them will not compile, rather than letting a pattern fail
// silently mid-verdict. See schemadoc.go.
//
// # The embedded fallback, and why it is a fallback
//
// SchemaPath derives its answer from the executable's own location. That is
// right in a checkout and wrong for a distributable binary, whose whole purpose
// is to live somewhere else: installed at /usr/local/bin/validate-intent it computed
// /usr/local/schemas/open-test-intent.v1.json, found nothing, and exited 2 in
// every working mode — adopter, --source, self-test, --json. Only --help
// worked. A released binary that installs cleanly and then fails at everything
// it exists to do (SPGD-131).
//
// The obvious fix — embed the schema and stop reading disk — is WRONG here, and
// the reason is worth stating because it is invisible from this file.
// The validator implements keywords the shipped schema does not declare
// (`pattern`, numeric bounds, `items`), and the only way to exercise them is to
// point the binary at a schema that DOES declare them — which fileio_schema_test.go
// does by planting one in a throwaway tree. A pure embed would make the binary
// ignore every such schema while the tests went on reporting green: coverage
// deleted without a single case going red, which is this project's house defect
// wearing a new hat.
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
// The error is returned as-is rather than being reworded: os.PathError already
// names the operation and the path, which is the whole content of a useful
// "could not load schema" line.
func loadSchemaFrom(path string) (*Schema, SchemaSource, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if !errors.Is(readErr, fs.ErrNotExist) {
			// Present, but we could not have it. Not our call to substitute.
			// No bytes, so no digest: see SchemaSource on why that field is
			// empty here rather than the digest of an empty input.
			return nil, SchemaSource{Origin: path}, readErr
		}
		data = opentestintent.SchemaJSON()
		path = EmbeddedSchemaLabel
	}
	source := SchemaSource{Origin: path, SHA256: opentestintent.SHA256Hex(data)}
	if !utf8.Valid(data) {
		return nil, source, errors.New(errNotUTF8)
	}
	root, err := DecodeJSON(data)
	if err != nil {
		return nil, source, err
	}
	schema, err := CompileSchema(root)
	if err != nil {
		return nil, source, err
	}
	return schema, source, nil
}
