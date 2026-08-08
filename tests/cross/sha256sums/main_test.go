package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Calibration for the manifest tool.
//
// WHY THIS EXISTS
//
// On a healthy release run every artifact matches its row, so verify() returns
// "no problems" forever. That is indistinguishable from a verify() that CANNOT
// return anything else — a comparison inverted, a loop scoped to a slice that is
// always empty, a re-read quietly replaced by a re-use of the digest computed
// during emit. The release would stay green through all of it while checking
// nothing, which is the defect scripts/build-release.sh's header names and the
// reason this tool exists at all.
//
// So the failures are asserted here, by handing verify() a set that has been
// deliberately broken in each of the ways a release can be broken, and requiring
// the SPECIFIC diagnostic each should produce. This mirrors what
// tests/cross/inspect-artifact/main_test.go does for the same reason.
//
// Two of these are load-bearing beyond the general principle:
//
//   - TestVerifyCatchesAnArtifactCorruptedAfterEmit is the non-vacuousness
//     proof. It corrupts a file in the window between digesting and verifying,
//     which is exactly the window scripts/build-release.sh opens.
//   - TestDigestsAreTheRealSHA256AndNotMerelySelfConsistent pins a digest this
//     tool did not choose. Every other test here compares the tool against
//     itself and would pass just as happily on a tool that hashed with the wrong
//     algorithm, consistently.

// manifestRow matches the format a consumer's own sha256sum/shasum must read:
// 64 lowercase hex characters, two spaces, then a plain basename.
var manifestRow = regexp.MustCompile(`^[0-9a-f]{64} {2}[^/\\]+$`)

// stageWith builds a directory holding the named files with the given contents,
// standing in for the release script's staging directory.
func stageWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatalf("staging %s: %v", name, err)
		}
	}
	return dir
}

// stagedArtifacts is a stand-in for one run's four cross-compiled binaries.
// The contents are irrelevant to every assertion here; only that they differ.
var stagedArtifacts = map[string]string{
	"validate-intent-linux-amd64":  "linux amd64 bytes\n",
	"validate-intent-linux-arm64":  "linux arm64 bytes\n",
	"validate-intent-darwin-amd64": "darwin amd64 bytes\n",
	"validate-intent-darwin-arm64": "darwin arm64 bytes\n",
}

// emitFor digests every file in dir into dir/SHA256SUMS, the way the release
// script does, and returns the manifest path.
func emitFor(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		files = append(files, filepath.Join(dir, e.Name()))
	}
	manifest := filepath.Join(dir, "SHA256SUMS")
	if err := emit(manifest, files); err != nil {
		t.Fatalf("emit: %v", err)
	}
	return manifest
}

// requireProblem fails unless some problem contains want.
func requireProblem(t *testing.T, problems []string, want string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no problem mentioned %q; got %#v", want, problems)
}

func TestEmitWritesTheStandardFormatWithBasenamesOnly(t *testing.T) {
	dir := stageWith(t, stagedArtifacts)
	manifest := emitFor(t, dir)

	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	text := string(raw)

	if strings.Contains(text, dir) {
		t.Errorf("the manifest leaks the staging path %q:\n%s", dir, text)
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != len(stagedArtifacts) {
		t.Fatalf("want %d rows, got %d:\n%s", len(stagedArtifacts), len(lines), text)
	}

	var names []string
	for i, line := range lines {
		if !manifestRow.MatchString(line) {
			t.Errorf("row %d is not in coreutils format: %q", i+1, line)
			continue
		}
		names = append(names, line[66:])
	}
	for _, want := range []string{
		"validate-intent-darwin-amd64",
		"validate-intent-darwin-arm64",
		"validate-intent-linux-amd64",
		"validate-intent-linux-arm64",
	} {
		if !strings.Contains(text, want+"\n") {
			t.Errorf("the manifest does not list %s:\n%s", want, text)
		}
	}

	// Sorted, so the manifest is a function of the SET and not of the order the
	// release loop happened to build in.
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("rows are not sorted by name: %q then %q", names[i-1], names[i])
		}
	}
}

func TestDigestsAreTheRealSHA256AndNotMerelySelfConsistent(t *testing.T) {
	// The canonical NIST vector. Pinned here because every other assertion in
	// this file compares the tool against itself: a tool that hashed with SHA-1,
	// or with a truncated state, would satisfy all of them and produce a
	// manifest no consumer's sha256sum could ever verify.
	const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

	dir := stageWith(t, map[string]string{"abc": "abc"})
	manifest := emitFor(t, dir)

	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	if want := abcSHA256 + "  abc\n"; string(raw) != want {
		t.Errorf("manifest is\n  %q\nwant\n  %q", string(raw), want)
	}
}

func TestAnUntouchedSetVerifies(t *testing.T) {
	dir := stageWith(t, stagedArtifacts)
	manifest := emitFor(t, dir)

	problems, notes, err := verify(manifest)
	if err != nil {
		t.Fatalf("could not verify an untouched set: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("an untouched set reported problems: %#v", problems)
	}
	if len(notes) == 0 {
		t.Error("a passing verification said nothing about what it checked")
	}
}

func TestVerifyCatchesAnArtifactCorruptedAfterEmit(t *testing.T) {
	// The non-vacuousness proof, and the reason verify() re-reads from disk
	// rather than trusting what emit() just computed. This is the exact window
	// scripts/build-release.sh opens: digest the staged set, then verify it,
	// then promote.
	dir := stageWith(t, stagedArtifacts)
	manifest := emitFor(t, dir)

	victim := filepath.Join(dir, "validate-intent-darwin-arm64")
	if err := os.WriteFile(victim, []byte("darwin arm64 bytes\x00"), 0o755); err != nil {
		t.Fatalf("corrupting the staged artifact: %v", err)
	}

	problems, notes, err := verify(manifest)
	if err != nil {
		t.Fatalf("verify errored instead of reporting a mismatch: %v", err)
	}
	if len(problems) == 0 {
		t.Fatal("a staged artifact was corrupted between digesting and verifying and the manifest check PASSED")
	}
	requireProblem(t, problems, "validate-intent-darwin-arm64 does not match SHA256SUMS")

	// The three untouched artifacts must not be swept up in it: a check that
	// failed everything on any change would be no more informative than one that
	// failed nothing.
	if len(problems) != 1 {
		t.Errorf("one artifact was corrupted but %d problems were reported: %#v", len(problems), problems)
	}

	// And nothing may CLAIM a match on a run that found one. The note is printed
	// on stdout above the failures, so a summary asserting "each re-read and
	// matching" here would be a false green line inside a failing run.
	for _, n := range notes {
		if strings.Contains(n, "matching") {
			t.Errorf("a failing verification still reported %q", n)
		}
	}
}

func TestVerifyCatchesAListedArtifactThatIsMissing(t *testing.T) {
	dir := stageWith(t, stagedArtifacts)
	manifest := emitFor(t, dir)

	if err := os.Remove(filepath.Join(dir, "validate-intent-linux-arm64")); err != nil {
		t.Fatalf("removing the staged artifact: %v", err)
	}

	problems, _, err := verify(manifest)
	if err != nil {
		// A file the manifest promised and that is not there is a finding about
		// the release, not an inability to check it. Collapsing the two would
		// report half a release as "could not examine".
		t.Fatalf("a missing listed file was reported as unexaminable: %v", err)
	}
	requireProblem(t, problems, "validate-intent-linux-arm64 is listed in SHA256SUMS but is not there")
}

func TestVerifyCatchesAnArtifactThatNoRowDescribes(t *testing.T) {
	dir := stageWith(t, stagedArtifacts)
	manifest := emitFor(t, dir)

	// An artifact that appeared after the manifest was written — a stray build
	// output, or a target dropped from TARGETS whose file survived. `sha256sum
	// -c` would report a clean run over it.
	if err := os.WriteFile(filepath.Join(dir, "validate-intent-windows-amd64"), []byte("?"), 0o755); err != nil {
		t.Fatalf("staging the stray artifact: %v", err)
	}

	problems, _, err := verify(manifest)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	requireProblem(t, problems, "described by no row in SHA256SUMS: validate-intent-windows-amd64")
}

func TestVerifyRefusesAManifestItCannotParse(t *testing.T) {
	for name, body := range map[string]string{
		"single space":    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad abc\n",
		"short digest":    "ba7816bf  abc\n",
		"not hex":         "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz  abc\n",
		"escaped name":    "\\ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  a\\nb\n",
		"path in the row": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  /tmp/stage/abc\n",
		"empty":           "",
		"listed twice":    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  abc\nba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  abc\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := stageWith(t, map[string]string{"abc": "abc"})
			manifest := filepath.Join(dir, "SHA256SUMS")
			if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
				t.Fatalf("writing the manifest: %v", err)
			}
			// Refused as unexaminable rather than silently skipped: a row this
			// tool cannot parse is a file it did not check, and a run that
			// ignored it would report a green verification of fewer artifacts
			// than it claimed.
			if _, _, err := verify(manifest); err == nil {
				t.Error("a manifest that cannot be parsed was accepted")
			}
		})
	}
}

func TestEmitRefusesWhatItCannotDescribeHonestly(t *testing.T) {
	t.Run("two operands with the same basename", func(t *testing.T) {
		a := stageWith(t, map[string]string{"validate-intent-linux-amd64": "a"})
		b := stageWith(t, map[string]string{"validate-intent-linux-amd64": "b"})
		err := emit(filepath.Join(t.TempDir(), "SHA256SUMS"), []string{
			filepath.Join(a, "validate-intent-linux-amd64"),
			filepath.Join(b, "validate-intent-linux-amd64"),
		})
		if err == nil {
			t.Error("emit accepted two operands a manifest of basenames cannot distinguish")
		}
	})

	t.Run("a file that is not there", func(t *testing.T) {
		dir := t.TempDir()
		if err := emit(filepath.Join(dir, "SHA256SUMS"), []string{filepath.Join(dir, "absent")}); err == nil {
			t.Error("emit produced a manifest for a file it could not read")
		}
	})

	t.Run("the manifest as its own entry", func(t *testing.T) {
		dir := stageWith(t, map[string]string{"SHA256SUMS": "old"})
		if err := emit(filepath.Join(dir, "SHA256SUMS"), []string{filepath.Join(dir, "SHA256SUMS")}); err == nil {
			t.Error("emit listed the manifest inside itself, which can never verify")
		}
	})
}

// TestThePlatformToolReadsWhatWeWrite is the point of emitting coreutils format
// rather than something of our own: the adopter who downloads an artifact
// verifies it with a tool that has never heard of this program. Skipped, loudly,
// on a host with neither — the skip is the honest answer, since this container
// has both but a stock macOS host has only shasum.
func TestThePlatformToolReadsWhatWeWrite(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args []string
	}{
		{"sha256sum", []string{"-c", "SHA256SUMS"}},
		{"shasum", []string{"-a", "256", "-c", "SHA256SUMS"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			if _, err := exec.LookPath(tc.tool); err != nil {
				t.Skipf("no %s on this host, so the format was not checked against it", tc.tool)
			}
			dir := stageWith(t, stagedArtifacts)
			emitFor(t, dir)

			cmd := exec.Command(tc.tool, tc.args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s could not verify our manifest: %v\n%s", tc.tool, err, out)
			}

			// And it must disagree when it should, or the check above proves
			// only that the tool ran.
			if err := os.WriteFile(filepath.Join(dir, "validate-intent-linux-amd64"), []byte("tampered"), 0o755); err != nil {
				t.Fatalf("corrupting the artifact: %v", err)
			}
			cmd = exec.Command(tc.tool, tc.args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("%s passed a tampered artifact:\n%s", tc.tool, out)
			}
		})
	}
}
