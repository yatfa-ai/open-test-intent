package main

// Unit coverage for the version identity (version.go, SPGD-141).
//
// What this file can and cannot prove, stated up front because the distinction
// is the whole design of resolveVersion:
//
//   * The TIER SELECTION is proven here, exhaustively, because resolveVersion
//     is a pure function of its three inputs. A `go test` binary is linked
//     exactly one way, so testing the tiers through the real Version var and
//     the real debug.ReadBuildInfo would test one tier and assume two.
//   * The WIRING — that Version really is the symbol -ldflags -X writes to, and
//     that the linker really applied it — cannot be proven by any test in this
//     package, for the same reason. scripts/build-release.sh proves it, by
//     running the artifact it just built and comparing the reported identity to
//     the requested one. A `-X` naming a symbol that does not exist is silently
//     ignored by the linker, so that check is load-bearing, not ceremony.
//   * The END-TO-END surface (exit 0, one line on stdout, nothing on stderr,
//     any argv position) is proven here through run(), and again from a real
//     installed binary in tests/parity/run_parity.sh section 16b
//     ("--version — the excluded surface that SUCCEEDS").

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// --------------------------------------------------------------------------- //
// tier selection
// --------------------------------------------------------------------------- //

func TestResolveVersionTiers(t *testing.T) {
	const revision = "e545d1b692136f36fa11bc62a4c54c57f858047a"

	cases := []struct {
		name     string
		injected string
		revision string
		modified bool
		want     string
	}{
		// Tier 1: a release build. The stamp wins even when a revision is
		// also embedded — which is the normal case, since a release is built
		// from a checkout.
		{"stamped", "1.4.0", revision, false, "1.4.0"},
		{"stamped, no revision", "1.4.0", "", false, "1.4.0"},
		{"stamped from a dirty tree", "1.4.0", revision, true, "1.4.0" + dirtySuffix},

		// Tier 2: a plain `go build` inside a checkout — what
		// tests/parity/run_parity.sh does, so this path is exercised on every
		// harness run rather than only here.
		{"revision only", "", revision, false, revision},
		{"revision only, dirty", "", revision, true, revision + dirtySuffix},

		// Tier 3: -buildvcs=false, an extracted tarball, a container with no
		// git. Never empty, and never a bare dirty marker.
		{"nothing at all", "", "", false, versionUnknown},
		{"nothing at all, modified flag set", "", "", true, versionUnknown},

		// A stamp of only whitespace is a stamping accident (an empty shell
		// variable interpolated into -X), not a version. It must not win the
		// tier it did not earn.
		{"whitespace stamp falls through", "   ", revision, false, revision},
		{"whitespace stamp with no revision", "  \t ", "", false, versionUnknown},
		{"whitespace revision falls through", "", "  ", false, versionUnknown},

		// Surrounding whitespace on a real stamp is trimmed rather than
		// emitted, so the version line cannot gain a stray space or newline.
		{"stamp is trimmed", "  1.4.0\n", "", false, "1.4.0"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := resolveVersion(testCase.injected, testCase.revision, testCase.modified)
			if got != testCase.want {
				t.Errorf("resolveVersion(%q, %q, %v) = %q, want %q",
					testCase.injected, testCase.revision, testCase.modified, got, testCase.want)
			}
		})
	}
}

// The single invariant the whole file exists to protect, asserted directly and
// separately from the table above: no combination of inputs produces an empty
// or whitespace-only identity.
//
// An empty version token is this project's house defect in version form — the
// line still prints, the exit code is still 0, and every consumer that checks
// only those two reads "fine" from a binary that just told it nothing.
func TestResolveVersionIsNeverEmpty(t *testing.T) {
	blanks := []string{"", " ", "\t", "\n", "  \n\t "}
	for _, injected := range blanks {
		for _, revision := range blanks {
			for _, modified := range []bool{false, true} {
				got := resolveVersion(injected, revision, modified)
				if strings.TrimSpace(got) == "" {
					t.Fatalf("resolveVersion(%q, %q, %v) = %q — an identity must never be blank",
						injected, revision, modified, got)
				}
				if got == dirtySuffix || strings.HasPrefix(got, dirtySuffix) {
					t.Fatalf("resolveVersion(%q, %q, %v) = %q — a bare dirty marker is not an identity",
						injected, revision, modified, got)
				}
			}
		}
	}
}

// --------------------------------------------------------------------------- //
// the line itself
// --------------------------------------------------------------------------- //

// versionLineRE is the shape a consumer can rely on:
//
//	validate-intent <identity> (<goversion> <goos>/<goarch>) schema sha256:<hex>
//
// The identity group deliberately excludes whitespace and the closing paren, so
// an empty or multi-token identity fails the match rather than parsing into
// something plausible.
//
// The digest group is `[0-9a-f]{64}` rather than `\S+` for the same reason: a
// truncated, uppercased or templated digest is a wrong answer to the one
// question this token exists to answer, and it must fail the match rather than
// be captured and then compared as an equally-wrong string.
var versionLineRE = regexp.MustCompile(`^validate-intent (\S+) \((go\S+) (\S+)/(\S+)\) schema sha256:([0-9a-f]{64})$`)

func TestVersionLineShape(t *testing.T) {
	line := VersionLine()

	match := versionLineRE.FindStringSubmatch(line)
	if match == nil {
		t.Fatalf("VersionLine() = %q, which does not match %s", line, versionLineRE)
	}
	if match[1] != VersionString() {
		t.Errorf("VersionLine() reports identity %q, VersionString() says %q", match[1], VersionString())
	}
	if match[2] != runtime.Version() {
		t.Errorf("VersionLine() reports toolchain %q, want %q", match[2], runtime.Version())
	}
	if match[3] != runtime.GOOS || match[4] != runtime.GOARCH {
		t.Errorf("VersionLine() reports %s/%s, want %s/%s",
			match[3], match[4], runtime.GOOS, runtime.GOARCH)
	}
	// The point of the token: it must be the digest of the schema this binary
	// carries, not merely 64 hex characters. schema_test.go holds
	// SchemaSHA256 itself to the embedded bytes and to the canonical pin, so
	// this one comparison inherits both.
	if match[5] != opentestintent.SchemaSHA256() {
		t.Errorf("VersionLine() reports schema digest %q, the embedded schema digests to %q",
			match[5], opentestintent.SchemaSHA256())
	}
	if strings.Contains(line, "\n") {
		t.Errorf("VersionLine() = %q — it must be one line, with the newline added by the caller", line)
	}
}

// The budget the line has to fit in, and it belongs to a different repository.
//
// specguard-rspec's identity probe (lib/specguard/rspec/validator_backend.rb)
// shells out to `--version` to name which implementation produced a run's
// verdicts, and DISCARDS the answer if it is not one renderable line under
// IDENTITY_MAX_BYTES = 200. Discards it silently — the run still succeeds, with
// the identity simply absent — so a line that outgrew the budget would not fail
// anything here, or there: it would quietly stop being reported, which is the
// vacuous shape this project keeps naming.
//
// The worst case is derived from the real line rather than re-stating the format
// string, so this cannot pass while VersionLine's format drifts underneath it.
// Fixed overhead is everything except the identity token; the longest identity
// resolveVersion can produce from a plain build is a 40-hex VCS revision from a
// modified tree.
func TestVersionLineFitsTheGemsIdentityBudget(t *testing.T) {
	// validator_backend.rb's IDENTITY_MAX_BYTES. Changing it there without
	// changing it here is caught by nothing; the constant is repeated with its
	// source named so the two can at least be found together.
	const identityMaxBytes = 200

	line := VersionLine()
	if len(line) > identityMaxBytes {
		t.Errorf("VersionLine() is %d bytes, over specguard-rspec's %d-byte identity budget:\n  %s",
			len(line), identityMaxBytes, line)
	}

	overhead := len(line) - len(VersionString())
	worst := overhead + 40 + len(dirtySuffix)
	if worst > identityMaxBytes {
		t.Errorf("a plain build from a modified tree would report %d bytes, over specguard-rspec's %d-byte "+
			"identity budget — its probe would drop the identity rather than fail, so nothing else would say so",
			worst, identityMaxBytes)
	}
}

// The identity this particular test binary reports, whatever tier it came from,
// must still be a real answer. `go test` links without -ldflags, so in a
// checkout this is the vcs.revision path and in a tarball it is the literal —
// either way it must not be blank, and it must not look like an unexpanded
// build template.
func TestVersionStringIsUsable(t *testing.T) {
	got := VersionString()
	if strings.TrimSpace(got) == "" {
		t.Fatal("VersionString() is blank")
	}
	for _, placeholder := range []string{"$", "{", "}", "%!", "<", ">"} {
		if strings.Contains(got, placeholder) {
			t.Errorf("VersionString() = %q — contains %q, which reads as an unexpanded template",
				got, placeholder)
		}
	}
	if strings.ContainsAny(got, " \t\n") {
		t.Errorf("VersionString() = %q — an identity must be a single token", got)
	}
}

// --------------------------------------------------------------------------- //
// the CLI surface
// --------------------------------------------------------------------------- //

// captureRun runs the CLI with the given argv and returns its exit code and
// whatever it wrote to stdout and stderr.
//
// run() writes to os.Stdout/os.Stderr directly (fmt.Println, os.Stdout.Write),
// because the reference does and byte-for-byte parity is the acceptance test —
// so capturing means replacing the file descriptors, not injecting a writer.
// Those are process globals: no test using this may call t.Parallel().
func captureRun(t *testing.T, argv ...string) (code int, stdout, stderr string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	// Drain both pipes concurrently: a run that writes more than the pipe
	// buffer would otherwise block forever, and a test that hangs is a worse
	// failure than one that fails.
	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { var buf bytes.Buffer; io.Copy(&buf, outR); outCh <- buf.String() }()
	go func() { var buf bytes.Buffer; io.Copy(&buf, errR); errCh <- buf.String() }()

	func() {
		defer func() {
			os.Stdout, os.Stderr = savedOut, savedErr
			outW.Close()
			errW.Close()
		}()
		code = run(argv)
	}()

	return code, <-outCh, <-errCh
}

// Success criteria 1 and 3: exit 0, a version line on stdout, nothing on
// stderr — from any argv position.
//
// Position independence is not cosmetic here. `--json` is stripped anywhere on
// the line, so a user who has learned that convention will write
// `validate-intent examples/*.json --version`; if that fell through to adopter
// mode it would report "no file(s) match '--version'" and exit 1, which is the
// exact false-red this slice exists to remove.
func TestVersionFlagFromAnyPosition(t *testing.T) {
	cases := [][]string{
		{"--version"},
		{"--version", "examples/unit-order-total.json"},
		{"examples/unit-order-total.json", "--version"},
		{"--source", "examples/sources/order_spec.rb", "--version"},
		{"--version", "--json"},
		{"--json", "--version"},
		{"--version", "-"},                      // ahead of the stdin refusal
		{"--version", "examples/**/*.json"},     // ahead of a recursive-glob expansion
		{"nope.json", "--version", "also.json"}, // between two non-existent files
	}

	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, argv...)
			if code != 0 {
				t.Errorf("run(%q) = %d, want 0 (stderr: %q)", argv, code, stderr)
			}
			if stderr != "" {
				t.Errorf("run(%q) wrote to stderr: %q — --version is not a diagnostic", argv, stderr)
			}
			want := VersionLine() + "\n"
			if stdout != want {
				t.Errorf("run(%q) stdout = %q, want %q", argv, stdout, want)
			}
		})
	}
}

// --help wins over --version in either order, exactly as it wins over
// everything else. The reference agrees on this crossing (it has no --version,
// so it reads it as a filename its own --help loop pre-empts), which is why
// tests/parity/run_parity.sh compares it against the oracle rather than
// asserting it Go-side.
//
// What --help prints is the shared usage block plus helpTrailer — the trailer
// documents the flag, it does not answer it — so the expectation here is the
// concatenation, and a port that let --version win fails on it.
func TestHelpWinsOverVersion(t *testing.T) {
	for _, argv := range [][]string{
		{"--help", "--version"},
		{"--version", "--help"},
		{"--version", "-h"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, argv...)
			if code != 0 {
				t.Errorf("run(%q) = %d, want 0", argv, code)
			}
			if stdout != usage+helpTrailer {
				t.Errorf("run(%q) printed the version line, not the usage block: %q", argv, stdout)
			}
			if stderr != "" {
				t.Errorf("run(%q) wrote to stderr: %q", argv, stderr)
			}
		})
	}
}

// helpTrailer must never leave the --help path, and `usage` must never absorb
// it.
//
// This is the assertion that keeps the Go-only row from costing parity
// coverage. `usage` is printed on refusals as well as on --help, and three of
// those refusals are compared byte-for-byte against the reference in
// tests/parity/run_parity.sh — `--source` with no FILE, `--json` in self-test
// mode, and the two crossed. A --version row inside the shared constant would
// break all of them, and no matched edit to bin/validate-intent could restore
// them: the reference has no such flag, and documenting one that answers "no
// file(s) match" would be a worse falsehood than the misleading line SPGD-279
// removed.
//
// The harness would catch that too, but only when a Go toolchain, python3 and
// the whole corpus are present. This catches it in `go test`, one package wide,
// which is where the mistake would actually be made.
func TestTheHelpTrailerNeverLeavesTheHelpPath(t *testing.T) {
	if strings.Contains(usage, "--version") {
		t.Error("the shared usage block names --version; it is compared byte-for-byte " +
			"against a reference that has no such flag")
	}
	if !strings.Contains(helpTrailer, "--version") {
		t.Error("helpTrailer does not name --version — it exists to document exactly that")
	}

	for _, argv := range [][]string{
		{"--source"},           // --source with no FILE argument
		{"-s"},                 // the alias
		{"--json"},             // --json in self-test mode
		{"--source", "--json"}, // both, still the usage error
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, argv...)
			if code != 2 {
				t.Fatalf("run(%q) = %d, want 2 (a usage error)", argv, code)
			}
			if stdout != "" {
				t.Errorf("run(%q) wrote to stdout: %q — a refusal must not", argv, stdout)
			}
			if !strings.Contains(stderr, usage) {
				t.Errorf("run(%q) did not print the shared usage block on stderr: %q", argv, stderr)
			}
			if strings.Contains(stderr, helpTrailer) {
				t.Errorf("run(%q) printed the Go-only trailer on a refusal path; "+
					"it breaks the byte-for-byte comparison of this refusal", argv)
			}
		})
	}
}

// The control for the test above: a `--version`-shaped argument that is NOT
// `--version` must still reach adopter mode and be reported as a filename that
// matched nothing. Without this, a loop that matched too eagerly — on a prefix,
// or on any argument containing "version" — would pass every assertion above
// while swallowing real arguments.
func TestOnlyTheExactFlagIsVersion(t *testing.T) {
	for _, arg := range []string{"--versions", "-version", "--version=1", "version", "--VERSION"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, _ := captureRun(t, arg)
			if code == 0 {
				t.Errorf("run([%q]) = 0 — it was treated as --version", arg)
			}
			if strings.Contains(stdout, programName+" ") {
				t.Errorf("run([%q]) printed a version line: %q", arg, stdout)
			}
		})
	}
}

// Success criterion 6, at the unit level: adding a flag to the top of run()
// must not disturb any existing invocation. The harness proves this properly,
// over the whole corpus; this catches the gross form early, in the package
// where the regression would be introduced.
func TestExistingSurfacesAreUndisturbed(t *testing.T) {
	// THREE of these rows are retired refusals, and each is KEPT rather than
	// deleted for the reason the `**` row already states: the assertion still
	// has teeth in the direction this file cares about. A `--version` loop that
	// matched too eagerly returns 0; a regression to a whole-argv precheck
	// returns 2. Only the implemented path returns 1.
	//
	// The expectation moves to the stream that now carries the message. Slices 3
	// and 4 turned these refusals — which wrote to stderr — into working modes
	// that report on STDOUT, so a row still asserting on stderr would be
	// comparing against the empty string and passing vacuously.
	cases := []struct {
		argv     []string
		wantCode int
		wantOut  string // in stdout when non-empty...
		wantErr  string // ...in stderr when non-empty
	}{
		// Slice 3 (SPGD-107) implemented stdin. `go test` gives this process an
		// empty stdin, so the document is unparseable and the mode reports that
		// on stdout rather than refusing on stderr.
		{[]string{"-"}, 1, "could not read/parse JSON", ""},
		// Slice 3 also implemented adopter `--json`. From this package's working
		// directory the path matches nothing, which the JSON renderer reports as
		// a `no-match` FINDING on stdout — the whole point of that renderer, and
		// the reason it writes nothing to stderr.
		{[]string{"--json", "examples/unit-order-total.json"}, 1, `"kind": "no-match"`, ""},
		// Slice 4 (SPGD-123) retired the `**` refusal — this argument is now
		// EXPANDED, and from this package's working directory it expands to
		// nothing.
		{[]string{"examples/**/*.json"}, 1, "", "no file(s) match 'examples/**/*.json'"},
		{[]string{"nope-does-not-exist.json"}, 1, "", "no file(s) match"},
	}
	for _, testCase := range cases {
		t.Run(strings.Join(testCase.argv, " "), func(t *testing.T) {
			code, stdout, stderr := captureRun(t, testCase.argv...)
			if code != testCase.wantCode {
				t.Errorf("run(%q) = %d, want %d", testCase.argv, code, testCase.wantCode)
			}
			// Exactly one stream is asserted per row, and the table is checked
			// for that rather than trusted: a row with neither expectation set
			// would assert only the exit code while looking like it asserted
			// prose.
			if (testCase.wantOut == "") == (testCase.wantErr == "") {
				t.Fatalf("run(%q): the table must name exactly one stream to assert",
					testCase.argv)
			}
			if testCase.wantOut != "" && !strings.Contains(stdout, testCase.wantOut) {
				t.Errorf("run(%q) stdout = %q, want it to contain %q",
					testCase.argv, stdout, testCase.wantOut)
			}
			if testCase.wantErr != "" && !strings.Contains(stderr, testCase.wantErr) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q",
					testCase.argv, stderr, testCase.wantErr)
			}
		})
	}
}
