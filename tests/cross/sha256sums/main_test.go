package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yatfa-ai/open-test-intent/tests/cross/internal/crosstest"
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
// Three of these are load-bearing beyond the general principle:
//
//   - TestVerifyCatchesAnArtifactCorruptedAfterEmit is the non-vacuousness
//     proof. It corrupts a file in the window between digesting and verifying,
//     which is exactly the window scripts/build-release.sh opens.
//   - TestDigestsAreTheRealSHA256AndNotMerelySelfConsistent pins a digest this
//     tool did not choose. Every other test here compares the tool against
//     itself and would pass just as happily on a tool that hashed with the wrong
//     algorithm, consistently.
//   - TestTheCLIExitCodeIsTheContractTheReleaseScriptReads runs the COMPILED
//     tool. Everything else drives emit()/verify() in-process, which says
//     nothing about whether main() reports their verdict faithfully — and the
//     exit code is the only thing scripts/build-release.sh reads.

// sumsBin is this package, compiled. Built once by TestMain rather than per
// test, since several tests exec it.
var sumsBin string

// TestMain builds the tool before running anything, because the exit-code tests
// below assert against the real binary rather than against main() as a library.
//
// A build failure FAILS the run; it does not skip it. The package under test is
// pure stdlib Go and the toolchain is already running (it is running this test),
// so "could not build it" is a broken tree, not an unavailable platform — and
// skipping would hide the exit-code contract behind a green summary, which is
// the shape this whole file exists to refuse. That is also why this differs from
// inspect-artifact's buildFixture, which legitimately skips: its fixtures are
// CROSS-compiled, and a host that cannot produce a darwin/arm64 object really
// has not been asked a question it can answer.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sha256sums-bin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not make a directory to build the tool into: %v\n", err)
		os.Exit(1)
	}
	sumsBin = filepath.Join(dir, "sha256sums")

	if out, err := exec.Command("go", "build", "-o", sumsBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "could not build the tool under test: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runSums executes the compiled tool with dir as its working directory, so that
// manifest and operand arguments can be the plain basenames the release script
// ends up passing. It returns the exit code and the combined output.
func runSums(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(sumsBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), string(out)
	}
	t.Fatalf("could not run the tool with %v: %v", args, err)
	return 0, ""
}

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

// releaseOrder is the order scripts/build-release.sh hands its artifacts to
// emit: its TARGETS list is linux/amd64, linux/arm64, darwin/amd64,
// darwin/arm64, and the loop appends in that order.
//
// It is reproduced here because it is NOT lexicographic ("darwin" sorts before
// "linux"), and that is what makes the sortedness assertion in
// TestEmitWritesTheStandardFormatWithBasenamesOnly able to fail at all. An
// operand list built straight from os.ReadDir would arrive already sorted — Go
// returns directory entries in name order — so emit's sort could be deleted
// outright and every test here would stay green, over a manifest whose row order
// had quietly become a function of the build loop rather than of the set.
var releaseOrder = []string{
	"validate-intent-linux-amd64",
	"validate-intent-linux-arm64",
	"validate-intent-darwin-amd64",
	"validate-intent-darwin-arm64",
}

// stagedOperands lists dir's files in releaseOrder, with anything else in dir
// following in REVERSE name order — so this helper never hands emit an operand
// list that is already sorted, whatever the fixture holds.
func stagedOperands(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	pending := make(map[string]bool, len(entries))
	for _, e := range entries {
		pending[e.Name()] = true
	}

	var names []string
	for _, n := range releaseOrder {
		if pending[n] {
			names = append(names, n)
			delete(pending, n)
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if pending[entries[i].Name()] {
			names = append(names, entries[i].Name())
		}
	}

	paths := make([]string, 0, len(names))
	for _, n := range names {
		paths = append(paths, filepath.Join(dir, n))
	}
	return paths
}

// emitFor digests every file in dir into dir/SHA256SUMS, the way the release
// script does, and returns the manifest path.
func emitFor(t *testing.T, dir string) string {
	t.Helper()
	manifest := filepath.Join(dir, "SHA256SUMS")
	if err := emit(manifest, stagedOperands(t, dir)); err != nil {
		t.Fatalf("emit: %v", err)
	}
	return manifest
}

func TestEmitWritesTheStandardFormatWithBasenamesOnly(t *testing.T) {
	dir := stageWith(t, stagedArtifacts)

	// The premise of the sortedness assertion at the bottom, asserted rather
	// than assumed: if a future edit ever hands emit an already-sorted operand
	// list, that assertion silently stops being able to fail and the sort it
	// guards becomes untested. Fail here instead, where the cause is visible.
	operands := stagedOperands(t, dir)
	sortedOperands := true
	for i := 1; i < len(operands); i++ {
		if filepath.Base(operands[i-1]) > filepath.Base(operands[i]) {
			sortedOperands = false
			break
		}
	}
	if sortedOperands {
		t.Fatalf("the operand list is already sorted, so it cannot show that emit sorts: %v", operands)
	}

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
	crosstest.RequireProblem(t, problems, "validate-intent-darwin-arm64 does not match SHA256SUMS")

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
	crosstest.RequireProblem(t, problems, "validate-intent-linux-arm64 is listed in SHA256SUMS but is not there")
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
	crosstest.RequireProblem(t, problems, "described by no row in SHA256SUMS: validate-intent-windows-amd64")
}

// abcRow is a WELL-FORMED row for the file every parse fixture's directory
// holds. Each fixture below is this row plus one bad row, and the pairing is the
// whole point: with only the bad row present, refusing it would leave the
// manifest with no entries at all, so parseManifest's "lists no files" guard
// would produce an error whether or not the strictness each fixture names still
// existed. Every one of these would pass over a parser that silently SKIPPED the
// rows it could not read — which is a file silently unverified, the exact trade
// this tool exists to undo. The valid row keeps the guard out of the way so the
// assertion lands on the bad row.
const abcRow = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  abc\n"

func TestVerifyRefusesAManifestWithARowItCannotParse(t *testing.T) {
	for _, tc := range []struct{ name, badRow string }{
		{"single space", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad def\n"},
		{"short digest", "ba7816bf  def\n"},
		{"not hex", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz  def\n"},
		{"escaped name", "\\ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  d\\nef\n"},
		{"path in the row", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  /tmp/stage/def\n"},
		{"a name that is a directory", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  ..\n"},
		{"blank line", "\n"},
		{"listed twice", abcRow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := stageWith(t, map[string]string{"abc": "abc"})
			manifest := filepath.Join(dir, "SHA256SUMS")
			if err := os.WriteFile(manifest, []byte(abcRow+tc.badRow), 0o644); err != nil {
				t.Fatalf("writing the manifest: %v", err)
			}

			// Refused as unexaminable rather than silently skipped: a row this
			// tool cannot parse is a file it did not check, and a run that
			// ignored it would report a green verification of fewer artifacts
			// than it claimed.
			_, _, err := verify(manifest)
			if err == nil {
				t.Fatal("a manifest with a row that cannot be parsed was accepted")
			}
			// And refused FOR THAT ROW. "lists no files" here would mean the
			// bad row was dropped and the manifest merely ended up empty — the
			// skip this fixture exists to forbid.
			if strings.Contains(err.Error(), "lists no files") {
				t.Errorf("the bad row was skipped and the manifest refused for being empty instead: %v", err)
			}
		})
	}
}

func TestVerifyRefusesAManifestThatListsNothing(t *testing.T) {
	// An empty manifest verifies trivially: nothing to re-read, nothing to
	// mismatch. A release promoted under it would carry a file asserting
	// precisely nothing about the artifacts beside it.
	dir := stageWith(t, map[string]string{"abc": "abc"})
	manifest := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(manifest, nil, 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	if _, _, err := verify(manifest); err == nil {
		t.Error("an empty manifest was accepted")
	}
}

func TestVerifyRefusesAManifestThatIsNotThere(t *testing.T) {
	// Distinct from an empty one, and it must not be readable as a pass: the
	// release script asks this tool whether the set is intact, and "there was
	// no manifest" is an answer it has to be told rather than infer.
	if _, _, err := verify(filepath.Join(t.TempDir(), "SHA256SUMS")); err == nil {
		t.Error("a manifest that does not exist was accepted")
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

// TestTheCLIExitCodeIsTheContractTheReleaseScriptReads runs the COMPILED tool.
//
// Every other test in this file calls emit() and verify() directly and asserts
// what they RETURN. scripts/build-release.sh cannot do that. It runs the binary
// and reads one number, and its `case "$sums_rc" in 0) ;; 1) die ...; *) die ...`
// is the entire consumer of everything above. So main()'s translation from
// return value to exit code is a link in the chain with a consumer and, until
// this test, no coverage: `os.Exit(1)` → `os.Exit(0)` on the problems branch is
// a one-character edit that leaves the tool reporting every mismatch it finds on
// stderr, exiting 0, and the release promoting a set whose own manifest fails
// `sha256sum -c`. A green line over an unverified release, which is the shape
// this slice exists to close — reproduced one level up, in the reporting rather
// than in the checking.
//
// The three codes are asserted EXACTLY, never merely as zero/non-zero. 1 and 2
// mean different things ("examined and wrong" vs "could not examine"), both
// main.go's package comment and the release script's two `die` messages say so
// at length, and a test that accepted either would let them collapse.
func TestTheCLIExitCodeIsTheContractTheReleaseScriptReads(t *testing.T) {
	// Emitting and verifying through the binary, in the order the release script
	// does it, so what is exercised is the round trip and not two halves that
	// have only ever been run apart.
	emitStagedSet := func(t *testing.T) string {
		t.Helper()
		dir := stageWith(t, stagedArtifacts)
		args := append([]string{"-o", "SHA256SUMS"}, releaseOrder...)
		if rc, out := runSums(t, dir, args...); rc != 0 {
			t.Fatalf("emitting a clean set exited %d, want 0:\n%s", rc, out)
		}
		return dir
	}

	t.Run("a clean set verifies: 0", func(t *testing.T) {
		dir := emitStagedSet(t)
		rc, out := runSums(t, dir, "-c", "SHA256SUMS")
		if rc != 0 {
			t.Fatalf("an untouched set exited %d, want 0:\n%s", rc, out)
		}
	})

	t.Run("a tampered artifact is examined-and-wrong: 1", func(t *testing.T) {
		dir := emitStagedSet(t)
		victim := filepath.Join(dir, "validate-intent-linux-arm64")
		if err := os.WriteFile(victim, []byte("tampered"), 0o755); err != nil {
			t.Fatalf("corrupting the staged artifact: %v", err)
		}

		rc, out := runSums(t, dir, "-c", "SHA256SUMS")
		if rc != 1 {
			t.Fatalf("a tampered artifact exited %d, want 1 (examined and wrong):\n%s", rc, out)
		}
		if !strings.Contains(out, "validate-intent-linux-arm64") {
			t.Errorf("the failing run did not name the artifact it rejected:\n%s", out)
		}
		// The tool must not claim a clean result anywhere in a run that found
		// one wrong — the release script prints this output above its own
		// summary line.
		if strings.Contains(out, "each re-read and matching") {
			t.Errorf("a failing run still printed the everything-matched note:\n%s", out)
		}
	})

	t.Run("an unlisted artifact is examined-and-wrong: 1", func(t *testing.T) {
		dir := emitStagedSet(t)
		stray := filepath.Join(dir, "validate-intent-windows-amd64")
		if err := os.WriteFile(stray, []byte("?"), 0o755); err != nil {
			t.Fatalf("staging the stray artifact: %v", err)
		}
		if rc, out := runSums(t, dir, "-c", "SHA256SUMS"); rc != 1 {
			t.Fatalf("an unlisted artifact exited %d, want 1 (examined and wrong):\n%s", rc, out)
		}
	})

	t.Run("a manifest that cannot be parsed is could-not-examine: 2", func(t *testing.T) {
		dir := stageWith(t, map[string]string{"abc": "abc"})
		if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(abcRow+"deadbeef  def\n"), 0o644); err != nil {
			t.Fatalf("writing the manifest: %v", err)
		}
		if rc, out := runSums(t, dir, "-c", "SHA256SUMS"); rc != 2 {
			t.Fatalf("an unparseable manifest exited %d, want 2 (could not examine):\n%s", rc, out)
		}
	})

	t.Run("a manifest that is not there is could-not-examine: 2", func(t *testing.T) {
		if rc, out := runSums(t, t.TempDir(), "-c", "SHA256SUMS"); rc != 2 {
			t.Fatalf("a missing manifest exited %d, want 2 (could not examine):\n%s", rc, out)
		}
	})

	t.Run("an emit that cannot digest an operand: 2", func(t *testing.T) {
		// The release script dies on a non-zero emit for the same reason it
		// dies on a failed verify: artifacts with no recorded identity are not
		// a release. A zero here would promote them anyway.
		dir := t.TempDir()
		rc, out := runSums(t, dir, "-o", "SHA256SUMS", "absent")
		if rc != 2 {
			t.Fatalf("emitting over a file that is not there exited %d, want 2:\n%s", rc, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "SHA256SUMS")); err == nil {
			t.Error("a failed emit still left a manifest behind")
		}
	})

	t.Run("a bad invocation is could-not-examine: 2", func(t *testing.T) {
		for _, args := range [][]string{
			{},                                       // neither mode
			{"-o", "SHA256SUMS"},                     // emit with nothing to digest
			{"-c", "SHA256SUMS", "extra"},            // check takes no operands
			{"-o", "SHA256SUMS", "-c", "SHA256SUMS"}, // both modes at once
		} {
			if rc, out := runSums(t, t.TempDir(), args...); rc != 2 {
				t.Errorf("invocation %v exited %d, want 2 (could not examine):\n%s", args, rc, out)
			}
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
