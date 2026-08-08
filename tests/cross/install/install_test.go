// Package install holds the calibration for scripts/install.sh.
//
// WHY THIS EXISTS
// ===============
//
// install.sh is the consuming half of the release seam: it acquires the artifact
// for the host it runs on and verifies it against the SHA256SUMS manifest
// scripts/build-release.sh emits. On a healthy release every digest matches, so
// a verifier that CANNOT fail looks exactly like one that simply never has —
// a comparison inverted, a row selected by nothing, a check skipped when the
// obvious `sha256sum -c` refuses. The install would stay green through all of
// it while verifying nothing, and the thing it installed would be a binary
// nobody checked.
//
// That is the defect scripts/build-release.sh's header names, and
// tests/cross/sha256sums/main_test.go exists for the same reason one level up.
// So the failures are asserted here, by handing install.sh a source that has
// been deliberately broken in each way a download can be broken, and requiring
// the SPECIFIC exit code and diagnostic each should produce.
//
// Four of these are load-bearing beyond the general principle:
//
//   - TestACorruptedArtifactIsRefusedAndLeavesThePrefixUntouched is the
//     non-vacuousness proof for the digest comparison. It corrupts the artifact
//     in the window between the manifest being written and the install running,
//     which is the window a download opens.
//   - TestTheRowCheckedIsTheOneDescribingThisHostsArtifact is the
//     non-vacuousness proof for the row SELECTION. Every other passing case
//     would pass just as happily on an installer that compared against whatever
//     row it read last, because on a healthy manifest every row is correct.
//   - TestAPartialSourceStillInstalls pins the constraint that makes this script
//     unable to use `sha256sum -c` at all: the manifest describes four
//     artifacts, a download brings one, and the three absent ones are not a
//     finding.
//   - TestCouldNotCheckIsAlwaysTwoAndNeverAnInstall keeps 1 and 2 apart. Both
//     refuse, but a caller reading "corrupt download" for "this manifest does
//     not describe your platform" is sent hunting a corruption that is not
//     there.
//
// WHY A GO TEST FOR A SHELL SCRIPT
// ================================
//
// tests/cross/ already holds Go programs that shell out to built artifacts, and
// go test ./... is a command README.md designates as part of the gate — so a Go
// test is the placement that actually runs. What is under test is the SCRIPT,
// executed as a subprocess exactly the way an adopter runs it, and the assertions
// are on its exit code and its output. Nothing here is linked into install.sh and
// install.sh gains no Go dependency: the whole point of the script is that it
// runs on a host with no toolchain and no clone.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// manifestName is the file install.sh verifies against — the same constant
// scripts/build-release.sh calls MANIFEST_NAME.
const manifestName = "SHA256SUMS"

// installedName is the name install.sh installs under, regardless of which
// per-target artifact it installed.
const installedName = "validate-intent"

// releaseArtifacts is the set scripts/build-release.sh produces, in its own
// naming. Spelled out here rather than derived, so that this file states what it
// believes a release contains instead of restating whatever the scripts happen
// to say — and TestTheFourTargetListsAgree then requires that belief to match
// the two scripts and tests/cross/run_cross_build.sh. A target renamed in one
// place is a failure there, by name.
var releaseArtifacts = []string{
	"validate-intent-linux-amd64",
	"validate-intent-linux-arm64",
	"validate-intent-darwin-amd64",
	"validate-intent-darwin-arm64",
}

// hostArtifact is the one install.sh must select on the machine running this
// test. runtime.GOOS/GOARCH is the same vocabulary the artifact names use, which
// is what makes this a check of the script's uname mapping rather than a
// restatement of it: the script gets there from `uname -s`/`uname -m` and has to
// arrive at the same answer.
func hostArtifact() string {
	return fmt.Sprintf("%s-%s-%s", installedName, runtime.GOOS, runtime.GOARCH)
}

// repoRoot is three levels up from tests/cross/install, where go test runs.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("could not resolve the repository root: %v", err)
	}
	return root
}

func installScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "scripts", "install.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("scripts/install.sh is not there: %v", err)
	}
	// Asserted rather than assumed: an adopter runs `scripts/install.sh`, and a
	// script committed without its executable bit fails for them and for nobody
	// here, since every test could invoke it through `bash` instead.
	if info.Mode()&0o111 == 0 {
		t.Fatalf("scripts/install.sh is not executable (mode %v)", info.Mode())
	}
	return path
}

// requireSupportedHost skips when this machine is not one of the four release
// targets. install.sh is then CORRECT to refuse everything, and every
// installs-successfully assertion below would be asserting the refusal instead —
// a green run that checked nothing. TestAnUnsupportedHostIsRefusedByName covers
// that path deliberately, with a host it fakes rather than one it happens to be
// on.
func requireSupportedHost(t *testing.T) {
	t.Helper()
	want := hostArtifact()
	for _, name := range releaseArtifacts {
		if name == want {
			return
		}
	}
	t.Skipf("this host (%s/%s) is not one of the four release targets, so there is no artifact for install.sh to install here; only its refusal could be tested",
		runtime.GOOS, runtime.GOARCH)
}

// --- fixtures ---------------------------------------------------------------

// workingArtifactBody is a stand-in for a release binary: it answers --version
// the way cmd/validate-intent does and exits 0.
//
// A shell script rather than a real cross-compiled binary, because what these
// tests exercise is install.sh — its host mapping, its manifest parsing, its
// digest comparison and its staging discipline — none of which can tell the
// difference, and all of which would otherwise be gated behind several minutes
// of cross-compilation per case. TestAgainstARealReleaseBuild closes that gap
// once, with the genuine article.
func workingArtifactBody(version string) string {
	return fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "%s %s (fake %s/%s)"
  exit 0
fi
exit 0
`, installedName, version, runtime.GOOS, runtime.GOARCH)
}

// sourceOptions describes the release directory a test wants to install from.
type sourceOptions struct {
	// artifacts written into the directory, name -> contents. Defaults to all
	// four release artifacts, the host's being a working fake.
	artifacts map[string]string

	// listed names the artifacts the manifest gets a row for. Defaults to every
	// artifact present. Rows are always digested from the file on disk at the
	// moment the manifest is written.
	listed []string

	// afterManifest runs once the manifest exists, so a test can break the
	// directory in the window a download opens.
	afterManifest func(t *testing.T, dir string)

	// manifestOverride replaces the generated manifest wholesale, for the
	// malformed-manifest cases.
	manifestOverride *string

	// omitManifest leaves the directory with no SHA256SUMS at all.
	omitManifest bool
}

// stageSource builds a release-shaped directory to install from.
func stageSource(t *testing.T, opts sourceOptions) string {
	t.Helper()
	dir := t.TempDir()

	artifacts := opts.artifacts
	if artifacts == nil {
		artifacts = map[string]string{}
		for _, name := range releaseArtifacts {
			if name == hostArtifact() {
				artifacts[name] = workingArtifactBody("1.4.0")
			} else {
				// Distinct contents per target, so a comparison against the
				// wrong row is a mismatch rather than a coincidence.
				artifacts[name] = "not this host's artifact: " + name + "\n"
			}
		}
	}
	for name, body := range artifacts {
		writeArtifact(t, filepath.Join(dir, name), body)
	}

	if opts.omitManifest {
		if opts.manifestOverride != nil || opts.listed != nil {
			t.Fatal("omitManifest with manifest content is a contradictory fixture")
		}
		return dir
	}

	manifest := filepath.Join(dir, manifestName)
	if opts.manifestOverride != nil {
		if err := os.WriteFile(manifest, []byte(*opts.manifestOverride), 0o644); err != nil {
			t.Fatalf("writing the manifest override: %v", err)
		}
	} else {
		listed := opts.listed
		if listed == nil {
			for _, name := range releaseArtifacts {
				if _, ok := artifacts[name]; ok {
					listed = append(listed, name)
				}
			}
		}
		writeManifest(t, dir, listed)
	}

	if opts.afterManifest != nil {
		opts.afterManifest(t, dir)
	}
	return dir
}

func writeArtifact(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// writeManifest emits rows in the format tests/cross/sha256sums writes and
// install.sh parses: 64 lowercase hex, two spaces, the basename.
func writeManifest(t *testing.T, dir string, names []string) {
	t.Helper()
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s  %s\n", digestOf(t, filepath.Join(dir, name)), name)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", manifestName, err)
	}
}

func digestOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("digesting %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// --- running the script -----------------------------------------------------

type result struct {
	code   int
	output string
}

// run executes install.sh the way an adopter does — as a program, not through
// an interpreter this test chose for it — and returns its exit code and its
// combined output.
func run(t *testing.T, args ...string) result {
	t.Helper()
	return runCmd(t, exec.Command(installScript(t), args...))
}

// runWithPath executes install.sh with a PATH of the test's choosing, for the
// cases about what the HOST is missing. It goes through an absolute `bash`
// because the script's `#!/usr/bin/env bash` shebang resolves the interpreter
// through PATH, and a restricted PATH would otherwise make every such case fail
// as "no interpreter" — a refusal for the wrong reason, which is exactly the
// class of false green these tests exist to prevent.
func runWithPath(t *testing.T, path string, args ...string) result {
	t.Helper()
	return runWithEnv(t, []string{"PATH=" + path}, args...)
}

func runWithEnv(t *testing.T, env []string, args ...string) result {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("no bash to run the script under: %v", err)
	}
	cmd := exec.Command(bash, append([]string{installScript(t)}, args...)...)
	// The remote cases below talk to a loopback test server, which an inherited
	// proxy setting would route somewhere else entirely and turn into a failure
	// about the environment rather than about install.sh.
	cmd.Env = append(os.Environ(), "no_proxy=*", "NO_PROXY=*")
	cmd.Env = append(cmd.Env, env...)
	return runCmd(t, cmd)
}

// baseTools are the external commands install.sh legitimately needs on any host.
// The restricted-PATH tests below assemble a PATH from these plus exactly the
// one tool under examination, so that each stays a test about the tool it names
// instead of quietly becoming a test about a missing `cp`.
var baseTools = []string{"uname", "mktemp", "mkdir", "cp", "chmod", "mv", "rm", "cat"}

// onlyTheseTools builds a PATH holding symlinks to the named commands and
// nothing else. A command this host does not have SKIPS rather than fails: the
// path through it genuinely cannot be exercised here, and saying so is not the
// same as saying it works.
func onlyTheseTools(t *testing.T, tools ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("this host has no %s, so install.sh's path through it cannot be exercised here", tool)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatalf("linking %s into the restricted PATH: %v", tool, err)
		}
	}
	return dir
}

func runCmd(t *testing.T, cmd *exec.Cmd) result {
	t.Helper()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return result{code: 0, output: string(out)}
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("could not run install.sh: %v\n%s", err, out)
	}
	return result{code: exit.ExitCode(), output: string(out)}
}

// requireNothingInstalled asserts the prefix holds exactly the entries it held
// before the run. Every refusal below is required to be this as well as the
// right exit code: a script that reports failure and installs anyway is worse
// than one that reports success, because the exit code is the only part anyone
// automates against.
func requireNothingInstalled(t *testing.T, prefix string, before map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(prefix)
	if err != nil {
		t.Fatalf("reading the prefix: %v", err)
	}
	for _, e := range entries {
		body, ok := before[e.Name()]
		if !ok {
			t.Errorf("a refused run left %q in the prefix", e.Name())
			continue
		}
		got, err := os.ReadFile(filepath.Join(prefix, e.Name()))
		if err != nil {
			t.Fatalf("reading %s back: %v", e.Name(), err)
		}
		if string(got) != body {
			t.Errorf("a refused run rewrote %q in the prefix", e.Name())
		}
	}
	if len(entries) != len(before) {
		t.Errorf("the prefix holds %d entries, want the %d it started with", len(entries), len(before))
	}
}

// --- the installing cases ---------------------------------------------------

func TestInstallsTheArtifactForThisHostAndProvesItRuns(t *testing.T) {
	requireSupportedHost(t)

	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()

	got := run(t, "--from", source, "--prefix", prefix)
	if got.code != 0 {
		t.Fatalf("installing from a clean release exited %d, want 0:\n%s", got.code, got.output)
	}

	installed := filepath.Join(prefix, installedName)
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatalf("install.sh exited 0 but %s is not there: %v\n%s", installed, err, got.output)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("%s was installed without an executable bit (mode %v)", installed, info.Mode())
	}

	// It is the HOST's artifact under the installed name, not merely some file.
	if want, err := os.ReadFile(filepath.Join(source, hostArtifact())); err != nil {
		t.Fatalf("re-reading the source artifact: %v", err)
	} else if body, err := os.ReadFile(installed); err != nil {
		t.Fatalf("reading the installed binary: %v", err)
	} else if string(body) != string(want) {
		t.Errorf("the installed binary is not the %s that was verified", hostArtifact())
	}

	// The acceptance question, asked of the installed path rather than of the
	// script's own output: does the thing that landed in the prefix run?
	out, err := exec.Command(installed, "--version").Output()
	if err != nil {
		t.Fatalf("%s --version failed after a successful install: %v", installed, err)
	}
	if !strings.HasPrefix(string(out), installedName+" 1.4.0 ") {
		t.Errorf("the installed binary reported %q, want it to start with %q", strings.TrimSpace(string(out)), installedName+" 1.4.0 ")
	}

	// Only the host's artifact was installed. A script that copied the whole
	// release into the prefix would satisfy every assertion above.
	entries, err := os.ReadDir(prefix)
	if err != nil {
		t.Fatalf("reading the prefix: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != installedName {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the prefix holds %v, want exactly [%s]", names, installedName)
	}
}

// TestAPartialSourceStillInstalls is the constraint that rules out the obvious
// implementation.
//
// The manifest describes all four release artifacts. A download brings ONE — the
// host's — and `sha256sum -c SHA256SUMS` would walk all four rows, find three
// files missing, and fail. (tests/cross/sha256sums' own check mode would fail
// even harder: it additionally requires the directory to hold nothing beyond the
// manifest and its own entries, which is right for a release being promoted and
// wrong for a partial download.) The three absent artifacts are not a finding
// here, and this test fails the moment install.sh starts treating them as one.
func TestAPartialSourceStillInstalls(t *testing.T) {
	requireSupportedHost(t)

	source := stageSource(t, sourceOptions{
		afterManifest: func(t *testing.T, dir string) {
			// The manifest keeps all four rows; the directory keeps one file.
			for _, name := range releaseArtifacts {
				if name == hostArtifact() {
					continue
				}
				if err := os.Remove(filepath.Join(dir, name)); err != nil {
					t.Fatalf("thinning the source down to a download: %v", err)
				}
			}
		},
	})
	prefix := t.TempDir()

	got := run(t, "--from", source, "--prefix", prefix)
	if got.code != 0 {
		t.Fatalf("installing from a one-artifact download exited %d, want 0 — the three artifacts this host did not download are not a finding:\n%s", got.code, got.output)
	}
	if _, err := os.Stat(filepath.Join(prefix, installedName)); err != nil {
		t.Fatalf("install.sh exited 0 but installed nothing: %v", err)
	}
}

// --- examined and found wrong: exit 1 ---------------------------------------

func TestACorruptedArtifactIsRefusedAndLeavesThePrefixUntouched(t *testing.T) {
	requireSupportedHost(t)

	source := stageSource(t, sourceOptions{
		afterManifest: func(t *testing.T, dir string) {
			// Corrupted in the window between the manifest being written and the
			// install reading it — which is the window the network opens, and the
			// same window tests/cross/sha256sums/main_test.go corrupts for the
			// producing half.
			victim := filepath.Join(dir, hostArtifact())
			body, err := os.ReadFile(victim)
			if err != nil {
				t.Fatalf("reading the artifact to corrupt: %v", err)
			}
			if err := os.WriteFile(victim, append(body, "# truncated mirror\n"...), 0o755); err != nil {
				t.Fatalf("corrupting the artifact: %v", err)
			}
		},
	})

	// A prefix that already holds a working install, because that is the state
	// an upgrade runs against and it is the one with something to lose.
	prefix := t.TempDir()
	before := map[string]string{installedName: workingArtifactBody("1.3.0")}
	writeArtifact(t, filepath.Join(prefix, installedName), before[installedName])

	got := run(t, "--from", source, "--prefix", prefix)
	if got.code != 1 {
		t.Fatalf("a corrupted artifact exited %d, want 1 (examined and found wrong):\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, hostArtifact()) {
		t.Errorf("the refusal did not name the artifact it rejected:\n%s", got.output)
	}
	requireNothingInstalled(t, prefix, before)
}

// TestTheRowCheckedIsTheOneDescribingThisHostsArtifact is the non-vacuousness
// proof for row SELECTION, as distinct from the digest comparison.
//
// Every passing case above would pass just as happily on an installer that
// compared the download against whatever row it happened to read last, because
// on a healthy manifest every row is correct for the file beside it. Here the
// host's artifact is given the contents of a DIFFERENT release artifact, whose
// row is present and whose digest is right. An installer that matched any row,
// or the first row, or the last, finds agreement and installs the wrong binary.
// Only one that selects by basename finds the mismatch.
func TestTheRowCheckedIsTheOneDescribingThisHostsArtifact(t *testing.T) {
	requireSupportedHost(t)

	var impostor string
	for _, name := range releaseArtifacts {
		if name != hostArtifact() {
			impostor = name
			break
		}
	}

	source := stageSource(t, sourceOptions{
		afterManifest: func(t *testing.T, dir string) {
			body, err := os.ReadFile(filepath.Join(dir, impostor))
			if err != nil {
				t.Fatalf("reading %s: %v", impostor, err)
			}
			if err := os.WriteFile(filepath.Join(dir, hostArtifact()), body, 0o755); err != nil {
				t.Fatalf("swapping in the impostor: %v", err)
			}
		},
	})
	prefix := t.TempDir()

	got := run(t, "--from", source, "--prefix", prefix)
	if got.code != 1 {
		t.Fatalf("an artifact carrying another target's bytes exited %d, want 1 — its digest matches %s's row, so a run that accepted it checked the wrong row:\n%s",
			got.code, impostor, got.output)
	}
	requireNothingInstalled(t, prefix, map[string]string{})
}

func TestAnArtifactThatDoesNotRunIsRefusedAfterItsDigestMatches(t *testing.T) {
	requireSupportedHost(t)

	// Digest-clean and executable, and it fails when asked what it is. The
	// digest can only say the bytes arrived intact; whether those bytes are a
	// working binary for this host is a different question, and it is the one
	// scripts/build-release.sh can only ask for the single target matching its
	// build host.
	cases := map[string]string{
		"it cannot be executed at all": "#!/bin/sh\necho 'cannot execute binary file' >&2\nexit 126\n",

		// The one case where only the EXIT CODE can catch it: a well-formed
		// version line on stdout and a non-zero status. Deleting the status
		// check leaves every other assertion here passing, so without this
		// fixture that check is untested — and `validate-intent --version ||
		// echo "not installed"` is the CI preflight
		// cmd/validate-intent/version.go was written to make honest.
		"it prints a version line and still fails": "#!/bin/sh\necho '" + installedName + " 1.4.0 (go1.22.12 fake)'\nexit 3\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			source := stageSource(t, sourceOptions{artifacts: map[string]string{hostArtifact(): body}})
			prefix := t.TempDir()

			got := run(t, "--from", source, "--prefix", prefix)
			if got.code != 1 {
				t.Fatalf("exited %d, want 1 (examined and found wrong):\n%s", got.code, got.output)
			}
			requireNothingInstalled(t, prefix, map[string]string{})
		})
	}
}

func TestAnArtifactThatRunsButIsNotValidateIntentIsRefused(t *testing.T) {
	requireSupportedHost(t)

	// Exit 0 and a line of output — everything a check of the exit code alone
	// would accept. cmd/validate-intent/version.go's third tier exists so the
	// identity token is never empty, so requiring it is not pedantry: an empty
	// one means something other than that binary answered.
	for name, body := range map[string]string{
		"something else entirely": "#!/bin/sh\necho 'some-other-tool 2.0.0'\nexit 0\n",
		"an empty version token":  "#!/bin/sh\necho '" + installedName + " '\nexit 0\n",
		"no output at all":        "#!/bin/sh\nexit 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := stageSource(t, sourceOptions{artifacts: map[string]string{hostArtifact(): body}})
			prefix := t.TempDir()

			got := run(t, "--from", source, "--prefix", prefix)
			if got.code != 1 {
				t.Fatalf("exited %d, want 1 (examined and found wrong):\n%s", got.code, got.output)
			}
			requireNothingInstalled(t, prefix, map[string]string{})
		})
	}
}

// --- could not check: exit 2 ------------------------------------------------

// TestCouldNotCheckIsAlwaysTwoAndNeverAnInstall pins the half of the contract
// that is easiest to lose.
//
// Every case here is one where install.sh has NOT found anything wrong — it has
// been unable to look. Collapsing them into 1 would report a corrupt download to
// someone whose manifest simply does not describe their platform; collapsing
// them into 0 would install a binary against no evidence at all. The exit codes
// are asserted exactly, never as merely non-zero, for the reason
// tests/cross/sha256sums/main_test.go gives about its own three.
func TestCouldNotCheckIsAlwaysTwoAndNeverAnInstall(t *testing.T) {
	requireSupportedHost(t)

	otherArtifacts := func() map[string]string {
		out := map[string]string{}
		for _, name := range releaseArtifacts {
			if name != hostArtifact() {
				out[name] = "not this host's artifact: " + name + "\n"
			}
		}
		return out
	}

	// A manifest whose only row is for some file that is not in the release at
	// all, so the artifact is present, the manifest is parseable, and there is
	// still nothing to check it against.
	strayRow := strings.Repeat("a", 64) + "  some-other-release-asset.tar.gz\n"

	cases := []struct {
		name    string
		source  func(t *testing.T) string
		args    func(source string) []string
		mustSay string
	}{
		{
			name:    "the source directory is not there",
			source:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "no-such-directory") },
			mustSay: "no-such-directory",
		},
		{
			name:    "the source holds no manifest",
			source:  func(t *testing.T) string { return stageSource(t, sourceOptions{omitManifest: true}) },
			mustSay: manifestName,
		},
		{
			name: "the source holds no artifact for this host",
			source: func(t *testing.T) string {
				return stageSource(t, sourceOptions{artifacts: otherArtifacts()})
			},
			mustSay: hostArtifact(),
		},
		{
			name: "the manifest has no row for this host's artifact",
			source: func(t *testing.T) string {
				return stageSource(t, sourceOptions{manifestOverride: &strayRow})
			},
			mustSay: "no row",
		},
		{
			name: "the manifest is empty",
			source: func(t *testing.T) string {
				empty := ""
				return stageSource(t, sourceOptions{manifestOverride: &empty})
			},
			mustSay: "empty",
		},
		{
			name: "the manifest names this host's artifact twice",
			source: func(t *testing.T) string {
				return stageSource(t, sourceOptions{
					afterManifest: func(t *testing.T, dir string) {
						writeManifest(t, dir, append(append([]string{}, releaseArtifacts...), hostArtifact()))
					},
				})
			},
			mustSay: "2 times",
		},
		{
			name: "a manifest row is malformed",
			source: func(t *testing.T) string {
				// Truncated digest. Skipping the row rather than refusing would
				// turn "I could not read your row" into "you have no row" — both
				// exit 2, but only one of them is a bug in the parser.
				bad := "deadbeef  " + hostArtifact() + "\n"
				return stageSource(t, sourceOptions{manifestOverride: &bad})
			},
			mustSay: manifestName + ":1",
		},
		{
			name:   "no source was given at all",
			source: func(t *testing.T) string { return "" },
			args:   func(string) []string { return nil },
		},
		{
			name:   "an unknown argument",
			source: func(t *testing.T) string { return stageSource(t, sourceOptions{}) },
			args: func(source string) []string {
				return []string{"--from", source, "--no-verify"}
			},
		},
		{
			name:   "--from with nothing after it",
			source: func(t *testing.T) string { return "" },
			args:   func(string) []string { return []string{"--from"} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source(t)
			prefix := t.TempDir()

			args := []string{"--from", source, "--prefix", prefix}
			if tc.args != nil {
				args = append(tc.args(source), "--prefix", prefix)
			}

			got := run(t, args...)
			if got.code != 2 {
				t.Fatalf("exited %d, want 2 (could not check) — nothing here was examined and found wrong:\n%s", got.code, got.output)
			}
			if tc.mustSay != "" && !strings.Contains(got.output, tc.mustSay) {
				t.Errorf("the refusal did not mention %q, so it did not name its cause:\n%s", tc.mustSay, got.output)
			}
			requireNothingInstalled(t, prefix, map[string]string{})
		})
	}
}

// TestAnUnsupportedHostIsRefusedByName fakes `uname` rather than waiting for a
// machine nobody has.
//
// The refusal has to NAME the host, because the useful information is not "this
// failed" but "there is no artifact for sparc64" — and because the alternative
// implementation, quietly installing the nearest match, produces an `Exec format
// error` from something that reported success.
func TestAnUnsupportedHostIsRefusedByName(t *testing.T) {
	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()

	fakeBin := t.TempDir()
	writeArtifact(t, filepath.Join(fakeBin, "uname"), `#!/bin/sh
case "$1" in
  -s) echo SunOS ;;
  -m) echo sparc64 ;;
  *)  echo unknown ;;
esac
`)

	got := runWithPath(t, fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"--from", source, "--prefix", prefix)
	if got.code != 2 {
		t.Fatalf("an unsupported host exited %d, want 2 (could not check):\n%s", got.code, got.output)
	}
	for _, want := range []string{"SunOS", "sparc64"} {
		if !strings.Contains(got.output, want) {
			t.Errorf("the refusal did not name the host component %q:\n%s", want, got.output)
		}
	}
	requireNothingInstalled(t, prefix, map[string]string{})
}

// TestAHostWithNoDigestToolIsRefusedRatherThanTrusted is the case the whole
// script is arranged around.
//
// `sha256sum` is GNU coreutils and a stock macOS does not have it; `shasum` is
// perl-based and many minimal Linux images do not have it. A host with neither
// cannot verify a download, and the only two honest answers are "refuse" and
// "install unverified and say so". This asserts the first, and asserts that the
// diagnostic names BOTH tools — a message naming only the one the author's
// machine happened to have is how the other platform's user is left guessing.
//
// The probe runs before anything is fetched, which this test also pins: the
// source it is given is perfectly good.
func TestAHostWithNoDigestToolIsRefusedRatherThanTrusted(t *testing.T) {
	requireSupportedHost(t)

	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()

	// A PATH holding the tools install.sh legitimately needs and neither digest
	// tool. Assembled from the real ones rather than stubbed, so this stays a
	// test about the missing digest tool instead of quietly becoming a test
	// about a missing `cp`.
	restricted := onlyTheseTools(t, baseTools...)
	for _, absent := range []string{"sha256sum", "shasum"} {
		if _, err := os.Stat(filepath.Join(restricted, absent)); err == nil {
			t.Fatalf("the restricted PATH still holds %s, so this test cannot be about its absence", absent)
		}
	}

	got := runWithPath(t, restricted, "--from", source, "--prefix", prefix)
	if got.code != 2 {
		t.Fatalf("a host with no SHA-256 tool exited %d, want 2 (could not check):\n%s", got.code, got.output)
	}
	for _, want := range []string{"sha256sum", "shasum"} {
		if !strings.Contains(got.output, want) {
			t.Errorf("the refusal did not name %q as a tool that would have worked:\n%s", want, got.output)
		}
	}
	requireNothingInstalled(t, prefix, map[string]string{})
}

// TestEitherDigestToolVerifiesTheDownload runs the install once per tool, each
// with the other one absent from PATH.
//
// Without this, exactly one of the two branches ever executes — whichever the CI
// image happens to ship — and the other is a code path that has never run,
// documented in README.md as the answer for the platform nobody tests on. The
// asymmetry is the whole reason both branches exist: `sha256sum` is GNU
// coreutils and a stock macOS does not have it, `shasum` is perl-based and many
// minimal Linux images do not. A host is expected to have one; this repository
// is not entitled to assume which.
func TestEitherDigestToolVerifiesTheDownload(t *testing.T) {
	requireSupportedHost(t)

	for _, tool := range []string{"sha256sum", "shasum"} {
		t.Run(tool, func(t *testing.T) {
			path := onlyTheseTools(t, append(append([]string{}, baseTools...), tool)...)
			source := stageSource(t, sourceOptions{})
			prefix := t.TempDir()

			got := runWithPath(t, path, "--from", source, "--prefix", prefix)
			if got.code != 0 {
				t.Fatalf("installing on a host whose only digest tool is %s exited %d, want 0:\n%s", tool, got.code, got.output)
			}
			if _, err := os.Stat(filepath.Join(prefix, installedName)); err != nil {
				t.Fatalf("exited 0 but installed nothing: %v", err)
			}
		})
	}
}

// --- the remote source ------------------------------------------------------

// serveRelease publishes dir over loopback HTTP. The 404 cases below hand it a
// set of paths to withhold, because "the release does not have that asset" is a
// status code, not a connection failure, and the two are worth telling apart.
//
// The server is returned rather than just its URL so that one case can close it
// early and point install.sh at a base URL nothing answers on.
func serveRelease(t *testing.T, dir string, withhold ...string) *httptest.Server {
	t.Helper()
	missing := map[string]bool{}
	for _, name := range withhold {
		missing[name] = true
	}
	files := http.FileServer(http.Dir(dir))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if missing[strings.TrimPrefix(r.URL.Path, "/")] {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestARemoteSourceIsFetchedAndVerified exercises the branch an adopter actually
// takes: a release's asset base URL rather than a directory they already have.
//
// Run once per HTTP client, each with the other absent, for the same reason the
// digest tools are: two branches, and a CI image that only ever proves one of
// them.
func TestARemoteSourceIsFetchedAndVerified(t *testing.T) {
	requireSupportedHost(t)

	for _, client := range []string{"curl", "wget"} {
		t.Run(client, func(t *testing.T) {
			path := onlyTheseTools(t, append(append([]string{}, baseTools...), "sha256sum", client)...)
			base := serveRelease(t, stageSource(t, sourceOptions{})).URL
			prefix := t.TempDir()

			got := runWithPath(t, path, "--from", base, "--prefix", prefix)
			if got.code != 0 {
				t.Fatalf("installing over HTTP with %s exited %d, want 0:\n%s", client, got.code, got.output)
			}

			out, err := exec.Command(filepath.Join(prefix, installedName), "--version").Output()
			if err != nil {
				t.Fatalf("the downloaded binary does not run: %v", err)
			}
			if !strings.HasPrefix(string(out), installedName+" 1.4.0 ") {
				t.Errorf("the downloaded binary reported %q", strings.TrimSpace(string(out)))
			}
		})
	}
}

// TestARemoteSourceThatIsNotThereIsCouldNotCheck is the case where falling
// through is most tempting and most dangerous.
//
// A 404 is a response, and a downloader told to write its body to a file writes
// the error page. Digest that and it mismatches — reporting a CORRUPT ARTIFACT
// for a URL that was never there, which sends someone hunting a truncated
// download instead of a wrong version number. Worse, if the missing file is the
// MANIFEST, there is nothing left to mismatch against at all, and "carry on
// without it" installs a binary against no evidence.
//
// So every one of these is exit 2, and none of them installs anything.
func TestARemoteSourceThatIsNotThereIsCouldNotCheck(t *testing.T) {
	requireSupportedHost(t)

	for _, client := range []string{"curl", "wget"} {
		t.Run(client, func(t *testing.T) {
			tools := append(append([]string{}, baseTools...), "sha256sum", client)

			t.Run("the release has no manifest", func(t *testing.T) {
				path := onlyTheseTools(t, tools...)
				base := serveRelease(t, stageSource(t, sourceOptions{}), manifestName).URL
				prefix := t.TempDir()

				got := runWithPath(t, path, "--from", base, "--prefix", prefix)
				if got.code != 2 {
					t.Fatalf("a 404 on %s exited %d, want 2 (could not check):\n%s", manifestName, got.code, got.output)
				}
				requireNothingInstalled(t, prefix, map[string]string{})
			})

			t.Run("the release has no artifact for this host", func(t *testing.T) {
				path := onlyTheseTools(t, tools...)
				base := serveRelease(t, stageSource(t, sourceOptions{}), hostArtifact()).URL
				prefix := t.TempDir()

				got := runWithPath(t, path, "--from", base, "--prefix", prefix)
				if got.code != 2 {
					t.Fatalf("a 404 on %s exited %d, want 2 (could not check) — a downloaded error page is not a corrupt artifact:\n%s", hostArtifact(), got.code, got.output)
				}
				if !strings.Contains(got.output, hostArtifact()) {
					t.Errorf("the refusal did not name the asset it could not fetch:\n%s", got.output)
				}
				requireNothingInstalled(t, prefix, map[string]string{})
			})

			t.Run("there is no server", func(t *testing.T) {
				path := onlyTheseTools(t, tools...)
				// A port nothing is listening on: the base URL of a server that
				// was up when the docs were written and is not now.
				dead := serveRelease(t, stageSource(t, sourceOptions{}))
				base := dead.URL
				dead.Close()
				prefix := t.TempDir()

				got := runWithPath(t, path, "--from", base, "--prefix", prefix)
				if got.code != 2 {
					t.Fatalf("an unreachable source exited %d, want 2 (could not check):\n%s", got.code, got.output)
				}
				requireNothingInstalled(t, prefix, map[string]string{})
			})
		})
	}
}

// TestARemoteSourceWithNoHTTPClientIsRefused covers the host that can verify a
// download but cannot make one. The diagnostic has to name both clients and the
// --from <dir> escape hatch, because that host's user still has a way through:
// fetch the two files by hand and point at the directory.
func TestARemoteSourceWithNoHTTPClientIsRefused(t *testing.T) {
	requireSupportedHost(t)

	path := onlyTheseTools(t, append(append([]string{}, baseTools...), "sha256sum")...)
	base := serveRelease(t, stageSource(t, sourceOptions{})).URL
	prefix := t.TempDir()

	got := runWithPath(t, path, "--from", base, "--prefix", prefix)
	if got.code != 2 {
		t.Fatalf("a host with neither curl nor wget exited %d, want 2 (could not check):\n%s", got.code, got.output)
	}
	for _, want := range []string{"curl", "wget", "--from"} {
		if !strings.Contains(got.output, want) {
			t.Errorf("the refusal did not mention %q:\n%s", want, got.output)
		}
	}
	requireNothingInstalled(t, prefix, map[string]string{})
}

// TestADirectoryOnTheInstallNameIsRefused covers the one way `mv` can succeed
// and still not install anything: `mv src dir` moves src INTO dir. A prefix
// holding a DIRECTORY called validate-intent would swallow the artifact, and the
// run would report a successful install of a binary nothing can execute.
func TestADirectoryOnTheInstallNameIsRefused(t *testing.T) {
	requireSupportedHost(t)

	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()
	if err := os.Mkdir(filepath.Join(prefix, installedName), 0o755); err != nil {
		t.Fatalf("staging a directory on the install name: %v", err)
	}

	got := run(t, "--from", source, "--prefix", prefix)
	if got.code != 2 {
		t.Fatalf("a directory on the install name exited %d, want 2 (could not check):\n%s", got.code, got.output)
	}

	entries, err := os.ReadDir(filepath.Join(prefix, installedName))
	if err != nil {
		t.Fatalf("reading the directory back: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("the run moved something into the directory rather than refusing: %v", entries)
	}
}

// --- the lists and defaults that have to agree ------------------------------

// shellTargets reads the `TARGETS=( ... )` block out of a shell script.
//
// Every failure here is a t.Fatal rather than a skip or an empty result, because
// the whole value of TestTheFourTargetListsAgree is that it FAILS when the lists
// diverge — and a parser that quietly returned nothing would make four empty
// lists agree perfectly while the scripts disagreed.
func shellTargets(t *testing.T, rel string) []string {
	t.Helper()
	path := filepath.Join(repoRoot(t), rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}

	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "TARGETS=(" {
			continue
		}
		if start != -1 {
			t.Fatalf("%s holds more than one `TARGETS=(` block, so which one describes the release is a guess", rel)
		}
		start = i
	}
	if start == -1 {
		t.Fatalf("%s holds no `TARGETS=(` block. This test can no longer see the list it compares, which is not the same as the lists agreeing — re-point it rather than deleting it", rel)
	}

	var targets []string
	for _, line := range lines[start+1:] {
		entry := strings.TrimSpace(line)
		if entry == ")" {
			if len(targets) == 0 {
				t.Fatalf("%s: the TARGETS block is empty", rel)
			}
			return targets
		}
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if len(entry) < 3 || !strings.HasPrefix(entry, `"`) || !strings.HasSuffix(entry, `"`) {
			t.Fatalf("%s: cannot read %q as a target entry; this test parses one quoted \"goos/goarch\" per line", rel, entry)
		}
		targets = append(targets, strings.Trim(entry, `"`))
	}
	t.Fatalf("%s: the `TARGETS=(` block is never closed", rel)
	return nil
}

// TestTheFourTargetListsAgree is the check that makes the duplication of TARGETS
// safe to have.
//
// The list of release targets is written out FOUR times in this repository:
// scripts/build-release.sh builds them, scripts/install.sh maps a host onto one
// of them, tests/cross/run_cross_build.sh walks them, and this file names the
// artifacts they produce. install.sh cannot read the producer's copy at runtime
// — it runs on a host with no clone, which is the entire premise of the script —
// so the copy is unavoidable, and only an agreement check makes it honest.
//
// Nothing else in the suite can catch a divergence. Every other test here runs
// on ONE machine and therefore only ever exercises the single target that
// machine is, and both scripts derive that one from the same `uname`/`runtime`
// answer — so it is the one entry that can never disagree. Rename darwin/amd64
// in build-release.sh and, without this test, a linux/amd64 runner stays fully
// green while install.sh asks releases for an artifact that no longer exists.
// That is the same "a target quietly dropped or renamed" defect
// build-release.sh's staging discipline and tests/cross/sha256sums' stray-file
// check exist to close on the producing side.
//
// Compared as SETS, not as ordered lists: build-release.sh's order fixes the
// manifest's row order and run_cross_build.sh's fixes its report order, and both
// are theirs to choose. What must not differ is WHICH targets are named.
func TestTheFourTargetListsAgree(t *testing.T) {
	// This file's own belief, converted into the producer's vocabulary so all
	// four are compared as the same kind of thing.
	var fromThisFile []string
	for _, name := range releaseArtifacts {
		rest := strings.TrimPrefix(name, installedName+"-")
		goos, goarch, ok := strings.Cut(rest, "-")
		if rest == name || !ok || goos == "" || goarch == "" {
			t.Fatalf("releaseArtifacts holds %q, which is not a %s-<goos>-<goarch> name", name, installedName)
		}
		fromThisFile = append(fromThisFile, goos+"/"+goarch)
	}

	// build-release.sh is the authority: it is the script that decides what a
	// release contains. Everything else is a consumer of that decision.
	const authorityName = "scripts/build-release.sh"
	authority := shellTargets(t, authorityName)

	for _, other := range []struct {
		name    string
		targets []string
	}{
		{"scripts/install.sh", shellTargets(t, "scripts/install.sh")},
		{"tests/cross/run_cross_build.sh", shellTargets(t, "tests/cross/run_cross_build.sh")},
		{"tests/cross/install/install_test.go's releaseArtifacts", fromThisFile},
	} {
		if missing, extra := setDiff(authority, other.targets); len(missing) > 0 || len(extra) > 0 {
			t.Errorf("%s does not name the same release targets as %s:\n  missing from %s: %v\n  named only by %s: %v\n"+
				"A release built from %s would not contain what %s asks for.",
				other.name, authorityName,
				other.name, missing,
				other.name, extra,
				authorityName, other.name)
		}
	}
}

// setDiff reports what is in want but not got, and what is in got but not want.
func setDiff(want, got []string) (missing, extra []string) {
	index := func(items []string) map[string]bool {
		m := make(map[string]bool, len(items))
		for _, item := range items {
			m[item] = true
		}
		return m
	}
	wantSet, gotSet := index(want), index(got)
	for _, item := range want {
		if !gotSet[item] {
			missing = append(missing, item)
		}
	}
	for _, item := range got {
		if !wantSet[item] {
			extra = append(extra, item)
		}
	}
	return missing, extra
}

// TestPrefixInTheEnvironmentDoesNotRedirectTheInstall pins where the binary is
// allowed to come from being decided.
//
// `PREFIX` is one of the most commonly exported variables in a build shell, and
// this script puts an executable on a host. install.sh used to read it as a
// fallback default, so a machine with PREFIX set anywhere in its environment
// would silently install somewhere other than the location `--help` promises,
// with no flag passed and nothing in the output to suggest a choice had been
// made. Two sources of truth — the flag and the built-in default — and no third.
func TestPrefixInTheEnvironmentDoesNotRedirectTheInstall(t *testing.T) {
	requireSupportedHost(t)

	t.Run("with no --prefix it is ignored", func(t *testing.T) {
		// The artifact verifies cleanly and then fails `--version`, so the run is
		// refused at the LAST step: after the prefix has been resolved and
		// created — the moment this test is about — and before anything is
		// installed anywhere. A fixture that installed successfully would, on a
		// correct script, write into the real /usr/local/bin of whatever machine
		// ran the suite, which is not a thing a test may do.
		source := stageSource(t, sourceOptions{artifacts: map[string]string{
			hostArtifact(): "#!/bin/sh\nexit 3\n",
		}})
		envPrefix := filepath.Join(t.TempDir(), "chosen-by-the-environment")

		got := runWithEnv(t, []string{"PREFIX=" + envPrefix}, "--from", source)

		// Non-vacuousness. The assertion below only means anything if the run got
		// far enough to choose and create a prefix; this line is printed
		// immediately before it does. Without this guard, a script that refused
		// early for an unrelated reason would pass by never having tried.
		if !strings.Contains(got.output, "matches "+manifestName) {
			t.Fatalf("the run never reached the install step, so it never chose a prefix and this test observed nothing (exit %d):\n%s", got.code, got.output)
		}
		if got.code == 0 {
			t.Fatalf("an artifact that fails --version was installed anyway:\n%s", got.output)
		}
		if _, err := os.Stat(envPrefix); !os.IsNotExist(err) {
			t.Errorf("PREFIX in the environment redirected the install: %s was created. Only --prefix and the default install.sh documents may decide where a binary lands.", envPrefix)
		}
	})

	t.Run("--prefix wins over it", func(t *testing.T) {
		source := stageSource(t, sourceOptions{})
		prefix := t.TempDir()
		envPrefix := filepath.Join(t.TempDir(), "chosen-by-the-environment")

		got := runWithEnv(t, []string{"PREFIX=" + envPrefix}, "--from", source, "--prefix", prefix)
		if got.code != 0 {
			t.Fatalf("installing with PREFIX set in the environment exited %d, want 0:\n%s", got.code, got.output)
		}
		if _, err := os.Stat(filepath.Join(prefix, installedName)); err != nil {
			t.Errorf("--prefix was not honoured: %v\n%s", err, got.output)
		}
		if _, err := os.Stat(envPrefix); !os.IsNotExist(err) {
			t.Errorf("PREFIX in the environment was used despite an explicit --prefix: %s was created", envPrefix)
		}
	})
}

// TestUsageReportsTheDefaultPrefixItActuallyUses closes the loop the other way.
//
// The complaint that produced these two tests was not that the default was
// wrong, it was that `--help` and the code disagreed about it and nothing
// noticed. So the help text is required to name the same path the script
// installs to when it is given no --prefix.
func TestUsageReportsTheDefaultPrefixItActuallyUses(t *testing.T) {
	script, err := os.ReadFile(installScript(t))
	if err != nil {
		t.Fatalf("reading install.sh: %v", err)
	}

	// The single literal the script is allowed to hold, read back out of it so
	// this test asserts agreement rather than restating a third copy of the path.
	const marker = `DEFAULT_PREFIX="`
	i := strings.Index(string(script), marker)
	if i < 0 {
		t.Fatalf("install.sh no longer holds a DEFAULT_PREFIX assignment; the default it uses is now unfindable from here")
	}
	rest := string(script)[i+len(marker):]
	def, _, ok := strings.Cut(rest, `"`)
	if !ok || def == "" {
		t.Fatalf("could not read the DEFAULT_PREFIX value out of install.sh")
	}

	help := run(t, "--help")
	if help.code != 0 {
		t.Fatalf("--help exited %d, want 0:\n%s", help.code, help.output)
	}
	if !strings.Contains(help.output, "default: "+def) {
		t.Errorf("install.sh installs to %s by default, but --help does not say so:\n%s", def, help.output)
	}
}

// --- the genuine article ----------------------------------------------------

// TestAgainstARealReleaseBuild is the acceptance criterion, end to end:
//
//	scripts/build-release.sh 1.4.0 && scripts/install.sh --from dist/release --prefix <tmp>
//	<tmp>/validate-intent --version   →  exit 0, reporting 1.4.0
//
// Everything above installs a shell script standing in for a release binary,
// which is the right trade for the twenty-odd cases that are about install.sh's
// own logic — but it means none of them has ever seen the two halves of the
// release seam meet: a manifest this repository's producer actually wrote, over
// artifacts it actually cross-compiled, with the version stamp linked in.
//
// What this does NOT add, since a stand-in binary is the obvious thing to think
// it adds it: it does not exercise the embedded-schema fallback either.
// `--version` is answered and returned above cmd/validate-intent's LoadSchema()
// call, so a real artifact answers it without ever loading a schema. The
// fallback on a bare prefix is checked end to end by
// tests/cross/run_cross_build.sh:266-287, which asserts the installed prefix has
// no schemas/ and then runs a real fixture through the installed binary.
//
// Skipped under -short because it cross-compiles four targets. It is not skipped
// when the toolchain is merely absent from PATH: `go test` is running, so a Go
// toolchain exists, and it is handed to the script through the GO variable that
// script already reads. "Could not build it" here is a broken tree, not an
// unavailable platform.
func TestAgainstARealReleaseBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles the four release targets; run without -short")
	}
	requireSupportedHost(t)

	root := repoRoot(t)
	dist := filepath.Join(t.TempDir(), "release")

	build := exec.Command(filepath.Join(root, "scripts", "build-release.sh"), "1.4.0")
	build.Dir = root
	build.Env = append(os.Environ(),
		"DIST="+dist,
		"GO="+filepath.Join(runtime.GOROOT(), "bin", "go"),
	)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("scripts/build-release.sh 1.4.0 failed: %v\n%s", err, out)
	}

	prefix := t.TempDir()
	got := run(t, "--from", dist, "--prefix", prefix)
	if got.code != 0 {
		t.Fatalf("installing a real release exited %d, want 0:\n%s", got.code, got.output)
	}

	installed := filepath.Join(prefix, installedName)
	out, err := exec.Command(installed, "--version").Output()
	if err != nil {
		t.Fatalf("%s --version failed: %v", installed, err)
	}
	// The stamp, not the whole line: build-release.sh appends `-dirty` when the
	// working tree is modified, which it legitimately is while this very change
	// is being written.
	if !strings.HasPrefix(string(out), installedName+" 1.4.0") {
		t.Errorf("the installed release reported %q, want it to report 1.4.0", strings.TrimSpace(string(out)))
	}

	// The install verified against the release's own manifest, so a release whose
	// manifest did not describe it could not have got here. Asserted anyway,
	// because the two files are produced by different scripts and the naming
	// agreement between them is the entire interface.
	if _, err := os.Stat(filepath.Join(dist, manifestName)); err != nil {
		t.Errorf("the release directory has no %s: %v", manifestName, err)
	}
	if _, err := os.Stat(filepath.Join(dist, hostArtifact())); err != nil {
		t.Errorf("the release directory has no %s, so install.sh and build-release.sh disagree about artifact naming: %v", hostArtifact(), err)
	}
}
