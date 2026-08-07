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
# to match. Artifacts land in dist/.
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
#   * every artifact is checked to be statically linked, because "static" is
#     half of what the DoD asks for and a cgo-enabled build would still pass
#     the two checks above.
#
# A failure in any of these fails the whole script. There is no "warn and
# continue" tier — a release you were warned about is a release you shipped.
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
DIST="${DIST:-$REPO_ROOT/dist}"

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

  # --- check 2: statically linked ----------------------------------------- #
  #
  # CGO_ENABLED=0 is set above, but asserting the property beats trusting the
  # flag: an import that pulls in cgo, or an inherited CGO_ENABLED from the
  # environment, changes the answer without changing this file.
  #
  # `ldd` exits non-zero on a static binary and on a Mach-O one it cannot read
  # at all, so its exit code says nothing useful. What is diagnostic is a
  # resolved shared-library line ("libc.so.6 => /lib/..."), and its absence is
  # the assertion.
  #
  # Captured into a variable rather than piped into grep, because this script
  # runs under `set -o pipefail`: `ldd x | grep -q '=>'` takes ldd's exit code,
  # so a dynamically-linked binary whose ldd happened to exit non-zero would
  # read as static and pass.
  if command -v ldd >/dev/null 2>&1; then
    ldd_output="$(ldd "$out" 2>&1 || true)"
    if printf '%s\n' "$ldd_output" | grep -q '=>'; then
      red "  ldd $out:"
      printf '%s\n' "$ldd_output" | sed 's/^/    /'
      die "$out is dynamically linked — it will not run on scratch/alpine"
    fi
  fi

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
