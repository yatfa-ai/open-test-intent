#!/usr/bin/env bash
#
# Build the release artifacts, with the version stamped in.
# =========================================================
#
#   scripts/build-release.sh 1.4.0
#   VERSION=1.4.0 scripts/build-release.sh
#
# Cross-compiles cmd/validate-intent for the four targets the roadmap's DoD
# names, with CGO_ENABLED=0 so each is a static binary that runs on
# node:alpine, scratch, distroless — anywhere, with no Go toolchain and no libc
# to match. Artifacts land in dist/release/.
#
# Not dist/ itself, which is where tests/cross/run_cross_build.sh puts the
# UNSTAMPED builds it verifies — and under exactly the same filenames. Sharing
# the directory would let a cross-build run silently replace a release artifact
# with a byte-different one reporting a different version, which is the quiet
# drift this script exists to prevent. One /dist/ entry in .gitignore covers
# both trees.
#
# This is the SOURCE half of releasing, deliberately separated from publishing.
# It is not a CI workflow and it does not upload anything: a pipeline that
# publishes artifacts which cannot identify themselves is the wrong order, so
# the stamping seam lands first and the publishing step consumes it.
#
#
# What "the stamping seam" means, and why this script verifies itself
# -------------------------------------------------------------------
# The seam is `-ldflags -X main.Version=<semver>` writing the Version var in
# cmd/validate-intent/version.go. That mechanism has one nasty property:
#
#     the linker SILENTLY IGNORES -X when the symbol path does not exist.
#
# Rename the var, move it to another package, mistype the flag, and the build
# still succeeds — it just produces a binary that reports something else. The
# failure is invisible at build time and only shows up as a released artifact
# lying about its own version, which is the exact class of defect this project
# keeps having to name: a step that reports success having verified nothing.
#
# So every artifact is checked after it is built:
#
#   * the native one is RUN — `--version` must exit 0 and report the requested
#     semver. That is the only check that proves the flag reached the var;
#   * every artifact is scanned for the literal version bytes. This is a
#     per-target corroborator of the check above, NOT an independent proof of
#     it — a version that is a substring of something the toolchain already
#     embeds passes it either way. See the comment on check 1;
#   * every artifact is handed to tests/cross/inspect-artifact, which reads the
#     ELF/Mach-O headers and asserts it really is the OS and architecture it was
#     asked to be, and really is free of a runtime the target machine would have
#     to supply.
#
# A failure in any of these fails the whole script. There is no "warn and
# continue" tier — a release you were warned about is a release you shipped.
#
#
# Why the linkage check is delegated rather than done here with `ldd`
# -------------------------------------------------------------------
# It used to be `ldd`, grepping its output for a resolved shared-library line.
# That check was real on the two linux targets and VACUOUS on the two darwin
# ones: `ldd` cannot read a Mach-O file at all, so it printed a refusal, the
# grep for `=>` found nothing in it, and the absence of evidence was scored as
# evidence of absence. Half the release matrix passed a check that had not run
# — this project's house defect, inside the script whose own header paragraph
# is about exactly that.
#
# tests/cross/inspect-artifact (SPGD-171) is the checker without that gap:
# debug/elf and debug/macho rather than a shell tool that only speaks one of
# them, an emptiness assertion on linux and an OS-baseline dylib allowlist on
# darwin, plus the OS/arch identity this script never checked at all. A release
# script reaching into tests/ is deliberate: the alternative is a second,
# weaker copy of it under scripts/, and two implementations of "is this
# artifact what we think it is" is one more than can be kept honest.
#
# If the inspector cannot be built, this script STOPS rather than falling back.
# "Could not check" is not a pass, and a release is the last place to start
# making that trade.
#
#
# The UNSTAMPED build is not a bug
# --------------------------------
# `go build ./cmd/validate-intent` with no ldflags is the normal developer path
# and is what tests/parity/run_parity.sh does. Such a binary reports the
# vcs.revision the Go toolchain embeds, and a binary built with -buildvcs=false
# reports the literal `unknown`. Both are real answers; neither is empty. See
# resolveVersion in cmd/validate-intent/version.go. Because the parity harness
# takes the unstamped path on every run, that fallback is exercised
# continuously rather than asserted once here.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
GO="${GO:-go}"
PACKAGE="./cmd/validate-intent"
DIST="${DIST:-$REPO_ROOT/dist/release}"
INSPECTOR_PACKAGE="./tests/cross/inspect-artifact"

# The four targets named by the roadmap's DoD. Adding a fifth here is all it
# takes; every check below is per-target.
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
dim()   { printf '\033[2m%s\033[0m\n' "$*"; }

die() { red "error: $*"; exit 1; }

VERSION="${1:-${VERSION:-}}"
if [ -z "$VERSION" ]; then
  # Refused rather than defaulted. A release build that quietly stamps "dev",
  # today's date, or nothing at all produces artifacts that are indistinguishable
  # from each other after the fact, which defeats the point of stamping.
  red "error: no version given"
  cat >&2 <<'USAGE'

usage: scripts/build-release.sh <semver>
       VERSION=<semver> scripts/build-release.sh

  e.g. scripts/build-release.sh 1.4.0

This script stamps a release identity into the binary, so it needs one. It is
deliberately not defaulted: an unstamped build is a legitimate thing to want,
and the way to ask for it is the plain toolchain command, which reports the
embedded VCS revision instead:

       go build -o bin/validate-intent-go ./cmd/validate-intent
USAGE
  exit 2
fi

case "$VERSION" in
  *[[:space:]]*) die "version '$VERSION' contains whitespace" ;;
esac

command -v "$GO" >/dev/null 2>&1 || die "no Go toolchain on PATH (set GO=/path/to/go)"

# A dirty tree is stamped honestly rather than refused: version.go appends
# `-dirty` when the toolchain reports vcs.modified, so the artifact says what it
# is. It is called out here because it changes what the checks below expect, and
# because a release built from uncommitted changes cannot be reproduced from the
# tag it claims.
DIRTY=""
if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  if [ -n "$(git -C "$REPO_ROOT" status --porcelain)" ]; then
    DIRTY="-dirty"
    red "warning: the working tree is dirty — artifacts will report ${VERSION}${DIRTY}"
  fi
fi

mkdir -p "$DIST"

# The inspector is built for the HOST, up front, because it is a tool this
# script runs rather than an artifact it ships. Building it before the loop
# means a compile error in it surfaces once, as "could not check", instead of
# four times as an apparent per-target failure.
#
# Into a temp dir rather than $DIST: everything in $DIST is a release artifact,
# and a host-only helper sitting among four cross-compiled binaries under a
# similar name is an invitation to publish it.
INSPECT_DIR="$(mktemp -d)"
trap 'rm -rf "$INSPECT_DIR"' EXIT
INSPECT="$INSPECT_DIR/inspect-artifact"
(cd "$REPO_ROOT" && "$GO" build -o "$INSPECT" "$INSPECTOR_PACKAGE") \
  || die "could not build $INSPECTOR_PACKAGE.
       Without it no artifact can be verified as the target it claims to be,
       so nothing below would be checked. Refusing to build a release on that."

HOST_OS="$("$GO" env GOHOSTOS)"
HOST_ARCH="$("$GO" env GOHOSTARCH)"
native_verified=0
built=0

for target in "${TARGETS[@]}"; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  out="$DIST/validate-intent-${goos}-${goarch}"

  dim "building $goos/$goarch ..."
  # -trimpath keeps absolute build paths out of the artifact, so the same
  # source at the same commit produces the same bytes from any checkout
  # directory.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    "$GO" build -trimpath -ldflags "-X main.Version=$VERSION" -o "$out" "$PACKAGE" \
    || die "build failed for $target"

  [ -s "$out" ] || die "$out was not produced, or is empty"
  built=$((built + 1))

  # --- check 1: the stamp is physically in the artifact ------------------- #
  #
  # A per-target CORROBORATOR of check 3, not an independent proof of it. The
  # distinction matters, so it is stated rather than implied: this check can
  # pass without verifying anything.
  #
  # `grep -F "$VERSION"` asks whether some bytes appear, not whether THESE
  # bytes came from the stamp. Any version that is a substring of something the
  # toolchain already embeds matches whether or not `-X` landed --
  # `scripts/build-release.sh 1.22.12` matches the `go1.22.12` build string in
  # all four artifacts, and a semver that happens to sit inside the vcs.revision
  # hex would do the same. A false PASS, which is the direction this repo cares
  # about.
  #
  # It is kept anyway, for the case it does cover: check 3 can only run the
  # artifact whose target matches the host, and a per-target `-X` failure that
  # somehow spared the native build would show up here and nowhere else. When it
  # is not defeated by a collision it is also stronger than it first looks,
  # thanks to dead-code elimination -- a Version var that is stamped but never
  # READ is dropped from the binary entirely, so its value never appears here
  # either.
  #
  # What would make it independent: build each target a second time WITHOUT the
  # ldflag and require the version bytes to be absent from that control. That is
  # a real differential and it doubles the build. It is not done because `-X
  # main.Version=` either resolves the symbol or does not, uniformly across
  # targets -- so check 3, which RUNS the native artifact, already settles the
  # question this one approximates. Read a failure here as real and a pass here
  # as agreement, not as proof.
  if ! grep -qF -- "$VERSION" "$out"; then
    die "$out does not contain the string '$VERSION' — the -X stamp did not land.
       -X silently does nothing when the symbol path is wrong; check that
       cmd/validate-intent/version.go still declares 'var Version' in package main."
  fi

  # --- check 2: it is the target it claims, and needs no runtime ----------- #
  #
  # CGO_ENABLED=0 and GOOS/GOARCH are set above, but asserting the properties
  # beats trusting the flags: an import that pulls in cgo, or an inherited
  # CGO_ENABLED from the environment, changes the answer without changing this
  # file — and a check whose only evidence is a playback of its own env vars
  # could not have caught that anyway.
  #
  # Delegated to tests/cross/inspect-artifact, which reads the ELF/Mach-O
  # headers. See the header of this file for why this is not `ldd`: `ldd`
  # cannot read Mach-O, so it scored both darwin targets as passing a check it
  # had not performed.
  #
  # The inspector's exit codes are kept apart on purpose — it distinguishes
  # "examined and wrong" (1) from "could not examine" (2), and collapsing them
  # with `if !` would report an UNCHECKED artifact as a checked-and-wrong one.
  # Both stop the release here, but they stop it saying different things.
  inspect_rc=0
  "$INSPECT" -os "$goos" -arch "$goarch" "$out" || inspect_rc=$?
  case "$inspect_rc" in
    0) ;;
    1) die "$out is not the artifact it was asked to be (see above)" ;;
    *) die "could not examine $out (see above).
       The artifact may be perfectly correct; the inspector could not tell.
       That is not a failure and it is certainly not a release." ;;
  esac

  # --- check 3: the native artifact actually reports it -------------------- #
  #
  # The only check here that proves the flag reached the var rather than merely
  # reaching the binary. It can only run for one target, which is exactly why
  # checks 1 and 2 exist for the other three.
  if [ "$goos" = "$HOST_OS" ] && [ "$goarch" = "$HOST_ARCH" ]; then
    reported="$("$out" --version)" || die "$out --version exited non-zero"
    want="validate-intent ${VERSION}${DIRTY} "
    case "$reported" in
      "$want"*) ;;
      *) die "$out --version reported:
         $reported
       expected it to start with:
         $want" ;;
    esac
    dim "  verified by running it: $reported"
    native_verified=1
  fi

  green "  ok    $out"
done

# The premise of the whole run, asserted rather than assumed. If no target
# matched the host — a plausible future edit to TARGETS, or a run on a host
# architecture nobody added — then check 3 never ran, and the strongest evidence
# that stamping works was silently skipped while every artifact still printed
# "ok". That is a green report from a run that verified less than it looks like.
if [ "$native_verified" -ne 1 ]; then
  die "no target matched this host ($HOST_OS/$HOST_ARCH), so no artifact could be
       RUN to confirm the version stamp actually applied. Add the host's target
       to TARGETS, or run this on a host one of them matches."
fi

echo
green "$built artifacts in $DIST, all reporting ${VERSION}${DIRTY}"
ls -l "$DIST"
