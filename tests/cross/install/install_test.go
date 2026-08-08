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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
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

// TestASuccessfulInstallPrintsTheWiringLineNamingWhatItInstalled is the other
// half of "installed $TARGET_PATH".
//
// specguard-rspec's README tells the reader to point SPECGUARD_VALIDATE_INTENT
// at a `validate-intent` binary, and this script installs exactly that binary.
// Until this test, the two facts never met in the output: the variable's name
// appeared once in the script, inside its own header comment, and the reader who
// had just run the installer still had to go to the other repository's README
// and hand-assemble a path this script already held in a variable.
//
// Both arms of the PATH case are covered, because they fail differently. The
// off-PATH arm prints a note today and could plausibly grow the wiring line by
// accident; the on-PATH arm prints NOTHING after the version line, so it is the
// one where a conditional implementation would leave the reader with no wiring
// at all and no signal that anything was withheld.
//
// The wanted path is derived here the same way the script derives it —
// EvalSymlinks over the prefix, then joined with the installed name — rather
// than restated as a literal. t.TempDir() is under a symlinked /var on some
// hosts, and the script resolves its prefix with `pwd -P` before building
// $TARGET_PATH, so an unresolved join would assert against a path the script has
// no reason to ever print. A hardcoded /usr/local/bin would be worse still: it
// would pass only when the test stopped using --prefix.
//
// The assertion is a ROUND TRIP rather than a substring match, because the claim
// the line makes is not "these bytes appeared" — it is "paste this into a shell
// and the variable will name the binary just installed". Those come apart the
// moment the path holds a character the shell acts on: an unquoted
// `export SPECGUARD_VALIDATE_INTENT=/tmp/pre fix/validate-intent` contains the
// full path as a substring, and still leaves the variable holding `/tmp/pre`.
// So the emitted line is handed to a real bash, and what that bash ended up with
// is what gets compared. The third subtest supplies a prefix that requires
// quoting; without it the first two would pass just as happily on a line that
// cannot survive being pasted.
func TestASuccessfulInstallPrintsTheWiringLineNamingWhatItInstalled(t *testing.T) {
	requireSupportedHost(t)

	// The gem refuses a bare command name outright, so the assertion is on an
	// absolute path or it is not an assertion about the shape the gem accepts.
	assertWired := func(t *testing.T, got result, prefix string) {
		t.Helper()
		if got.code != 0 {
			t.Fatalf("installing from a clean release exited %d, want 0:\n%s", got.code, got.output)
		}
		resolved, err := filepath.EvalSymlinks(prefix)
		if err != nil {
			t.Fatalf("resolving the prefix the way install.sh does: %v", err)
		}
		want := filepath.Join(resolved, installedName)
		if !filepath.IsAbs(want) {
			t.Fatalf("the derived install path %q is not absolute, so this test cannot check the shape the gem requires", want)
		}

		// Anchored on the whole command, not just on the variable name: a
		// substring match would be satisfied by any prose that happened to
		// mention SPECGUARD_VALIDATE_INTENT=<path>, which is not the thing the
		// reader is being told to run.
		line := wiringLine(t, got.output)

		value := valueAfterPasting(t, line)
		if value != want {
			t.Errorf("pasting the emitted wiring line into a shell left SPECGUARD_VALIDATE_INTENT=%q, want %q\nline: %s\n%s",
				value, want, line, got.output)
			return
		}
		// What the variable names has to be the binary this run installed, not
		// merely a string that matches: the whole failure mode being pinned is a
		// value that reads plausibly and names nothing.
		info, err := os.Stat(value)
		if err != nil {
			t.Errorf("SPECGUARD_VALIDATE_INTENT would be set to %q, which does not exist: %v", value, err)
			return
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("SPECGUARD_VALIDATE_INTENT would be set to %q, which is not executable (mode %v)", value, info.Mode())
		}
	}

	t.Run("when the prefix is not on PATH", func(t *testing.T) {
		source := stageSource(t, sourceOptions{})
		prefix := t.TempDir()

		got := run(t, "--from", source, "--prefix", prefix)
		assertWired(t, got, prefix)

		// The pre-existing note is a separate, shell-scoped thing and keeps
		// firing on exactly the condition it fired on before.
		if !strings.Contains(got.output, "is not on your PATH") {
			t.Errorf("the off-PATH note stopped firing on a prefix that is not on PATH:\n%s", got.output)
		}
	})

	t.Run("when the prefix is already on PATH", func(t *testing.T) {
		source := stageSource(t, sourceOptions{})
		prefix := t.TempDir()

		// The script compares against $PREFIX AFTER `pwd -P`, so PATH has to
		// carry the resolved spelling or this subtest would silently exercise
		// the off-PATH arm again and prove nothing about the on-PATH one.
		resolved, err := filepath.EvalSymlinks(prefix)
		if err != nil {
			t.Fatalf("resolving the prefix the way install.sh does: %v", err)
		}
		got := runWithEnv(t, []string{"PATH=" + os.Getenv("PATH") + string(os.PathListSeparator) + resolved},
			"--from", source, "--prefix", prefix)
		assertWired(t, got, prefix)

		// The arm that says nothing today must still say nothing about PATH.
		if strings.Contains(got.output, "is not on your PATH") {
			t.Errorf("the off-PATH note fired for a prefix that IS on PATH:\n%s", got.output)
		}
	})

	// --prefix accepts a path with a space in it — the install succeeds, exit 0,
	// binary in place. So the line printed for such a prefix is a line real
	// readers get, and it is the one where "printed the path" and "printed
	// something that pastes" stop being the same assertion.
	t.Run("when the prefix needs quoting to survive a paste", func(t *testing.T) {
		source := stageSource(t, sourceOptions{})
		prefix := filepath.Join(t.TempDir(), "pre fix dir")

		got := run(t, "--from", source, "--prefix", prefix)
		assertWired(t, got, prefix)

		// Non-vacuousness: if the path ever stops holding a character the shell
		// would act on, this subtest is a duplicate of the first one and is
		// proving nothing about quoting.
		if !strings.ContainsAny(prefix, " \t\"'$`\\*?") {
			t.Fatalf("the prefix %q needs no quoting, so this subtest no longer exercises the case it was written for", prefix)
		}
	})
}

// ansiSequence matches the colour codes red()/green()/dim() wrap their output
// in. The assertions below are about the TEXT install.sh emits, and the reader
// pasting a line out of their terminal does not paste the escapes.
var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// wiringLine picks the `export SPECGUARD_VALIDATE_INTENT=…` command out of an
// install's output, as the reader would select that line and copy it.
//
// Exactly one is required. Two would mean a reader choosing between them with
// nothing to choose on; zero is the failure this whole test exists to catch, and
// naming it here keeps the round trip below from reporting it as some confusing
// downstream shell error.
func wiringLine(t *testing.T, output string) string {
	t.Helper()
	const command = "export SPECGUARD_VALIDATE_INTENT="
	var found []string
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(ansiSequence.ReplaceAllString(raw, ""))
		if strings.HasPrefix(line, command) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one %q line in a successful install's output, found %d:\n%s", command, len(found), output)
	}
	return found[0]
}

// valueAfterPasting runs the emitted line in a real bash and reports what
// SPECGUARD_VALIDATE_INTENT ends up holding — the property the line claims,
// checked the way the reader exercises it.
//
// The line is handed to bash verbatim rather than re-quoted here, because
// re-quoting it would be this test asserting against its own idea of the value
// instead of against the bytes install.sh printed. A malformed line is NOT an
// error to bail on: `export VAR=/tmp/pre fix/x` fails on the `fix/x` word and
// bash carries on, leaving the variable set to a truncated path — which is
// precisely the outcome to catch and compare, not to skip past.
func valueAfterPasting(t *testing.T, line string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("this host has no bash, so what the emitted line does when pasted cannot be exercised here: %v", err)
	}
	cmd := exec.Command(bash, "-c", line+`; printf %s "${SPECGUARD_VALIDATE_INTENT:-}"`)
	// A value inherited from the surrounding environment would make an
	// export that never ran look like one that worked.
	cmd.Env = append(os.Environ(), "SPECGUARD_VALIDATE_INTENT=")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("could not run the emitted wiring line under bash: %v", err)
		}
		t.Logf("pasting %q into bash exited %d: %s", line, exit.ExitCode(), stderr.String())
	}
	return stdout.String()
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

// TestAnArtifactThatPassesVersionAndFailsItsSelfTestIsRefused is the
// non-vacuousness proof for the check SPGD-315 added.
//
// `--version` returns above LoadSchema, so every assertion in this file before
// this one is satisfied by a binary that cannot validate a single document.
// install.sh now also runs the artifact bare — the self-test, over the corpus
// compiled into it — and that check is the only one here that reaches the
// validator at all. Without a fixture that passes the first and fails the
// second, it would be a line of shell nothing ever exercised: on a healthy
// release both succeed, so a deleted or inverted self-test check would look
// exactly like a working one, which is the failure mode this whole file exists
// to close.
//
// It is exit 1 and not 2 for the reason the header gives: the artifact was
// fetched, digest-verified, executed and EXAMINED. It ran the check and did not
// pass it. "Could not check" is the one thing this is not.
func TestAnArtifactThatPassesVersionAndFailsItsSelfTestIsRefused(t *testing.T) {
	requireSupportedHost(t)

	// Answers --version exactly as a healthy release does, and fails when asked
	// to prove it validates anything. A binary whose embedded schema or corpus
	// was broken behaves precisely like this.
	body := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "%s 1.4.0 (fake %s/%s)"
  exit 0
fi
echo "error: no fixtures match 'examples/*.json' — self-test cannot verify acceptance" >&2
exit 1
`, installedName, runtime.GOOS, runtime.GOARCH)

	source := stageSource(t, sourceOptions{artifacts: map[string]string{hostArtifact(): body}})
	prefix := t.TempDir()

	got := run(t, "--from", source, "--prefix", prefix)
	if got.code != 1 {
		t.Fatalf("exited %d, want 1 (examined and found wrong):\n%s", got.code, got.output)
	}
	// The diagnostic must say which check failed. "the artifact does not run"
	// would send someone hunting a corrupt download for a binary that ran fine.
	if !strings.Contains(got.output, "self-test") {
		t.Errorf("the refusal never mentions the self-test, so it reads as a different failure:\n%s", got.output)
	}
	requireNothingInstalled(t, prefix, map[string]string{})
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

// --- the interpreter the script itself needs --------------------------------
//
// Everything above runs install.sh under bash — `run` through the shebang,
// `runWithEnv` through an ABSOLUTE bash chosen precisely so that a restricted
// PATH cannot turn a case about a missing digest tool into a case about a
// missing interpreter. That is right for those cases and it left one axis
// untested: the suite whose subject is what the HOST is missing never asked what
// happens when the missing thing is bash itself.
//
// It was the one miss install.sh could not recover from. `sh install.sh` and
// `curl … | sh` bypass the `#!/usr/bin/env bash` shebang entirely, and on the
// common CI base /bin/sh is dash, which died on a raw shell syntax error at the
// TARGETS array — no exit code from the script's own vocabulary and no
// diagnostic naming what was missing.
//
// These cases are ADDITIVE and deliberately do NOT go through runWithEnv: a
// helper that resolves bash is the thing being excluded here.

// nonBashShell returns a command that runs a POSIX shell which is NOT bash, or
// skips.
//
// The candidate is checked rather than trusted: `ash` and `sh` are bash under
// another name on some hosts, and a case that thought it was exercising the
// non-bash path while running bash would pass for the wrong reason forever —
// which is the same vacuous green the rest of this file is built to refuse. A
// candidate whose $BASH_VERSION is set is rejected and the next one tried.
func nonBashShell(t *testing.T) []string {
	t.Helper()
	for _, cand := range [][]string{{"dash"}, {"ash"}, {"busybox", "sh"}, {"sh"}} {
		bin, err := exec.LookPath(cand[0])
		if err != nil {
			continue
		}
		cmdline := append([]string{bin}, cand[1:]...)
		out, err := exec.Command(cmdline[0], append(cmdline[1:], "-c", `echo "${BASH_VERSION:-none}"`)...).Output()
		if err != nil || strings.TrimSpace(string(out)) != "none" {
			continue // this one IS bash, whatever it is called
		}
		return cmdline
	}
	t.Skip("this host has no POSIX shell that is not bash, so install.sh's behaviour under one cannot be exercised here")
	return nil
}

// runUnder executes install.sh through an interpreter this test chose, with an
// optional PATH override and an optional script-on-stdin.
//
// The context bound is load-bearing rather than tidiness: the guard under test
// re-execs the script under bash, and the failure mode of a re-exec that does
// not converge is a process that never returns. A hang must be a failed test,
// not a suite that sits there.
func runUnder(t *testing.T, shell []string, path string, stdin string, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	argv := append([]string{}, shell[1:]...)
	if stdin == "" {
		argv = append(argv, installScript(t))
		argv = append(argv, args...)
	} else {
		argv = append(argv, "-s", "--")
		argv = append(argv, args...)
	}

	cmd := exec.CommandContext(ctx, shell[0], argv...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "no_proxy=*", "NO_PROXY=*")
	if path != "" {
		cmd.Env = append(cmd.Env, "PATH="+path)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	got := runCmd(t, cmd)
	if ctx.Err() != nil {
		t.Fatalf("install.sh did not terminate under %v — a re-exec that does not converge:\n%s", shell, got.output)
	}
	return got
}

// TestTheScriptParsesUnderAPosixShell is the static half of the guard: the file
// must PARSE as POSIX sh, which is a stronger statement than the guard running.
//
// dash parses and executes incrementally, so a guard at the top of the file runs
// before the bash-only array further down is ever reached — that is why the
// runtime cases below pass. But a shell that parsed the whole script before
// executing any of it would die at the array without reaching the explanation,
// and so would any `sh -n` lint an adopter points at the file. Hence the `eval`
// around TARGETS in install.sh, and hence this check.
//
// Parseable is NOT the same as runnable, and the difference is deliberate:
// install.sh is still bash to RUN — `${line:0:64}` and `shopt -s nocasematch`
// are POSIX-parseable and POSIX-broken. Porting those is a larger change that
// was consciously not made, and the guard is what makes not making it safe.
func TestTheScriptParsesUnderAPosixShell(t *testing.T) {
	shell := nonBashShell(t)

	// The falsifier for this whole test. `-n` has to be a real parse, not a
	// no-op: if this shell accepted a bash array, the assertion below would pass
	// on a file that had never been made POSIX-parseable at all.
	control := filepath.Join(t.TempDir(), "control.sh")
	if err := os.WriteFile(control, []byte("X=(\n  \"a\"\n)\n"), 0o644); err != nil {
		t.Fatalf("writing the control script: %v", err)
	}
	if got := runCmd(t, exec.Command(shell[0], append(append([]string{}, shell[1:]...), "-n", control)...)); got.code == 0 {
		t.Skipf("%v -n accepts a bash array, so it cannot tell whether install.sh is POSIX-parseable", shell)
	}

	got := runCmd(t, exec.Command(shell[0], append(append([]string{}, shell[1:]...), "-n", installScript(t))...))
	if got.code != 0 {
		t.Fatalf("`%v -n scripts/install.sh` exited %d, want 0 — the file does not parse as POSIX sh, so a shell that parses before it runs would die on a syntax error instead of reaching the interpreter guard:\n%s",
			shell, got.code, got.output)
	}
}

// TestANonBashShellReExecsIntoBashRatherThanDyingOnASyntaxError is the case the
// suite could not previously have: install.sh started by a shell that is not
// bash, on a host that HAS bash.
//
// The required answer is a normal install. The script's shebang was bypassed, it
// noticed, and it re-ran itself under the interpreter it needs — which is the
// difference between a dependency probed and a dependency assumed.
func TestANonBashShellReExecsIntoBashRatherThanDyingOnASyntaxError(t *testing.T) {
	requireSupportedHost(t)
	shell := nonBashShell(t)

	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()

	got := runUnder(t, shell, "", "", "--from", source, "--prefix", prefix)
	if got.code != 0 {
		t.Fatalf("install.sh run under %v exited %d, want 0:\n%s", shell, got.code, got.output)
	}
	if strings.Contains(got.output, "Syntax error") || strings.Contains(got.output, "unexpected") {
		t.Errorf("the run produced a shell syntax error rather than an install:\n%s", got.output)
	}
	if _, err := os.Stat(filepath.Join(prefix, installedName)); err != nil {
		t.Fatalf("exited 0 under %v but installed nothing: %v\n%s", shell, err, got.output)
	}
}

// TestAHostWithNoBashIsRefusedRatherThanRunUnchecked is criterion 3: the
// interpreter is the one dependency whose absence used to produce a raw syntax
// error. It now produces the same answer every other missing dependency here
// does — exit 2, the miss named, nothing installed.
//
// The source it is given is perfectly good, so this stays a test about the
// interpreter rather than about anything the run would have found later.
func TestAHostWithNoBashIsRefusedRatherThanRunUnchecked(t *testing.T) {
	requireSupportedHost(t)
	shell := nonBashShell(t)

	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()

	restricted := onlyTheseTools(t, baseTools...)
	if _, err := os.Stat(filepath.Join(restricted, "bash")); err == nil {
		t.Fatal("the restricted PATH still holds bash, so this test cannot be about its absence")
	}

	got := runUnder(t, shell, restricted, "", "--from", source, "--prefix", prefix)
	if got.code != 2 {
		t.Fatalf("a host with no bash exited %d, want 2 (could not check):\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "bash") {
		t.Errorf("the refusal did not name bash as the miss:\n%s", got.output)
	}
	requireNothingInstalled(t, prefix, map[string]string{})
}

// TestABashOnPathThatIsNotBashIsRefusedRatherThanReExecedForever pins the
// termination of the re-exec.
//
// A guard that re-execs under `command -v bash` and re-runs the same guard is
// one bad symlink away from an installer that never returns — and a hang is the
// worst of the available failures, because it reports nothing at all. The second
// pass refuses instead, and says which of the two things is wrong: not "no
// bash", but "the bash on PATH is not bash".
func TestABashOnPathThatIsNotBashIsRefusedRatherThanReExecedForever(t *testing.T) {
	requireSupportedHost(t)
	shell := nonBashShell(t)

	source := stageSource(t, sourceOptions{})
	prefix := t.TempDir()

	restricted := onlyTheseTools(t, baseTools...)
	if err := os.Symlink(shell[0], filepath.Join(restricted, "bash")); err != nil {
		t.Fatalf("planting a fake bash on the restricted PATH: %v", err)
	}

	got := runUnder(t, shell, restricted, "", "--from", source, "--prefix", prefix)
	if got.code != 2 {
		t.Fatalf("a PATH whose `bash` is not bash exited %d, want 2 (could not check):\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "bash") {
		t.Errorf("the refusal did not name bash:\n%s", got.output)
	}
	requireNothingInstalled(t, prefix, map[string]string{})
}

// TestTheScriptOnStdinIsRefusedByANonBashShellAndWorksUnderBash is the
// `curl … | sh` half of the boundary item this script exists for.
//
// Piped, there is no file for the script to hand to bash: it arrives on stdin
// and $0 names the SHELL. That shell binary is a real, readable file, so an
// existence test on $0 answers yes and a re-exec on it hands bash an ELF — which
// is why the guard identifies itself by its shebang line instead. bash being
// present on the host does not make this recoverable, so it is exit 2 with the
// working invocation spelled out.
//
// The second half is the one that must NOT have regressed: `curl … | bash` is
// the invocation the README documents, and it still installs.
func TestTheScriptOnStdinIsRefusedByANonBashShellAndWorksUnderBash(t *testing.T) {
	requireSupportedHost(t)
	shell := nonBashShell(t)

	body, err := os.ReadFile(installScript(t))
	if err != nil {
		t.Fatalf("reading install.sh to pipe it: %v", err)
	}

	t.Run("piped into a non-bash shell", func(t *testing.T) {
		source := stageSource(t, sourceOptions{})
		prefix := t.TempDir()

		got := runUnder(t, shell, "", string(body), "--from", source, "--prefix", prefix)
		if got.code != 2 {
			t.Fatalf("the script piped into %v exited %d, want 2 (could not check):\n%s", shell, got.code, got.output)
		}
		if !strings.Contains(got.output, "bash") {
			t.Errorf("the refusal did not name bash:\n%s", got.output)
		}
		requireNothingInstalled(t, prefix, map[string]string{})
	})

	t.Run("piped into bash", func(t *testing.T) {
		source := stageSource(t, sourceOptions{})
		prefix := t.TempDir()

		bash, err := exec.LookPath("bash")
		if err != nil {
			t.Skipf("no bash on this host: %v", err)
		}
		got := runUnder(t, []string{bash}, "", string(body), "--from", source, "--prefix", prefix)
		if got.code != 0 {
			t.Fatalf("the script piped into bash exited %d, want 0 — the guard must not break `curl … | bash`:\n%s", got.code, got.output)
		}
		if _, err := os.Stat(filepath.Join(prefix, installedName)); err != nil {
			t.Fatalf("exited 0 but installed nothing: %v\n%s", err, got.output)
		}
	})
}

// TestTheInterpreterGuardIsTheFirstCommandInTheScript pins the guard's
// PLACEMENT, which is load-bearing and which no runtime case on this host can
// observe.
//
// The line below the guard is `set -euo pipefail`, and `set -o pipefail` is not
// POSIX: dash 0.5.12+ accepts it, older dash and busybox ash reject it at
// runtime. On such a host a guard sitting below that line would be pre-empted by
// a failure about `pipefail` — a refusal for the wrong reason, which is the
// exact defect the guard exists to remove, reintroduced one line higher.
//
// This is asserted by inspection rather than by execution deliberately: it is
// true of hosts this suite cannot be run on (the no-bash busybox images the
// script is meant for), and an invariant that only holds where nobody checks it
// is the kind that gets edited away.
func TestTheInterpreterGuardIsTheFirstCommandInTheScript(t *testing.T) {
	body, err := os.ReadFile(installScript(t))
	if err != nil {
		t.Fatalf("reading install.sh: %v", err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed != `if [ -z "${BASH_VERSION:-}" ]; then` {
			t.Fatalf("the first command in install.sh is %q, but it must be the interpreter guard: anything above it runs on a shell that has not yet been established as bash, and `set -o pipefail` on the line below is itself not POSIX", trimmed)
		}
		return
	}
	t.Fatal("install.sh holds no commands at all, which is not the same as the guard being first")
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
// tests/cross/run_cross_build.sh:298-319, which asserts the installed prefix has
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
