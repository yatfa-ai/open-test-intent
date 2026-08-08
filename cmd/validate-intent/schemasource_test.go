package main

// Unit coverage for `--schema-source` (schemasource.go, SPGD-301): the flag that
// reports the schema a run ENFORCES rather than the one the binary CARRIES.
//
// What this file can and cannot prove:
//
//   - That the digest follows the BYTES rather than the build is proven here
//     without a skip, against loadSchemaFrom directly, because that function
//     takes the path as an argument. This is the claim the whole slice exists
//     for, so it must not rest on a case that can quietly not run.
//   - The end-to-end surface through run() is proven here for the EMBEDDED case
//     (a `go test` binary lives in a temp build directory with no schemas/ beside
//     it) and, where the filesystem permits, for the override and failure cases
//     too — those plant a file at the executable-relative path and SKIP if they
//     cannot, which is honest only because the non-skippable assertions above
//     already pin the rule.
//   - That the digest matches what an independent SHA-256 implementation says
//     is NOT proven here: the expectation below is computed with the same
//     algorithm and encoding the code uses, so it pins the choice rather than
//     re-deriving it. tests/parity/run_parity.sh section 16 does the independent
//     half, comparing against sha256sum/shasum over a real file.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// wantDigest is the expectation, written out rather than taken from the code
// under test. It pins the ALGORITHM and the ENCODING — SHA-256, hex, lowercase —
// which is what a consumer comparing this digest against another tool's output
// depends on.
func wantDigest(t *testing.T, data string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// --------------------------------------------------------------------------- //
// the digest follows the bytes
// --------------------------------------------------------------------------- //

// THE point of the slice, and the one assertion that must never be skippable:
// when a schema file wins, the reported digest is of THAT file's bytes, and it
// differs from the digest the binary carries. An implementation that reported the
// embedded digest with the on-disk path beside it would satisfy every "is it 64
// hex characters" check ever written and be a plausible lie.
func TestLoadSchemaFromDigestsTheBytesItActuallyLoaded(t *testing.T) {
	path := writeSchema(t, t.TempDir(), permissiveSchema)

	_, source, err := loadSchemaFrom(path)
	if err != nil {
		t.Fatalf("loading the override: %v", err)
	}
	if source.Origin != path {
		t.Errorf("origin = %q, want the on-disk path %q", source.Origin, path)
	}
	if got, want := source.SHA256, wantDigest(t, permissiveSchema); got != want {
		t.Errorf("SHA256 = %q, want the digest of the file's bytes %q", got, want)
	}
	if source.SHA256 == opentestintent.SchemaSHA256() {
		t.Errorf("SHA256 = %q, the digest of the COMPILED-IN schema — the report is "+
			"following the build rather than the bytes it loaded", source.SHA256)
	}
}

// The other side of the same claim, and success criterion 2: with no file to
// win, the enforced digest IS the carried one. Both directions are needed —
// "always differs" and "always agrees" are each satisfiable by a broken
// implementation, and only the pair pins the behaviour.
func TestLoadSchemaFromReportsTheCarriedDigestWhenItFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schemas", "open-test-intent.v1.json")

	_, source, err := loadSchemaFrom(path)
	if err != nil {
		t.Fatalf("expected the embedded fallback, got error: %v", err)
	}
	if source.Origin != EmbeddedSchemaLabel {
		t.Errorf("origin = %q, want %q", source.Origin, EmbeddedSchemaLabel)
	}
	if source.SHA256 != opentestintent.SchemaSHA256() {
		t.Errorf("SHA256 = %q, want the carried digest %q — a fallback that reported "+
			"anything else would make the two flags incomparable",
			source.SHA256, opentestintent.SchemaSHA256())
	}
	// And the fallback's digest is the digest of the bytes it substituted, not a
	// constant that happens to agree with SchemaSHA256 for a different reason.
	if got, want := source.SHA256, wantDigest(t, string(opentestintent.SchemaJSON())); got != want {
		t.Errorf("SHA256 = %q, want the digest of the embedded bytes %q", got, want)
	}
}

// --------------------------------------------------------------------------- //
// the line
// --------------------------------------------------------------------------- //

// schemaSourceLineRE is the shape a consumer can rely on:
//
//	schema <origin> sha256:<hex>
//
// The origin group is greedy and permissive because a path may contain spaces;
// the digest is anchored, lowercase and exactly 64 hex, so a truncated or
// templated value fails the match rather than being captured and compared as an
// equally-wrong string.
var schemaSourceLineRE = regexp.MustCompile(`^schema (.+) sha256:([0-9a-f]{64})$`)

func TestSchemaSourceLineShape(t *testing.T) {
	cases := []SchemaSource{
		{Origin: "/usr/local/schemas/open-test-intent.v1.json", SHA256: opentestintent.SchemaSHA256()},
		{Origin: EmbeddedSchemaLabel, SHA256: opentestintent.SchemaSHA256()},
		{Origin: "/a path/with spaces/schemas/open-test-intent.v1.json", SHA256: opentestintent.SchemaSHA256()},
	}
	for _, source := range cases {
		t.Run(source.Origin, func(t *testing.T) {
			line := SchemaSourceLine(source)

			match := schemaSourceLineRE.FindStringSubmatch(line)
			if match == nil {
				t.Fatalf("SchemaSourceLine(%+v) = %q, which does not match %s",
					source, line, schemaSourceLineRE)
			}
			if match[1] != source.Origin {
				t.Errorf("line reports origin %q, want %q", match[1], source.Origin)
			}
			if match[2] != source.SHA256 {
				t.Errorf("line reports digest %q, want %q", match[2], source.SHA256)
			}
			if strings.Contains(line, "\n") {
				t.Errorf("SchemaSourceLine = %q — it must be one line, with the newline "+
					"added by the caller", line)
			}
			// The shell extraction the flag is meant to serve, asserted rather
			// than assumed: everything after the last space is the digest token,
			// whatever the origin holds.
			if tail := line[strings.LastIndex(line, " ")+1:]; tail != "sha256:"+source.SHA256 {
				t.Errorf("${line##* } = %q, want %q — a consumer cannot get the digest "+
					"out with one substitution any more", tail, "sha256:"+source.SHA256)
			}
		})
	}
}

// --------------------------------------------------------------------------- //
// the CLI surface
// --------------------------------------------------------------------------- //

// Exit 0, one line on stdout, nothing on stderr — from any argv position, for
// the same reason --version has it: a user who has learned that --json and
// --version go anywhere will write `validate-intent FILE --schema-source`, and a
// flag that only worked in first position would answer that by validating FILE
// or by reporting it as a filename that matched nothing.
//
// The expectation is LoadSchema's own answer, which is the claim: the flag
// reports what a verdict run would resolve, not something it derived itself. What
// that answer must BE is pinned by the two tests at the top of this file.
func TestSchemaSourceFlagFromAnyPosition(t *testing.T) {
	_, source, err := LoadSchema()
	if err != nil {
		t.Fatalf("LoadSchema in this test environment: %v", err)
	}
	want := SchemaSourceLine(source) + "\n"

	cases := [][]string{
		{schemaSourceFlag},
		{schemaSourceFlag, "examples/unit-order-total.json"},
		{"examples/unit-order-total.json", schemaSourceFlag},
		{"--source", "examples/sources/order_spec.rb", schemaSourceFlag},
		{schemaSourceFlag, "--json"},
		{"--json", schemaSourceFlag},
		{schemaSourceFlag, "-"},                  // ahead of the stdin refusal
		{schemaSourceFlag, "examples/**/*.json"}, // ahead of a recursive-glob expansion
		{"nope.json", schemaSourceFlag, "also.json"},
	}

	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, argv...)
			if code != 0 {
				t.Errorf("run(%q) = %d, want 0 (stderr: %q)", argv, code, stderr)
			}
			if stderr != "" {
				t.Errorf("run(%q) wrote to stderr: %q — this is a report, not a diagnostic",
					argv, stderr)
			}
			if stdout != want {
				t.Errorf("run(%q) stdout = %q, want %q", argv, stdout, want)
			}
		})
	}
}

// --help wins over --schema-source in either order, exactly as it wins over
// everything else. tests/parity/run_parity.sh compares this crossing against the
// oracle — the reference's own --help loop pre-empts the flag before anything
// reads it as a filename — so this is the early, package-local form of a claim
// that is settled against Python.
func TestHelpWinsOverSchemaSource(t *testing.T) {
	for _, argv := range [][]string{
		{"--help", schemaSourceFlag},
		{schemaSourceFlag, "--help"},
		{schemaSourceFlag, "-h"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, argv...)
			if code != 0 {
				t.Errorf("run(%q) = %d, want 0", argv, code)
			}
			if stdout != usage+helpTrailer {
				t.Errorf("run(%q) did not print the usage block: %q", argv, stdout)
			}
			if stderr != "" {
				t.Errorf("run(%q) wrote to stderr: %q", argv, stderr)
			}
		})
	}
}

// --version wins over --schema-source, in either order.
//
// This is not a preference, it is a compatibility constraint: scripts/install.sh,
// scripts/build-release.sh and specguard-rspec's identity probe all parse
// `--version`'s output, and the flag added by this slice must not change what any
// crossing of the two prints. Asserted rather than left to the order two loops
// happen to sit in.
func TestVersionWinsOverSchemaSource(t *testing.T) {
	for _, argv := range [][]string{
		{"--version", schemaSourceFlag},
		{schemaSourceFlag, "--version"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, argv...)
			if code != 0 {
				t.Errorf("run(%q) = %d, want 0", argv, code)
			}
			if stdout != VersionLine()+"\n" {
				t.Errorf("run(%q) stdout = %q, want the version line %q",
					argv, stdout, VersionLine()+"\n")
			}
			if stderr != "" {
				t.Errorf("run(%q) wrote to stderr: %q", argv, stderr)
			}
		})
	}
}

// The control: a `--schema-source`-shaped argument that is NOT the flag must
// still reach adopter mode and be reported as a filename that matched nothing.
// Without this, a loop matching on a prefix — or on any argument containing
// "schema" — would pass every assertion above while swallowing real arguments.
func TestOnlyTheExactFlagIsSchemaSource(t *testing.T) {
	for _, arg := range []string{
		"--schema-sources", "-schema-source", "--schema-source=x",
		"schema-source", "--SCHEMA-SOURCE", "--schema",
	} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, _ := captureRun(t, arg)
			if code == 0 {
				t.Errorf("run([%q]) = 0 — it was treated as %s", arg, schemaSourceFlag)
			}
			if strings.HasPrefix(stdout, "schema ") {
				t.Errorf("run([%q]) printed a schema-source line: %q", arg, stdout)
			}
		})
	}
}

// The flag, the documentation and the matcher must name the same string. The
// trailer is the ONLY documentation on the host this binary ships to, so a flag
// renamed in one place and not the other is a binary whose --help describes a
// program that does not exist — the defect SPGD-279 removed from the last line
// of `usage`.
func TestTheHelpTrailerDocumentsSchemaSource(t *testing.T) {
	if !strings.Contains(helpTrailer, schemaSourceFlag) {
		t.Errorf("helpTrailer does not name %s — it is the only on-host documentation "+
			"a released artifact has", schemaSourceFlag)
	}
	if strings.Contains(usage, schemaSourceFlag) {
		t.Errorf("the shared usage block names %s; it is compared byte-for-byte against "+
			"a reference that has no such flag, and is printed on refusals that are "+
			"compared too", schemaSourceFlag)
	}
}

// --------------------------------------------------------------------------- //
// end to end, through the executable-relative path
// --------------------------------------------------------------------------- //

// plantSchemaBesideTheExecutable writes content to the path LoadSchema resolves
// from os.Executable(), or SKIPs when it cannot — the same arrangement, and the
// same honesty about it, as TestLoadSchemaPrefersASchemaBesideTheExecutable in
// fileio_schema_test.go. Skipping is acceptable here ONLY because the two
// assertions at the top of this file pin the same rule without touching the
// filesystem layout.
func plantSchemaBesideTheExecutable(t *testing.T, content string) string {
	t.Helper()
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
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Skipf("cannot write %s: %v", path, err)
	}
	t.Cleanup(func() { os.Remove(path) })
	return path
}

// Success criterion 1, end to end: with a schema planted beside the executable
// that differs from the compiled-in copy, the flag names that path and reports a
// digest DIFFERENT from the one --version reports. Carried versus enforced,
// observable from the command line, which is the whole slice in one assertion.
func TestSchemaSourceReportsTheEnforcedSchemaNotTheCarriedOne(t *testing.T) {
	path := plantSchemaBesideTheExecutable(t, permissiveSchema)

	code, stdout, stderr := captureRun(t, schemaSourceFlag)
	if code != 0 {
		t.Fatalf("run(%s) = %d, want 0 (stderr: %q)", schemaSourceFlag, code, stderr)
	}

	match := schemaSourceLineRE.FindStringSubmatch(strings.TrimSuffix(stdout, "\n"))
	if match == nil {
		t.Fatalf("stdout = %q, which does not match %s", stdout, schemaSourceLineRE)
	}
	if match[1] != path {
		t.Errorf("origin = %q, want the planted schema %q", match[1], path)
	}
	if want := wantDigest(t, permissiveSchema); match[2] != want {
		t.Errorf("digest = %q, want the planted schema's %q", match[2], want)
	}

	// The comparison the flag exists to make possible. --version is unchanged
	// and still reports the carried digest; the two must now disagree, and a
	// consumer can see that they do.
	carried := versionLineRE.FindStringSubmatch(VersionLine())
	if carried == nil {
		t.Fatalf("VersionLine() = %q does not match %s", VersionLine(), versionLineRE)
	}
	if match[2] == carried[5] {
		t.Errorf("--schema-source and --version report the same digest %q while an "+
			"overriding schema is winning; carried-versus-enforced is still invisible",
			match[2])
	}
}

// Success criterion 3: a schema that EXISTS and cannot be loaded is not papered
// over with the embedded copy, and does not become a clean report. It exits 2
// with the diagnostic byte-for-byte identical to the one a verdict run emits on
// the same tree — asserted by RUNNING both rather than by observing that they
// call the same function.
func TestSchemaSourceFailsExactlyAsTheVerdictPathDoes(t *testing.T) {
	plantSchemaBesideTheExecutable(t, "{ this is not json")

	code, stdout, stderr := captureRun(t, schemaSourceFlag)
	if code != 2 {
		t.Fatalf("run(%s) = %d, want 2 (stdout: %q)", schemaSourceFlag, code, stdout)
	}
	if stdout != "" {
		t.Errorf("run(%s) wrote to stdout: %q — a run that loaded no schema has no "+
			"schema to report", schemaSourceFlag, stdout)
	}
	if !strings.HasPrefix(stderr, "error: could not load schema ") {
		t.Errorf("run(%s) stderr = %q, want the reference's could-not-load diagnostic",
			schemaSourceFlag, stderr)
	}

	verdictCode, _, verdictStderr := captureRun(t, "examples/unit-order-total.json")
	if verdictCode != code {
		t.Errorf("%s exited %d, the verdict path exited %d on the same tree",
			schemaSourceFlag, code, verdictCode)
	}
	if stderr != verdictStderr {
		t.Errorf("the two paths disagree about this host's broken schema:\n  %s: %q\n  verdict: %q",
			schemaSourceFlag, stderr, verdictStderr)
	}
}
