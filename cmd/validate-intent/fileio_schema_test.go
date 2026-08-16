package main

// Unit tests for the schema-resolution rule in fileio.go: disk when present,
// the embedded copy only when the file is ABSENT.
//
// WHY THESE EXIST AS UNIT TESTS AND NOT ONLY AS HARNESS CASES
// ===========================================================
//
// A CLI-level check of the present-but-unreadable case runs chmod 000 and
// inspects the diagnostic. Under root, chmod 000 does not make a
// file unreadable, so that case SKIPs — it prints a red SKIP line and moves on.
// A skipped case cannot go red, so "the fix does not fall back past a broken
// schema" would be established by watching a check that never ran fail to
// complain: a vacuous green about the fix for a vacuous green. CI containers in
// this project run as root often enough that this is not hypothetical.
//
// So the rule is asserted here too, against loadSchemaFrom directly, where the
// path is an argument rather than a function of os.Executable(). Three of the
// present-but-broken cases below (malformed, EISDIR, ENOTDIR) need no
// permission trickery at all and therefore hold under root as well — the
// permission case adds EACCES specifically and skips only itself.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// canonicalDoc is a document the shipped schema accepts.
const canonicalDoc = `{"entity":"Order","action":"total","behavior":"sums the line items with tax","layer":"unit"}`

// permissiveSchema accepts documents the canonical schema REJECTS — it requires
// nothing and forbids nothing. Using a schema that merely differs would not
// distinguish "the override was read" from "the embedded copy was used and
// happened to agree"; this one answers `{}` the opposite way.
const permissiveSchema = `{"type": "object"}`

// writeSchema puts content at <dir>/schemas/open-test-intent.v1.json and
// returns that path.
func writeSchema(t *testing.T, dir, content string) string {
	t.Helper()
	schemaDir := filepath.Join(dir, "schemas")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(schemaDir, "open-test-intent.v1.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return path
}

// mustDecode lives in validator_test.go — this file reuses it rather than keeping a
// second decoder helper that could drift from it.

// ABSENT — the release-asset case, and the only behaviour this slice changes.
func TestLoadSchemaFromFallsBackWhenTheFileIsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schemas", "open-test-intent.v1.json")

	schema, source, err := loadSchemaFrom(path)
	if err != nil {
		t.Fatalf("expected the embedded fallback, got error: %v", err)
	}
	if source.Origin != EmbeddedSchemaLabel {
		t.Errorf("origin = %q, want %q", source.Origin, EmbeddedSchemaLabel)
	}

	// Loading is not the claim; validating with the right rules is. The
	// embedded copy must be the *canonical* schema, so it accepts a conforming
	// document and rejects an empty one.
	if errs := schema.Validate(mustDecode(t, canonicalDoc)); len(errs) != 0 {
		t.Errorf("the embedded schema rejected a conforming document: %v", errs)
	}
	if errs := schema.Validate(mustDecode(t, `{}`)); len(errs) == 0 {
		t.Error("the embedded schema accepted `{}`, which the canonical schema requires four properties of")
	}
}

// An absent schema whose whole PARENT CHAIN is missing is the real install
// case: /usr/local/schemas/ does not exist, not merely the file inside it.
func TestLoadSchemaFromFallsBackWhenTheSchemasDirectoryIsAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no", "such", "tree", "schemas", "open-test-intent.v1.json")

	_, source, err := loadSchemaFrom(path)
	if err != nil {
		t.Fatalf("expected the embedded fallback, got error: %v", err)
	}
	if source.Origin != EmbeddedSchemaLabel {
		t.Errorf("origin = %q, want %q", source.Origin, EmbeddedSchemaLabel)
	}
}

// PRESENT and valid — disk wins. Asserted with a schema that answers a document
// the OPPOSITE way from the canonical one, so a fallback that silently ignored
// the override could not pass this by agreeing.
func TestLoadSchemaFromPrefersAPresentFileOverTheEmbeddedCopy(t *testing.T) {
	dir := t.TempDir()
	path := writeSchema(t, dir, permissiveSchema)

	schema, source, err := loadSchemaFrom(path)
	if err != nil {
		t.Fatalf("loading the override: %v", err)
	}
	if source.Origin != path {
		t.Errorf("origin = %q, want the on-disk path %q", source.Origin, path)
	}
	if errs := schema.Validate(mustDecode(t, `{}`)); len(errs) != 0 {
		t.Errorf("the on-disk override was not used: it rejected `{}` the way the embedded copy would: %v", errs)
	}
}

// The custom-schema assertions in this file depend on exactly the
// property above, through the executable-relative path rather than a direct
// call. This is that path, end to end, minus the subprocess.
func TestLoadSchemaPrefersASchemaBesideTheExecutable(t *testing.T) {
	if _, err := os.Executable(); err != nil {
		t.Skipf("os.Executable() unavailable: %v", err)
	}
	path, err := SchemaPath()
	if err != nil {
		t.Fatalf("SchemaPath: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Skipf("a real schema already sits at %s; this test needs to place its own", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Skipf("cannot create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(permissiveSchema), 0o644); err != nil {
		t.Skipf("cannot write %s: %v", path, err)
	}
	t.Cleanup(func() { os.Remove(path) })

	schema, source, err := LoadSchema()
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}
	if source.Origin != path {
		t.Errorf("origin = %q, want %q", source.Origin, path)
	}
	if errs := schema.Validate(mustDecode(t, `{}`)); len(errs) != 0 {
		t.Errorf("LoadSchema ignored the schema beside the executable: %v", errs)
	}
}

// PRESENT but broken — must fail, never fall back. Each row is a different
// errno or failure stage, and none of the first three needs a permission bit,
// so they hold when the suite runs as root.
func TestLoadSchemaFromDoesNotFallBackPastAPresentButBrokenSchema(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSchema(t, dir, "{ this is not json")

		_, source, err := loadSchemaFrom(path)
		if err == nil {
			t.Fatal("a malformed schema was silently replaced by the embedded copy")
		}
		if source.Origin != path {
			t.Errorf("origin = %q, want the offending path %q", source.Origin, path)
		}
	})

	t.Run("the schema path is a directory (EISDIR)", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "schemas", "open-test-intent.v1.json")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if _, _, err := loadSchemaFrom(path); err == nil {
			t.Fatal("a directory at the schema path was treated as an absent schema")
		}
	})

	t.Run("schemas/ is a file, not a directory (ENOTDIR)", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "schemas"), []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		path := filepath.Join(dir, "schemas", "open-test-intent.v1.json")

		// Guard the premise: if the platform ever started reporting this as
		// ENOENT the assertion below would be testing a different thing while
		// still reading like this one.
		_, readErr := os.ReadFile(path)
		if readErr == nil {
			t.Fatal("reading through a non-directory unexpectedly succeeded")
		}
		if errors.Is(readErr, os.ErrNotExist) {
			t.Skipf("this platform reports a non-directory parent as ErrNotExist (%v); "+
				"the absent/present distinction cannot be drawn here", readErr)
		}

		if _, _, err := loadSchemaFrom(path); err == nil {
			t.Fatal("a non-directory schemas/ was treated as an absent schema")
		}
	})

	t.Run("unreadable (EACCES)", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSchema(t, dir, permissiveSchema)
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { os.Chmod(path, 0o644) })

		// Under root, chmod 000 does not make a file unreadable. Skipping is
		// honest here BECAUSE the three cases above already establish the rule
		// without needing a permission bit — this row only adds the EACCES
		// errno. If it were the only evidence, skipping would be the defect.
		if _, err := os.ReadFile(path); err == nil {
			t.Skip("chmod 000 did not make the file unreadable (running as root?) — " +
				"the malformed/EISDIR/ENOTDIR cases above still assert the rule")
		}

		_, source, err := loadSchemaFrom(path)
		if err == nil {
			t.Fatal("an unreadable schema was silently replaced by the embedded copy")
		}
		if source.Origin != path {
			t.Errorf("origin = %q, want the offending path %q", source.Origin, path)
		}
		// No bytes were read, so there is no digest to report — and "" must be
		// what says so. The digest of an empty input is a real 64-hex value, so
		// emitting one here would name a schema that was never loaded.
		if source.SHA256 != "" {
			t.Errorf("SHA256 = %q after a failed read; want empty", source.SHA256)
		}
		// The diagnostic must still name WHY the file could not be had. A
		// "could not load schema <path>" with no cause sends the reader to a
		// file that looks fine to them, because they are not the user the
		// permission bits denied.
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("diagnostic lost the cause: %q", err.Error())
		}
	})
}

// The fallback must be the same bytes the module embeds — not a second copy
// that happens to parse. Without this, schema.go's SHA pin guards a variable
// nothing in the binary reads.
func TestLoadSchemaFromUsesTheModulesEmbeddedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schemas", "open-test-intent.v1.json")

	fromFallback, _, err := loadSchemaFrom(path)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}

	dir := t.TempDir()
	fromEmbedBytes := writeSchema(t, dir, string(opentestintent.SchemaJSON()))
	fromDisk, _, err := loadSchemaFrom(fromEmbedBytes)
	if err != nil {
		t.Fatalf("embedded bytes written to disk did not load: %v", err)
	}

	// Same rules, checked through behaviour rather than by comparing compiled
	// structs: agree on a conforming document and on every shipped invalid one.
	for _, doc := range []string{canonicalDoc, `{}`, `{"entity":"O"}`, `[]`} {
		a := fromFallback.Validate(mustDecode(t, doc))
		b := fromDisk.Validate(mustDecode(t, doc))
		if strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("fallback and the module's embedded bytes disagree on %s:\n  fallback: %v\n  embedded: %v",
				doc, a, b)
		}
	}
}
