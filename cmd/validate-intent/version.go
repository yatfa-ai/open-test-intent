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
// DELIBERATE DIVERGENCE FROM THE REFERENCE
// ========================================
//
// `python3 bin/validate-intent --version` still prints "no file(s) match" and
// still exits 1. That is not a port bug to be mirrored into oblivion, and it is
// not a reference bug to be "fixed" here: it is a Go-only surface, added on
// purpose, and the divergence is written down rather than hidden — see
// tests/parity/run_parity.sh, excluded group 5 in the file header and the
// assertions in section 16 ("Go-side refusals — the excluded surfaces, still
// asserted"). Nothing in the parity corpus invokes `--version`, so no
// comparison changes.
//
// Note also what is NOT here: `--version` is absent from the `usage` block in
// main.go. That text is compared byte-for-byte against the reference's USAGE,
// so documenting a Go-only flag in it would fail section 7 ("--help") and every
// refusal that prints the usage block. The matched edit belongs in
// bin/validate-intent, which this slice does not touch.

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

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
// the normal case — plain `go build`, `go test`, and tests/parity/run_parity.sh
// (which builds without ldflags at run_parity.sh:189) all take it — so the
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
//	validate-intent 1.4.0 (go1.22.12 linux/arm64)
//
// The identity token is the part that must be identical across the four release
// targets; the parenthesised half is the part that must differ.
func VersionLine() string {
	return fmt.Sprintf("%s %s (%s %s/%s)",
		programName, VersionString(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
