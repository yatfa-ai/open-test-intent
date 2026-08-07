#!/usr/bin/env bash
#
# Cross-build harness: the four release targets, built and then verified.
# ======================================================================
#
#   tests/cross/run_cross_build.sh
#
# Builds ./cmd/validate-intent for each of the roadmap's four release targets
#
#     linux/amd64   linux/arm64   darwin/amd64   darwin/arm64
#
# with CGO_ENABLED=0, then makes three separate claims about what came out:
#
#   1. Each artifact really is the OS and architecture it was asked to be, and
#      really is free of a runtime the target machine would have to supply.
#      Checked by tests/cross/inspect-artifact, which reads the ELF/Mach-O
#      headers rather than the toolchain's playback of our own env vars — see
#      that file's package comment for why the obvious check is circular, and
#      for why "statically linked" has to mean something different on darwin
#      than on linux.
#
#   2. The artifact for THIS host, run from an installed layout outside
#      <repo>/bin/, still validates correctly — exit 0 on a good fixture, exit
#      1 on each bad one — because it falls through to the embedded schema.
#
#   3. That fallthrough is a fallback and not dead code: the same artifact,
#      placed in a prefix that DOES have an on-disk schema, still prefers it.
#
#
# Why this harness exists at all
# ------------------------------
# The roadmap's Definition of Done asks for these four targets. Until this
# script, GOOS, GOARCH and CGO_ENABLED appeared nowhere in the repository:
# every build site was a bare host build, there was no build script and no
# release tooling, and the binary was a gitignored local artifact. The four
# builds had never been run once, and nothing here would have noticed if one
# of them broke.
#
# That is the whole claim. It is NOT claimed that any of the four was broken —
# as it happens they all build clean. The deliverable is the regression guard,
# plus claim 2, which is coverage the repo genuinely did not have.
#
#
# Why claim 2 needs claim 3 standing behind it
# --------------------------------------------
# cmd/validate-intent/fileio.go derives its schema path from os.Executable():
# the repo root is the parent of the directory holding the binary. Installed at
# /usr/local/bin/validate-intent that resolves to
# /usr/local/schemas/open-test-intent.v1.json, which does not exist — the exact
# failure schema.go's embedded copy was added to survive, and which schema.go's
# own header names as having happened.
#
# The existing Go test for this (cmd/validate-intent/fileio_schema_test.go)
# SKIPS when os.Executable() is unavailable, so it cannot be the thing that
# proves the installed layout works. Nothing did, through a real binary, until
# this script.
#
# But "binary in a prefix with no schema still exits 0" would be a weak
# assertion on its own. It passes just as happily if the executable-relative
# lookup were deleted tomorrow and the embedded copy became the only path —
# at which point the harness would still be green while silently no longer
# testing a fallback. So the prefix that HAS a schema is checked too, with a
# deliberately malformed one, and is required to exit 2 quoting the path it
# derived. That failure is only reachable if the lookup ran, so it is what
# makes the passing case mean what it says.
#
#
# Refusing honestly
# -----------------
# No Go toolchain, or a toolchain that cannot build the inspector, is "could
# not check" and exits 2 — never a pass. Same convention as
# tests/parity/run_parity.sh, which exits 2 rather than skipping when its build
# fails, and for the same reason: a build checker that silently skipped would
# be a textbook instance of the vacuous green this project keeps naming.
#
# The same rule governs claim 2. It can only run on an artifact this host can
# execute, so if GOHOSTOS/GOHOSTARCH is not one of the four targets, the
# installed-layout claim was NOT checked and the run exits 2 saying so, rather
# than reporting a clean sweep of the parts it could reach.
#
#
# What this deliberately does not do
# ----------------------------------
# It does not publish. No .github/workflows/, no version tagging, no upload of
# release assets — publishing is a human/CI decision, and .agents/README.md
# records why workflow files are human-owned in this repo. This script makes
# the artifacts reproducible and verified; someone else decides to ship them.
#
# Windows is not built. It is not a roadmap target, notwithstanding .gitignore
# mentioning .exe.
#
#
# Usage
# -----
#     tests/cross/run_cross_build.sh
#
#     GO=/path/to/go        override the toolchain
#     DIST=/path/to/dir     override where artifacts land (default: <repo>/dist)
#
# Exit 0 = every target built and every claim held.
# Exit 1 = something was checked and was wrong.
# Exit 2 = something could not be checked. Not a pass.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
GO="${GO:-go}"
DIST="${DIST:-$REPO_ROOT/dist}"

# The roadmap's four. Order is fixed so the report reads the same every run.
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

# Fixtures for the installed-layout smoke test. These already exist and are the
# same ones the reference implementation's own self-test uses.
GOOD_FIXTURE="examples/unit-order-total.json"
BAD_FIXTURES=(
  "examples/invalid/bad-layer.json"
  "examples/invalid/missing-required.json"
  "examples/invalid/short-behavior.json"
  "examples/invalid/typo-extra-property.json"
)

WORK="$(mktemp -d)"
trap 'chmod -R u+rwX "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
dim()   { printf '\033[2m%s\033[0m\n' "$*"; }

ok()   { passed=$((passed + 1)); green "  PASS  $*"; }
bad()  { failed=$((failed + 1)); red   "  FAIL  $*"; }

# --------------------------------------------------------------------------- #
# preflight: the toolchain
# --------------------------------------------------------------------------- #
if ! command -v "$GO" >/dev/null 2>&1; then
  red "error: no Go toolchain on PATH (set GO=/path/to/go)"
  red "       tests/cross/run_cross_build.sh verified NOTHING on this run."
  red "       Four release targets are unbuilt and unchecked; this is not a pass."
  exit 2
fi

dim "toolchain: $("$GO" version)"

# The inspector is built for the HOST, because it is a tool this harness runs,
# not an artifact it ships. Building it up front means a compile error in it
# surfaces as "could not check" rather than as four identical target failures.
INSPECT="$WORK/inspect-artifact"
if ! (cd "$REPO_ROOT" && "$GO" build -o "$INSPECT" ./tests/cross/inspect-artifact); then
  red "error: could not build tests/cross/inspect-artifact"
  red "       Without it no artifact can be verified, so nothing was checked."
  exit 2
fi

mkdir -p "$DIST" || { red "error: could not create $DIST"; exit 2; }

# --------------------------------------------------------------------------- #
# build and verify each target
# --------------------------------------------------------------------------- #
echo
dim "building four targets into $DIST ..."
echo

built_host_artifact=""
HOST_OS="$("$GO" env GOHOSTOS)"
HOST_ARCH="$("$GO" env GOHOSTARCH)"

for target in "${TARGETS[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  artifact="$DIST/validate-intent-$goos-$goarch"

  if ! (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        "$GO" build -trimpath -o "$artifact" ./cmd/validate-intent) 2>"$WORK/build.err"; then
    bad "$target — build failed"
    sed 's/^/        /' "$WORK/build.err" >&2
    continue
  fi

  if ! "$INSPECT" -os "$goos" -arch "$goarch" "$artifact"; then
    bad "$target — artifact is not what was asked for (see above)"
    continue
  fi

  ok "$target — built and verified: $(basename "$artifact")"
  if [ "$goos" = "$HOST_OS" ] && [ "$goarch" = "$HOST_ARCH" ]; then
    built_host_artifact="$artifact"
  fi
done

# --------------------------------------------------------------------------- #
# the installed layout: embedded-schema fallback, through a real binary
# --------------------------------------------------------------------------- #
#
# Everything below runs the host artifact from a prefix in $WORK — never from
# <repo>/bin/ and never from <repo>/dist/. Both of those would put a real
# schemas/ directory exactly where os.Executable() derivation looks, so the
# on-disk schema would be found and the fallback would never be exercised.
# That distinction is the entire point of this section, so the prefixes are
# built deliberately rather than reusing anything in the repo.
echo
if [ -z "$built_host_artifact" ]; then
  red "error: no artifact runnable on this host ($HOST_OS/$HOST_ARCH is not one"
  red "       of the four targets, or its build failed above)."
  red "       The installed-layout claim was NOT checked. Reporting the target"
  red "       builds alone as a pass would be exactly the vacuous green this"
  red "       harness exists to prevent."
  exit 2
fi

dim "installed-layout smoke test using $(basename "$built_host_artifact") ..."
echo

# --- prefix A: no schema on disk, so the embedded copy has to carry it ------ #
PREFIX_A="$WORK/prefix-embedded"
mkdir -p "$PREFIX_A/bin"
cp "$built_host_artifact" "$PREFIX_A/bin/validate-intent"

# Assert the absence rather than assuming it. If a schemas/ directory somehow
# existed here, every assertion below would pass for the wrong reason and this
# section would silently stop testing the embedded copy.
if [ -e "$PREFIX_A/schemas" ]; then
  red "error: $PREFIX_A/schemas exists; the embedded-fallback test would be void"
  exit 2
fi
ok "installed prefix has no schemas/ on disk (so a pass below means the embedded copy was used)"

# cwd is $WORK, i.e. outside the repo entirely, and fixtures are passed as
# absolute paths — so nothing here can be resolved relative to the checkout.
run_installed() {
  (cd "$WORK" && "$PREFIX_A/bin/validate-intent" "$@" >"$WORK/out" 2>"$WORK/err")
  echo $?
}

rc="$(run_installed "$REPO_ROOT/$GOOD_FIXTURE")"
if [ "$rc" = "0" ]; then
  ok "installed layout: $GOOD_FIXTURE exits 0"
else
  bad "installed layout: $GOOD_FIXTURE exits $rc, want 0"
  sed 's/^/        /' "$WORK/err" >&2
fi

for fixture in "${BAD_FIXTURES[@]}"; do
  rc="$(run_installed "$REPO_ROOT/$fixture")"
  if [ "$rc" = "1" ]; then
    ok "installed layout: $fixture exits 1"
  else
    bad "installed layout: $fixture exits $rc, want 1"
    sed 's/^/        /' "$WORK/err" >&2
  fi
done

# --- prefix B: the control that keeps prefix A meaningful ------------------- #
#
# See "Why claim 2 needs claim 3 standing behind it" in the header. A prefix
# WITH an on-disk schema must still prefer it, which is only observable if the
# executable-relative lookup actually runs. A deliberately malformed schema
# makes that observable as exit 2 quoting the derived path.
PREFIX_B="$WORK/prefix-ondisk"
mkdir -p "$PREFIX_B/bin" "$PREFIX_B/schemas"
cp "$built_host_artifact" "$PREFIX_B/bin/validate-intent"
printf '{ this is not valid json' > "$PREFIX_B/schemas/open-test-intent.v1.json"

(cd "$WORK" && "$PREFIX_B/bin/validate-intent" "$REPO_ROOT/$GOOD_FIXTURE" >"$WORK/out" 2>"$WORK/err")
rc=$?
if [ "$rc" != "2" ]; then
  bad "control: prefix with a malformed on-disk schema exits $rc, want 2"
  bad "         The executable-relative lookup did not run, so the embedded-schema"
  bad "         passes above no longer prove a FALLBACK — they may just be the only path."
  sed 's/^/        /' "$WORK/err" >&2
elif ! grep -qF "$PREFIX_B/schemas/open-test-intent.v1.json" "$WORK/err"; then
  bad "control: exit 2 as expected, but the diagnostic never names the derived path"
  bad "         $PREFIX_B/schemas/open-test-intent.v1.json"
  sed 's/^/        /' "$WORK/err" >&2
else
  ok "control: on-disk schema is still preferred (exit 2 naming the derived path)"
fi

# --------------------------------------------------------------------------- #
# verdict
# --------------------------------------------------------------------------- #
echo
total=$((passed + failed))
if [ "$failed" -gt 0 ]; then
  red "$failed/$total cross-build checks FAILED ($passed passed)"
  exit 1
fi
if [ "$passed" -eq 0 ]; then
  # Cannot happen given the exits above, but asserted anyway: a run that
  # checked nothing is not a pass, and that rule should not depend on the
  # control flow above staying exactly as it is today.
  red "error: no cross-build checks ran — the harness verified nothing"
  exit 2
fi
green "$passed/$total cross-build checks passed"
dim "artifacts in $DIST"
exit 0
