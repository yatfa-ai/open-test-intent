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
# to match. Artifacts, and a SHA256SUMS manifest describing them, land in
# dist/release/.
#
# Not dist/ itself, which is where tests/cross/run_cross_build.sh puts the
# UNSTAMPED builds it verifies — and under exactly the same filenames. Sharing
# the directory would let a cross-build run silently replace a release artifact
# with a byte-different one reporting a different version, which is the quiet
# drift this script exists to prevent. One /dist/ entry in .gitignore covers
# both trees.
#
# The same drift, turned inward: dist/release/ holds THIS run and nothing else
# ------------------------------------------------------------------------------
# The paragraph above closes the collision against the OTHER producer. The
# likelier collision is this script's own previous run — same four filenames,
# same directory, and the survivors are stale rather than merely unstamped, so
# they are PLAUSIBLE, which is worse. Building in place meant a run that died
# partway left the previous run's artifacts standing for the targets it never
# reached: four files, the four canonical names, two semvers, and nothing on
# disk to say which run wrote which. That reads exactly like a finished release.
#
# So the invariant is: everything is built in a staging directory and promoted
# into $DIST in a single rename, only after every target has been built and
# every check below has passed. dist/release/ therefore contains a complete,
# fully verified set or is left exactly as it was — never a half of one. "The
# artifacts exist" and "the release passed" become the same statement, which is
# the property the rest of this header already asserts in prose.
#
# Promotion REPLACES $DIST rather than writing into it, which closes the second
# shape of the same defect: a target dropped or renamed in TARGETS used to leave
# its artifact behind to be published as current by every later run, under a
# green summary line that counted only what that run built.
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
# Why the run also leaves a SHA256SUMS manifest behind
# ----------------------------------------------------
# Every check above runs on the BUILD HOST and leaves nothing behind. The
# artifacts then travel — over a network, to a machine that never saw this run —
# and arrive carrying no evidence any of it happened. Worse, of the three checks
# only check 3 is decisive, and it can run for the native target alone; for the
# other three artifacts the strongest identity evidence is check 1, which says
# of itself that it "can pass without verifying anything".
#
# So the run records what it actually produced: a SHA256SUMS manifest listing
# the basename and SHA-256 of each artifact, in the standard coreutils/shasum
# format, so an adopter verifies a download with `sha256sum -c SHA256SUMS` or
# `shasum -a 256 -c SHA256SUMS` — their own tool, no knowledge of this
# repository required. This is the ruling schema_test.go already made about
# invisible build-time snapshots ("nothing about reading the resulting binary
# says which bytes went in", remedied with a pinned digest), applied one level
# up to an artifact that is the same shape and strictly more exposed. It also
# finally observes the reproducibility the -trimpath below pays for: the same
# source at the same commit now produces a manifest anyone can compare.
#
# It is an INTEGRITY and IDENTITY record, not a signature. Generated by the same
# run that built the artifacts, it does not defend against an attacker who can
# replace both. Signing, attestation and provenance are a different mechanism
# and are not claimed here.
#
# Three properties of how it is done, each load-bearing:
#
#   * it is written INTO STAGING and verified there, BEFORE promotion. A
#     manifest written into $DIST after the rename could describe a set that is
#     no longer there — the same half-a-release defect staging exists to prevent
#     — and promotion replaces $DIST wholesale, so a row for a retired target
#     cannot survive into the next run either;
#   * it is VERIFIED, by re-reading every staged file from disk and comparing.
#     A manifest generated and never read again is precisely the "step that
#     reports success having verified nothing" this header opened with, and it
#     would pass just as happily over an artifact corrupted the instant after it
#     was digested. tests/cross/sha256sums/main_test.go corrupts a file in
#     exactly that window and requires the check to fail;
#   * the digests come from Go's crypto/sha256, via tests/cross/sha256sums,
#     built for the host up front exactly like the inspector — NOT from
#     `sha256sum`. That is GNU coreutils and is absent on a stock macOS host,
#     which is one of only two host families this script can run on at all (the
#     gate below requires the host to match a target, and two of the four are
#     darwin). Reaching for it would have reproduced the `ldd` failure documented
#     above, verbatim: a tool that cannot speak the platform, a shell that reads
#     its refusal as a pass, half the matrix unchecked. Probing for
#     sha256sum/shasum/openssl in order was the other honest option; crypto/sha256
#     was chosen because it is portable BY CONSTRUCTION — there is no host to
#     probe and so no probe to get wrong — and because a Go toolchain is already
#     a hard prerequisite here. The EMITTED format is still the portable one, so
#     the build host's problem is not exported to the adopter.
#
# If the manifest tool cannot be built, or the manifest cannot be verified, this
# script STOPS, for the same reason it stops without the inspector. "Could not
# digest" is not a pass.
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
SUMS_PACKAGE="./tests/cross/sha256sums"

# The conventional name, because the point of the file is that a consumer who
# has never seen this repository knows what to do with it.
MANIFEST_NAME="SHA256SUMS"

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

# --- where this run builds, and when it becomes the release ----------------- #
#
# See the header. Artifacts are staged and promoted in one move at the end, so
# $DIST is never a partial or mixed-version set.
#
# Promoting means DELETING $DIST, so its value is checked before anything else
# uses it. It is env-overridable and this is the one destructive thing here: a
# path that is not a directory this build owns must be refused rather than
# emptied. The parent is canonicalised first so a relative or dot-suffixed
# override cannot slip past the comparisons.
dist_base="$(basename "$DIST")"
case "$dist_base" in
  ""|"."|".."|"/") die "refusing '$DIST' as the artifact directory: it has no directory
       name of its own to replace, and promoting a release deletes it." ;;
esac
mkdir -p "$(dirname "$DIST")" || die "could not create the parent of $DIST"
dist_parent="$(cd "$(dirname "$DIST")" && pwd -P)" || die "could not resolve the parent of $DIST"
DIST="$dist_parent/$dist_base"
if [ "$DIST" = "$REPO_ROOT" ] || [ "$DIST" = "${HOME:-}" ] || [ "$DIST" = "/" ]; then
  die "refusing '$DIST' as the artifact directory: promoting a release DELETES it,
       so it must be a directory owned by the build, not the repo or a home."
fi

# A sibling of $DIST rather than mktemp's default, so promotion is a
# same-filesystem rename — one atomic swap instead of a four-file copy that
# could itself fail halfway and leave the exact state this staging is here to
# prevent. It lands under the same /dist/ .gitignore entry as its destination.
STAGE="$(mktemp -d "$dist_parent/.build-release.XXXXXX")" \
  || die "could not create a staging directory beside $DIST"

# The inspector and the manifest tool are built for the HOST, up front, because
# they are tools this script runs rather than artifacts it ships. Building them
# before the loop means a compile error in either surfaces once, as "could not
# check", instead of four times as an apparent per-target failure.
#
# Into their own temp dir, kept out of BOTH $DIST and the staging dir:
# everything staged is promoted verbatim, so a host-only helper sitting among
# four cross-compiled binaries under a similar name is an invitation to publish
# it.
INSPECT_DIR="$(mktemp -d)"

cleanup() {
  # The promotion below clears $STAGE once that directory has BECOME $DIST, so a
  # successful run has nothing to remove here, and a failed one discards its
  # half-built set rather than leaving it to be mistaken for a release.
  if [ -n "${STAGE:-}" ]; then rm -rf "$STAGE"; fi
  if [ -n "${INSPECT_DIR:-}" ]; then rm -rf "$INSPECT_DIR"; fi
  return 0
}
trap cleanup EXIT

INSPECT="$INSPECT_DIR/inspect-artifact"
(cd "$REPO_ROOT" && "$GO" build -o "$INSPECT" "$INSPECTOR_PACKAGE") \
  || die "could not build $INSPECTOR_PACKAGE.
       Without it no artifact can be verified as the target it claims to be,
       so nothing below would be checked. Refusing to build a release on that."

# Built here rather than reached for on PATH — see the header. `sha256sum` is
# GNU coreutils and a stock macOS build host does not have it, and this script's
# own history says what happens next when a host tool cannot speak the platform.
SUMS="$INSPECT_DIR/sha256sums"
(cd "$REPO_ROOT" && "$GO" build -o "$SUMS" "$SUMS_PACKAGE") \
  || die "could not build $SUMS_PACKAGE.
       Without it the artifacts would be promoted with no record of what they
       are, and no way for anyone downstream to tell. 'Could not digest' is not
       a pass."

HOST_OS="$("$GO" env GOHOSTOS)"
HOST_ARCH="$("$GO" env GOHOSTARCH)"
native_verified=0
built=0
staged=()

for target in "${TARGETS[@]}"; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  out="$STAGE/validate-intent-${goos}-${goarch}"

  dim "building $goos/$goarch ..."
  # -trimpath keeps absolute build paths out of the artifact, so the same
  # source at the same commit produces the same bytes from any checkout
  # directory.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    "$GO" build -trimpath -ldflags "-X main.Version=$VERSION" -o "$out" "$PACKAGE" \
    || die "build failed for $target"

  [ -s "$out" ] || die "$out was not produced, or is empty"
  built=$((built + 1))
  staged+=("$out")

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

  # The basename, not $out: the artifact is still in staging and is not the
  # release until the promotion below, so naming either path here would claim
  # something that is not true yet.
  green "  ok    validate-intent-${goos}-${goarch}"
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

# --- the manifest: what this run produced, recorded so it can leave the host - #
#
# See the header. Written into STAGING and verified there, because a manifest
# written into $DIST after the promotion below could describe a set that is no
# longer there, and because the promotion REPLACES $DIST — which is what keeps a
# row for a retired target from surviving into the next run.
#
# The operand list is the artifacts this run actually built, not a glob of the
# staging directory: the manifest should describe the set the loop verified, and
# saying so explicitly means it cannot quietly grow a row for something else that
# appeared there. (The verification below catches that from the other side.)
MANIFEST="$STAGE/$MANIFEST_NAME"
"$SUMS" -o "$MANIFEST" "${staged[@]}" \
  || die "could not write $MANIFEST_NAME for the staged artifacts.
       They are built and verified, but nothing downstream could tell what they
       are. 'Could not digest' is not a pass."

# Verified by RE-READING every staged file, which is the only thing that makes
# the step above worth doing: a manifest generated and never read again reports
# success having verified nothing. The exit codes are kept apart for the same
# reason the inspector's are — "found wrong" and "could not examine" both stop
# the release, but they stop it saying different things.
sums_rc=0
"$SUMS" -c "$MANIFEST" || sums_rc=$?
case "$sums_rc" in
  0) ;;
  1) die "the staged artifacts do not match the $MANIFEST_NAME just written for them
       (see above). Something changed them between digesting and verifying, or
       the staged set is not the set that was built." ;;
  *) die "could not verify $MANIFEST_NAME (see above).
       The artifacts may be perfectly correct; nothing here could tell.
       That is not a failure and it is certainly not a release." ;;
esac
green "  ok    $MANIFEST_NAME ($built entries, re-read and matching)"

# --- promote: the staged set becomes the release, in one move --------------- #
#
# Only reached when every target built and every check above passed, which is
# what makes "$DIST exists" mean "the release passed". $DIST is REPLACED, not
# written into, so an artifact from a target no longer in TARGETS is retired
# here rather than left behind to be published by every later run.
chmod 755 "$STAGE" || die "could not set permissions on the staged artifacts"
rm -rf "$DIST" || die "could not clear $DIST to promote this run's artifacts into it"
if ! mv "$STAGE" "$DIST"; then
  # Deliberately NOT cleaned up: these artifacts are built and fully verified,
  # and the only thing that failed is putting them where they belong. Deleting
  # them to keep the temp tree tidy would throw away the whole run.
  staged="$STAGE"
  STAGE=""
  die "built and verified $built artifacts, but could not move them into $DIST.
       They have been left in $staged."
fi
STAGE=""   # promoted — it IS $DIST now, and the EXIT trap must not remove it

echo
green "$built artifacts in $DIST, all reporting ${VERSION}${DIRTY}"
dim "verify a downloaded artifact against $MANIFEST_NAME with your own platform's tool:"
dim "  cd <dir holding both> && sha256sum -c $MANIFEST_NAME   # or: shasum -a 256 -c $MANIFEST_NAME"
ls -l "$DIST"
