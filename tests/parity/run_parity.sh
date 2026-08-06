#!/usr/bin/env bash
#
# Differential parity harness: Go port vs. the Python reference.
# =============================================================
#
#   tests/parity/run_parity.sh
#
# For every argument set below, this runs
#
#     python3 bin/validate-intent ARGS...      (the oracle)
#     bin/validate-intent-go      ARGS...      (the port under test)
#
# and requires their **stdout, stderr and exit code to be byte-identical**.
# Any single mismatch fails the whole run with a non-zero exit and a unified
# diff naming the case. There is no "close enough" tier.
#
# Python is the sole oracle here. specguard-rspec has no ported validator logic
# yet, so there is no second implementation to cross-check against — which is
# why the port is proven by differential testing rather than by a hand-written
# expectation file that could encode the same mistake twice.
#
#
# Why the Go binary must live in bin/
# -----------------------------------
# The reference derives its schema path from its own location
# (bin/validate-intent:76-77: the repo root is the parent of the script's
# directory). The port does the same from os.Executable(), so building it to
# bin/validate-intent-go makes both resolve the *same* schema path — which is
# what lets the "could not load schema" diagnostic be compared byte for byte
# rather than waved through. Building it anywhere else changes that path.
#
#
# Excluded cases, and why
# -----------------------
# Three groups of inputs are deliberately NOT compared against Python. Each is
# excluded for a stated reason, and each is still asserted against on the Go
# side (see "Go-side refusals" at the bottom) so the exclusion cannot quietly
# become "untested".
#
#   1. Recursive `**` glob patterns.
#      Python globs with recursive=True; Go's filepath.Glob has no `**` at all,
#      and the port implements the rest of Python's glob syntax rather than
#      approximating this one. A `**` component is REFUSED with exit 2, never
#      silently downgraded to a single `*` — a downgrade would quietly check a
#      smaller set of files and still report a clean pass.
#
#   2. Malformed JSON and non-UTF-8 input.
#      check_file embeds the raw Python exception text
#      ("could not read/parse JSON: %s" % exc, bin/validate-intent:243). Go's
#      encoding/json cannot reproduce json.JSONDecodeError's wording, so the
#      prose after the colon differs by construction. Everything around it does
#      match — the FAIL line shape, the file path, the exit code — and no
#      shipped examples/invalid/*.json fixture is malformed (all four parse
#      cleanly and fail on schema grounds), so this is unexercised by the
#      corpus. Read and OS errors are NOT excluded: Python's OSError prose is
#      mechanical enough to reproduce exactly, and cases 21-22 below prove it.
#
#   3. Modes outside slice 1: self-test (no arguments), stdin (`-`),
#      `--source`/`-s`, and `--json`.
#      Not implemented yet. They are refused with exit 2 rather than falling
#      through to adopter mode, where `--source foo.rb` would be read as a
#      filename glob and produce a confident, correctly-formatted, wrong answer.
#
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
PYTHON="${PYTHON:-python3}"
GO="${GO:-go}"
REFERENCE="$REPO_ROOT/bin/validate-intent"
GO_BIN="$REPO_ROOT/bin/validate-intent-go"

WORK="$(mktemp -d)"
trap 'chmod -R u+rwX "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0
skipped=0

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
dim()   { printf '\033[2m%s\033[0m\n' "$*"; }

# --------------------------------------------------------------------------- #
# build
# --------------------------------------------------------------------------- #
if ! command -v "$GO" >/dev/null 2>&1; then
  red "error: no Go toolchain on PATH (set GO=/path/to/go)"
  exit 2
fi
if ! command -v "$PYTHON" >/dev/null 2>&1; then
  red "error: no $PYTHON on PATH (set PYTHON=...)"
  exit 2
fi

dim "building $GO_BIN ..."
if ! (cd "$REPO_ROOT" && "$GO" build -o "$GO_BIN" ./cmd/validate-intent); then
  red "error: go build failed"
  exit 2
fi

# --------------------------------------------------------------------------- #
# the comparison primitive
# --------------------------------------------------------------------------- #

# compare_in <cwd> <reference-script> <go-binary> <label> [args...]
#
# Runs both implementations with identical arguments from identical working
# directories and requires identical stdout, stderr and exit code.
compare_in() {
  local cwd="$1" reference="$2" gobin="$3" label="$4"
  shift 4

  local py_rc go_rc
  (cd "$cwd" && "$PYTHON" "$reference" "$@" >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  (cd "$cwd" && "$gobin" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
  go_rc=$?

  local problems=()
  [ "$py_rc" = "$go_rc" ] || problems+=("exit code: python=$py_rc go=$go_rc")
  cmp -s "$WORK/py.out" "$WORK/go.out" || problems+=("stdout differs")
  cmp -s "$WORK/py.err" "$WORK/go.err" || problems+=("stderr differs")

  if [ ${#problems[@]} -eq 0 ]; then
    passed=$((passed + 1))
    printf '  ok    %s\n' "$label"
    return 0
  fi

  failed=$((failed + 1))
  red "  FAIL  $label"
  printf '        args: %s\n' "$*"
  local problem
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  if ! cmp -s "$WORK/py.out" "$WORK/go.out"; then
    printf '        --- stdout (-python +go) ---\n'
    diff -u "$WORK/py.out" "$WORK/go.out" | tail -n +3 | sed 's/^/        /'
  fi
  if ! cmp -s "$WORK/py.err" "$WORK/go.err"; then
    printf '        --- stderr (-python +go) ---\n'
    diff -u "$WORK/py.err" "$WORK/go.err" | tail -n +3 | sed 's/^/        /'
  fi
  return 1
}

# compare <label> [args...] — the common case: run from the repo root.
compare() {
  local label="$1"
  shift
  compare_in "$REPO_ROOT" "$REFERENCE" "$GO_BIN" "$label" "$@"
}

# --------------------------------------------------------------------------- #
# 1. every shipped fixture, individually
# --------------------------------------------------------------------------- #
echo
echo "== shipped fixtures, one argument at a time =="
for fixture in "$REPO_ROOT"/examples/*.json "$REPO_ROOT"/examples/invalid/*.json; do
  rel="${fixture#"$REPO_ROOT"/}"
  compare "$rel" "$rel"
done

# --------------------------------------------------------------------------- #
# 2. the parity fixtures — the hazards the shipped corpus does not reach
# --------------------------------------------------------------------------- #
echo
echo "== parity fixtures =="
# key-order-forward/reversed hold the SAME six violations with their keys in
# opposite, non-alphabetical order. validate() walks `instance.items()`, so the
# reference emits them in *document* order. A port backed by a Go map would
# emit them in randomized order and pass this only intermittently — which is
# why both directions are pinned, not just one.
compare "key order (forward)"  tests/parity/fixtures/key-order-forward.json
compare "key order (reversed)" tests/parity/fixtures/key-order-reversed.json
compare "key order (both, one invocation)" \
  tests/parity/fixtures/key-order-forward.json \
  tests/parity/fixtures/key-order-reversed.json
# A duplicated key takes the later value but keeps the earlier position, so the
# `entity` error must still sort after the `zzz` one.
compare "duplicate keys"          tests/parity/fixtures/duplicate-keys.json
compare "array items + index path" tests/parity/fixtures/array-items.json
compare "type mismatches"          tests/parity/fixtures/wrong-types.json
compare "non-object root"          tests/parity/fixtures/not-an-object.json
# repr() picks its quote character by content, and escapes control characters
# while leaving printable non-ASCII literal. Both land in the enum message.
compare "repr: quotes in value"    tests/parity/fixtures/quotes-in-value.json
compare "repr: unicode + control"  tests/parity/fixtures/unicode-and-control.json
compare "valid, with preconditions" tests/parity/fixtures/valid-with-preconditions.json
compare "hidden file by explicit path" tests/parity/fixtures/.hidden-draft.json
# Python's len() over a str counts code points; Go's len() counts bytes. This
# behavior is 12 characters but 17 bytes, so a byte-counting port would clear
# the minLength of 15 and report the file as valid.
compare "minLength counts code points" tests/parity/fixtures/multibyte-length.json

# --------------------------------------------------------------------------- #
# 3. multiple arguments — output order and exit-code aggregation
# --------------------------------------------------------------------------- #
echo
echo "== multiple arguments =="
compare "all valid examples" \
  examples/request-orders-checkout.json \
  examples/system-checkout-flow.json \
  examples/unit-checkout-service.json \
  examples/unit-order-total.json
compare "all invalid examples" \
  examples/invalid/bad-layer.json \
  examples/invalid/missing-required.json \
  examples/invalid/short-behavior.json \
  examples/invalid/typo-extra-property.json
compare "valid then invalid" \
  examples/unit-order-total.json examples/invalid/bad-layer.json
compare "invalid then valid" \
  examples/invalid/bad-layer.json examples/unit-order-total.json
compare "the same file twice (not de-duplicated)" \
  examples/unit-order-total.json examples/unit-order-total.json
compare "the schema itself is not a valid intent" schemas/open-test-intent.v1.json

# --------------------------------------------------------------------------- #
# 4. glob expansion — done by the program, so every pattern is quoted
# --------------------------------------------------------------------------- #
echo
echo "== glob expansion =="
compare "examples/*.json"          'examples/*.json'
compare "examples/invalid/*.json"  'examples/invalid/*.json'
# `examples/*` also matches the invalid/ and sources/ directories; _expand_files
# drops them, so only the four JSON files are checked.
compare "examples/* (directories filtered)" 'examples/*'
compare "examples/*/*.json (magic dirname)" 'examples/*/*.json'
compare "character class"          'examples/[su]*.json'
compare "negated character class"  'examples/[!u]*.json'
compare "single-character wildcard" 'examples/?nit-*.json'
# Python's fnmatch treats an unterminated '[' as a literal; Go's filepath.Match
# would reject the pattern outright, so the port implements Python's rule. The
# second case is the one with teeth: it matches a file whose name really does
# contain a '[', so a port that rejected the pattern would find nothing and
# report a no-match instead of a PASS.
compare "unterminated character class" 'examples/[unclosed*.json'
compare "unterminated [ matches a literal bracket" 'tests/parity/fixtures/bracket[unclosed*.json'
# A bare '*' never matches a dotfile in Python; Go's filepath.Glob would.
compare "dotfile excluded by a bare *" 'tests/parity/fixtures/*.json'
compare "dotfile included by an explicit ." 'tests/parity/fixtures/.*.json'
compare "class over the parity fixtures" 'tests/parity/fixtures/[kw]*.json'
# os.path.split/join round-tripping: a redundant separator survives verbatim
# when the pattern has no magic, and is normalised away when it does.
compare "redundant separator, no magic" 'examples//unit-order-total.json'
compare "redundant separator, with magic" 'examples//*.json'
compare "leading ./"               './examples/unit-order-total.json'
compare "absolute path"            "$REPO_ROOT/examples/unit-order-total.json"

# --------------------------------------------------------------------------- #
# 5. no-match diagnostics — never a silent pass
# --------------------------------------------------------------------------- #
echo
echo "== no-match =="
# The diagnostic reprs the pattern (bin/validate-intent:493), so the quoting is
# part of the contract too.
compare "glob matching nothing"    'nope/*.json'
compare "missing literal path"     'nope.json'
compare "a directory is not a file" 'examples'
compare "a directory is not a file (nested)" 'examples/invalid'
compare "glob matching only directories" 'examples/*/'
compare "pattern needing repr escaping" "no'such/*.json"
compare "no-match mixed with matches" \
  'examples/*.json' 'nope/*.json' 'examples/invalid/*.json'
compare "no-match first" 'nope/*.json' 'examples/unit-order-total.json'
# The reference has no notion of an "unknown flag": main() falls through to
# adopter mode, so anything unrecognised is treated as a path and reported as a
# no-match. The port must not be tidier about this than the oracle — a
# well-meant "unknown option" error here would be a divergence.
compare "unknown short flag"       -x
compare "unknown long flag"        --unknown
compare "near-miss of --json"      --jsonx
compare "near-miss of --source"    --sources 'examples/*.json'

# --------------------------------------------------------------------------- #
# 6. --help
# --------------------------------------------------------------------------- #
echo
echo "== help =="
compare "--help"                   --help
compare "-h"                       -h
compare "--help wins over a file"  'examples/*.json' --help
compare "--help wins over a missing file" nope.json -h

# --------------------------------------------------------------------------- #
# 7. OS-level failures
# --------------------------------------------------------------------------- #
echo
echo "== read and schema failures =="

# An unreadable file: Python renders str(OSError), which the port reproduces
# rather than excluding — "[Errno 13] Permission denied: '<path>'", repr'd path
# included.
unreadable="$WORK/unreadable.json"
cp "$REPO_ROOT/examples/unit-order-total.json" "$unreadable"
chmod 000 "$unreadable"
if [ -r "$unreadable" ]; then
  # Running as root (or on a filesystem ignoring the mode) makes the file
  # readable anyway. Skip loudly — a case that could not be set up must not be
  # counted as one that passed.
  skipped=$((skipped + 1))
  red "  SKIP  unreadable file — chmod 000 did not make it unreadable (running as root?)"
else
  compare "unreadable file" "$unreadable"
fi
chmod 644 "$unreadable"

# A missing schema: both implementations derive the schema path from their own
# location, so copying both into a tree with no schemas/ directory makes them
# compute the same missing path and report it identically. Exit 2.
schema_root="$WORK/noschema"
mkdir -p "$schema_root/bin"
cp "$REFERENCE" "$schema_root/bin/validate-intent"
cp "$GO_BIN" "$schema_root/bin/validate-intent-go"
cp "$REPO_ROOT/examples/unit-order-total.json" "$schema_root/thing.json"
compare_in "$schema_root" "$schema_root/bin/validate-intent" \
  "$schema_root/bin/validate-intent-go" "missing schema (exit 2)" thing.json

# An unreadable schema is the same code path with a different errno.
schema_root2="$WORK/badschema"
mkdir -p "$schema_root2/bin" "$schema_root2/schemas"
cp "$REFERENCE" "$schema_root2/bin/validate-intent"
cp "$GO_BIN" "$schema_root2/bin/validate-intent-go"
cp "$REPO_ROOT/schemas/open-test-intent.v1.json" "$schema_root2/schemas/"
cp "$REPO_ROOT/examples/unit-order-total.json" "$schema_root2/thing.json"
chmod 000 "$schema_root2/schemas/open-test-intent.v1.json"
if [ -r "$schema_root2/schemas/open-test-intent.v1.json" ]; then
  skipped=$((skipped + 1))
  red "  SKIP  unreadable schema — chmod 000 did not make it unreadable (running as root?)"
else
  compare_in "$schema_root2" "$schema_root2/bin/validate-intent" \
    "$schema_root2/bin/validate-intent-go" "unreadable schema (exit 2)" thing.json
fi
chmod 644 "$schema_root2/schemas/open-test-intent.v1.json"

# --------------------------------------------------------------------------- #
# 8. Go-side refusals — the excluded cases, still asserted
# --------------------------------------------------------------------------- #
#
# These are the inputs listed as excluded in the header. They are not compared
# against Python (the outputs differ by design), but "excluded" must not mean
# "unchecked": each has to exit 2 with a diagnostic on stderr. The failure this
# guards against is a fall-through — `--source foo.rb` reaching adopter mode and
# being reported as a filename that matched nothing, which looks like a real
# answer.
echo
echo "== excluded surfaces refuse loudly (Go only) =="
assert_refusal() {
  local label="$1"
  shift
  local rc
  (cd "$REPO_ROOT" && "$GO_BIN" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
  rc=$?
  if [ "$rc" -ne 2 ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — expected exit 2, got $rc"
    return 1
  fi
  if [ ! -s "$WORK/go.err" ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — exit 2 but nothing on stderr"
    return 1
  fi
  if [ -s "$WORK/go.out" ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — a refusal must not write to stdout"
    return 1
  fi
  passed=$((passed + 1))
  printf '  ok    %s (exit 2: %s)\n' "$label" "$(head -1 "$WORK/go.err")"
  return 0
}

assert_refusal "self-test mode"
assert_refusal "stdin mode" -
assert_refusal "source mode (--source)" --source 'examples/*.json'
assert_refusal "source mode (-s)" -s 'examples/*.json'
assert_refusal "--json" --json 'examples/*.json'
assert_refusal "--json anywhere on the line" 'examples/*.json' --json
assert_refusal "recursive glob" 'examples/**/*.json'
assert_refusal "recursive glob, bare" '**'

# --------------------------------------------------------------------------- #
# 9. the reference is untouched
# --------------------------------------------------------------------------- #
#
# This slice adds only. If the port were "made to pass" by editing the oracle,
# every comparison above would still be green — so the oracle's integrity is
# checked explicitly rather than assumed.
echo
echo "== the oracle is unmodified =="
if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  dirty="$(git -C "$REPO_ROOT" status --porcelain -- bin/validate-intent tests/test_validate_intent.py)"
  if [ -n "$dirty" ]; then
    failed=$((failed + 1))
    red "  FAIL  the Python reference has local modifications:"
    printf '        %s\n' "$dirty"
  else
    passed=$((passed + 1))
    printf '  ok    bin/validate-intent and tests/test_validate_intent.py are unmodified\n'
  fi
else
  skipped=$((skipped + 1))
  red "  SKIP  not a git checkout — cannot verify the reference is unmodified"
fi

# --------------------------------------------------------------------------- #
# verdict
# --------------------------------------------------------------------------- #
echo
total=$((passed + failed))
if [ "$failed" -gt 0 ]; then
  red "$failed/$total parity cases FAILED ($passed passed, $skipped skipped)"
  exit 1
fi
if [ "$passed" -eq 0 ]; then
  # A run that compared nothing is not a pass. Same rule the reference applies
  # to an empty fixture set (bin/validate-intent:648-665).
  red "error: no parity cases ran — the harness verified nothing"
  exit 1
fi
green "$passed/$total parity cases passed byte-for-byte ($skipped skipped)"
if [ "$skipped" -gt 0 ]; then
  dim "note: skipped cases were not verified — see the SKIP lines above"
fi
exit 0
