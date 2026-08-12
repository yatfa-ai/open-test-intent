package main

// The binary's version identity, and the three tiers it is resolved from.
//
// WHY THIS EXISTS
// ===============
//
// The roadmap ships this port as a static, cross-compiled release asset, and a
// released artifact has to be able to say what it is. Before this file,
// `validate-intent --version` fell through to adopter mode, reported
//
//	error: no file(s) match '--version'
//
// and exited 1 — the code the contract reserves for "at least one annotation is
// malformed". The natural CI preflight
//
//	validate-intent --version || echo "not installed"
//
// therefore reported a *content* failure for a *tool* failure: a malformed
// annotation that does not exist. specguard-rspec/lib/specguard/rspec/cli.rb
// names that shape "false red", and spends its whole exit-2 path avoiding it.
//
// The Go toolchain already stamps `vcs.revision` into every build, but reading
// it needs `go version -m` — i.e. the Go toolchain installed, which is exactly
// the dependency this binary exists to remove from node:alpine / scratch CI
// images. So the binary has to report it itself.
//
// Note what is NOT here: `--version` is absent from the `usage` block in
// main.go, and that is a placement decision rather than an omission — see
// helpTrailer below for where it IS documented, and why it is documented from
// there.

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	opentestintent "github.com/yatfa-ai/open-test-intent"
)

// helpTrailer is the Go-only addendum to the usage block. `--help` prints
// `usage + helpTrailer`; every other path that prints usage — all of them
// refusals — prints `usage` alone.
//
// It documents BOTH Go-only flags that succeed: `--version` (this file) and
// `--schema-source` (schemasource.go). It lives here rather than being split
// across the two files because it is one constant compared as one blob, and a
// trailer assembled from pieces in two places is a trailer whose exact bytes
// nobody can read in one sitting.
//
// WHY THEY ARE DOCUMENTED AT ALL
// ==============================
//
// On the host this binary actually ships to there is no repo, no README and no
// schemas/ directory: a release artifact is a single file at a prefix, and
// `--help` is the entire documentation set. Three consumers already depend on
// this flag — scripts/install.sh runs it as the final proof the artifact
// executes on the target host, README.md documents it, and specguard-rspec's
// identity probe shells out to it to name which implementation produced a run's
// verdicts. A flag that load-bearing being undiscoverable on the only surface
// an adopter has is the same defect SPGD-279 fixed in the last line of `usage`:
// on-host documentation that describes a different program.
//
// WHY IT IS NOT A ROW IN `usage`
// ==============================
//
// `usage` is not printed only by --help: every refusal that cannot name a
// better next step prints it too (`--source` with no FILE, `--json` in
// self-test mode, `--source --json` with no FILE). Those are usage errors on
// the INPUT MODES, and a reader looking at one is being told how to invoke a
// mode — not being offered two flags that report on the binary itself. Keeping
// the trailer out of `usage` is what makes each refusal answer the question it
// was asked.
//
// HOW IT STAYS HONEST
// ===================
//
// version_test.go holds `--help` stdout to `usage + helpTrailer` exactly: the
// trailer is neither stripped nor pattern-matched, because a stripped trailer is
// an unchecked one.
//
// The leading blank line and the trailing newline are part of the constant so
// that `usage + helpTrailer` reads as one block.
//
// WHAT THE SCHEMA SENTENCE IS CAREFUL NOT TO SAY
// ==============================================
//
// It says the digest names the contract this artifact CARRIES, and then says
// what beats it. That hedge is not padding. LoadSchema (fileio.go) gives a
// schemas/open-test-intent.v1.json found beside the executable priority over the
// embedded copy, and `--version` returns above LoadSchema and therefore never
// reaches that decision — it cannot know which copy a later invocation on this
// host will actually enforce. A trailer promising "the schema this binary
// enforces" would be false on precisely the trees where the difference matters,
// which is the same shape of on-host documentation SPGD-279 removed from the
// last line of `usage`.
//
// The hedge STAYS, and it now points somewhere. Slice 19 (SPGD-301) added
// `--schema-source`, which runs the real loader and reports the origin and digest
// of the bytes a run enforces, so the second row is the answer to the first row's
// disclaimer rather than a second way to print the same number. Naming that
// relationship in the trailer is the point of documenting it here at all: on the
// host this ships to, `--help` is where an adopter finds out that the two digests
// can differ and which one to ask for.
const helpTrailer = `
Reporting on the binary itself:

       --version   print this binary's identity (name, version, Go toolchain
                   and target) followed by the SHA-256 of the schema compiled
                   into it, on stdout, and exit 0. The digest names the
                   contract this artifact CARRIES; a schema file found beside
                   the binary wins over the compiled-in copy at validation
                   time, so it is not a claim about what a given run enforced.

       --schema-source
                   print the schema a run on this host ENFORCES — the resolved
                   origin (an absolute path, or <embedded schema>) followed by
                   the SHA-256 of the bytes actually loaded from it — on
                   stdout, and exit 0. Resolved by the same loader the
                   validating modes use, so a digest differing from --version's
                   means a schema beside the binary is winning. Exit 2, with
                   the usual "could not load schema" diagnostic, if that schema
                   exists and cannot be loaded.
`

// programName is what the version line calls this binary. It is a constant
// rather than filepath.Base(os.Args[0]) on purpose: the version line is an
// identity claim, and a renamed or symlinked copy claiming to be something else
// is a worse answer than a fixed, checkable name.
const programName = "validate-intent"

// Version is the release identity stamped in at link time:
//
//	go build -ldflags "-X main.Version=1.4.0" ./cmd/validate-intent
//
// scripts/build-release.sh is the committed invocation that does this for the
// four release targets, and it verifies that the stamp actually landed. That
// verification is not ceremony: `-X` naming a symbol that does not exist is
// silently ignored by the linker, so a typo'd flag produces a clean build of a
// binary that reports something else entirely.
//
// It is a var, and it is deliberately empty by default. An UNSTAMPED build is
// the normal case — plain `go build` and `go test` both take it — so the
// unstamped path has to produce a real answer rather than an empty one. That
// makes the fallback below continuously exercised rather than a one-off claim.
var Version = ""

// versionUnknown is the third tier: the literal used when there is no stamp AND
// no VCS revision to fall back on.
//
// The tier is not hypothetical. debug.ReadBuildInfo's vcs.* settings are absent
// when built with -buildvcs=false, from an extracted tarball or any other
// non-VCS directory, and in container builds with no git available. Two tiers
// would emit "" in every one of those cases — a version line whose version is
// nothing, which reads as "fine" to every consumer that only checks the exit
// code. resolveVersion never returns an empty string.
const versionUnknown = "unknown"

// dirtySuffix marks a binary built from a modified working tree. A dirty build
// reporting a clean revision is its own quiet drift: the revision names bytes
// that are not the bytes that were compiled.
const dirtySuffix = "-dirty"

// resolveVersion picks the identity from the three tiers, in order:
//
//  1. injected  — the semver stamped via -ldflags -X (a release build)
//  2. revision  — the vcs.revision the Go toolchain embeds (a plain build)
//  3. unknown   — the literal, when neither is available
//
// It is a pure function of its inputs so the tiers can be tested without
// three different link invocations; VersionString wires it to the real ones.
//
// It never returns an empty string, and never returns only the dirty marker.
func resolveVersion(injected, revision string, modified bool) string {
	suffix := ""
	if modified {
		suffix = dirtySuffix
	}
	if stamped := strings.TrimSpace(injected); stamped != "" {
		return stamped + suffix
	}
	if rev := strings.TrimSpace(revision); rev != "" {
		return rev + suffix
	}
	// No stamp and no revision. There is nothing to mark as dirty, because
	// there is no claim about a tree to qualify — say so plainly instead of
	// emitting a bare "-dirty" or an empty token.
	return versionUnknown
}

// vcsInfo reads the revision and dirty flag the Go toolchain embeds into every
// build. Both are absent under -buildvcs=false and outside a VCS checkout, in
// which case this reports ("", false) and resolveVersion falls to tier 3.
func vcsInfo() (revision string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return revision, modified
}

// VersionString is the resolved identity alone, with no surrounding prose.
func VersionString() string {
	revision, modified := vcsInfo()
	return resolveVersion(Version, revision, modified)
}

// VersionLine is the single line `--version` writes to stdout, newline
// excluded. The Go toolchain and target are included because this is a
// cross-compiled release artifact and "which build of it is this" is half the
// question anyone asking for --version is actually asking:
//
//	validate-intent 1.4.0 (go1.22.12 linux/arm64) schema sha256:6535d9ba…
//
// The identity token is the part that must be identical across the four release
// targets; the parenthesised half is the part that must differ.
//
// WHY THE SCHEMA DIGEST IS HERE, AND WHY IT IS OUTSIDE THE PARENTHESES
// ====================================================================
//
// It is the other half of "which build of it is this": a linter's answer is
// worth exactly what its contract is, and until now a released artifact could
// not be asked which contract it enforces. Every drift guard in this ecosystem
// compares a matched pair inside ONE checkout, and an installed binary has no
// checkout around it — tests/cross/run_cross_build.sh asserts there is no
// schemas/ beside it. See opentestintent.SchemaSHA256 for the full account of
// the gap this closes.
//
// Outside the parentheses because the group is documented above as the part
// that must DIFFER across the four release targets, and the schema digest is the
// part that must be IDENTICAL across all four: one contract, cross-compiled four
// ways. Putting it inside would file it under the wrong claim.
//
// Appended rather than inserted, because the version token's position is load
// bearing. scripts/install.sh strips the `validate-intent ` prefix and takes
// everything up to the first space, and scripts/build-release.sh matches
// `validate-intent <version> ` as a prefix — both survive a token added at the
// end, neither survives one added in the middle. install.sh is verified against
// this line rather than edited for it.
//
// One line, no control characters, and short enough to render: specguard-rspec's
// identity probe (validator_backend.rb) drops any `--version` output that is not
// a single renderable line under IDENTITY_MAX_BYTES = 200. The longest this can
// produce is an unstamped dirty build — a 40-hex revision plus `-dirty` — which
// lands near 165 bytes.
func VersionLine() string {
	return fmt.Sprintf("%s %s (%s %s/%s) schema sha256:%s",
		programName, VersionString(), runtime.Version(), runtime.GOOS, runtime.GOARCH,
		opentestintent.SchemaSHA256())
}
