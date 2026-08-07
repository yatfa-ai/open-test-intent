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
//     installed binary in tests/parity/run_parity.sh section 16 ("Go-side
//     refusals — the excluded surfaces, still asserted").

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
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
//	validate-intent <identity> (<goversion> <goos>/<goarch>)
//
// The identity group deliberately excludes whitespace and the closing paren, so
// an empty or multi-token identity fails the match rather than parsing into
// something plausible.
var versionLineRE = regexp.MustCompile(`^validate-intent (\S+) \((go\S+) (\S+)/(\S+)\)$`)

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
	if strings.Contains(line, "\n") {
		t.Errorf("VersionLine() = %q — it must be one line, with the newline added by the caller", line)
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
// tests/parity/run_parity.sh compares it byte-for-byte rather than asserting it
// Go-side.
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
			if stdout != usage {
				t.Errorf("run(%q) printed the version line, not the usage block: %q", argv, stdout)
			}
			if stderr != "" {
				t.Errorf("run(%q) wrote to stderr: %q", argv, stderr)
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
	cases := []struct {
		argv     []string
		wantCode int
		wantErr  string
	}{
		{[]string{"-"}, 2, "stdin mode"},
		{[]string{"--json", "examples/unit-order-total.json"}, 2, "--json output for adopter"},
		// Slice 4 (SPGD-123) retired the `**` refusal — this argument is now
		// EXPANDED, and from this package's working directory it expands to
		// nothing. Kept, rather than deleted, because the assertion still has
		// teeth in the direction that matters here: a `--version` loop that
		// matched too eagerly would return 0, and a regression back to the old
		// whole-argv precheck would return 2. Only the expansion path gives 1.
		{[]string{"examples/**/*.json"}, 1, "no file(s) match 'examples/**/*.json'"},
		{[]string{"nope-does-not-exist.json"}, 1, "no file(s) match"},
	}
	for _, testCase := range cases {
		t.Run(strings.Join(testCase.argv, " "), func(t *testing.T) {
			code, _, stderr := captureRun(t, testCase.argv...)
			if code != testCase.wantCode {
				t.Errorf("run(%q) = %d, want %d", testCase.argv, code, testCase.wantCode)
			}
			if !strings.Contains(stderr, testCase.wantErr) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q",
					testCase.argv, stderr, testCase.wantErr)
			}
		})
	}
}
