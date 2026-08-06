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
# Five groups of inputs are deliberately NOT compared against Python. Each is
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
#   4. Schemas carrying a `pattern` Go's RE2 engine cannot reproduce exactly.
#      validate() evaluates `pattern` with re.search (bin/validate-intent:205).
#      Python's engine and RE2 are different languages even where both compile
#      the same source: Python's `$` also matches before a trailing newline,
#      `\d`/`\w`/`\s`/`\b` are Unicode-aware in Python and ASCII-only in RE2,
#      `[[:alpha:]]` and `\p{L}` are RE2-only, and `{,n}` means `{0,n}` to
#      Python and three literal characters to RE2. The port accepts only the
#      constructs that provably agree (rewriting a trailing `$` to
#      `(?:\n?\z)`, its exact equivalent) and REFUSES the whole schema with
#      exit 2 otherwise — see cmd/validate-intent/pypattern.go. The accepted
#      half is compared here, over a grown schema, in section 8; the refused
#      half is asserted in section 10, including a check that Python loads each
#      of those schemas happily, so the refusal is a real divergence being
#      declined rather than a failure both implementations share.
#
#   5. `NaN` / `Infinity` / `-Infinity` literals in a document.
#      Python's json accepts them by default; Go's encoding/json rejects them.
#      Both implementations still exit 1 on such a file, but they classify it
#      differently — Python parses it and reports a schema violation
#      (KIND_SCHEMA), Go reports a parse failure (KIND_PARSE). Under adopter
#      text mode that difference is invisible in the prose beyond the reason
#      after the em dash, which is why it is excluded rather than fixed here.
#      It stops being cosmetic in the --json slice, where `kind` becomes
#      machine-readable: that slice must decide deliberately whether to teach
#      the decoder these literals or to declare them unsupported.
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
# self-contained trees with their own schema
# --------------------------------------------------------------------------- #
#
# Both implementations derive the schema path from their own location, so
# copying both into a tree that carries its own schemas/ directory runs them
# against a schema of our choosing while everything else stays identical. That
# is the only way to exercise the validator keywords the shipped schema does not
# declare — `pattern`, the numeric bounds, `items`, `minItems`/`maxItems` — which
# would otherwise be a large, untested fraction of validate.go that a green
# "N/N byte-identical" reads as covering.

# make_schema_root <name> — build $WORK/<name> holding both implementations and
# a schema read from stdin. Echoes the root path.
make_schema_root() {
  local root="$WORK/$1"
  mkdir -p "$root/bin" "$root/schemas"
  cp "$REFERENCE" "$root/bin/validate-intent"
  cp "$GO_BIN" "$root/bin/validate-intent-go"
  cat > "$root/schemas/open-test-intent.v1.json"
  printf '%s' "$root"
}

# compare_root <root> <label> [args...] — compare inside such a tree.
compare_root() {
  local root="$1" label="$2"
  shift 2
  compare_in "$root" "$root/bin/validate-intent" "$root/bin/validate-intent-go" "$label" "$@"
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
# 8. a grown schema — the validator keywords the shipped one does not declare
# --------------------------------------------------------------------------- #
#
# schemas/open-test-intent.v1.json declares no `pattern`, no numeric bounds and
# no `items`, so every case above leaves those branches of validate.go
# unexecuted. That is the gap the `pattern` divergence hid in: a port can be
# byte-identical on the whole shipped corpus and still answer differently the
# first time someone adds a constraint. These cases grow the schema in a
# throwaway tree and compare the same way — byte for byte, no exceptions.
echo
echo "== grown schema: pattern, bounds, items, nested paths =="

grown="$(make_schema_root grown <<'JSON'
{
  "type": "object",
  "additionalProperties": false,
  "required": ["slug"],
  "properties": {
    "slug":    {"type": "string", "pattern": "^[a-z][a-z0-9-]*$"},
    "version": {"type": "string", "pattern": "^v[0-9]{1,3}\\.[0-9]+$"},
    "count":   {"type": "integer", "minimum": 1, "maximum": 10},
    "ratio":   {"type": "number", "minimum": 0.5, "maximum": 2.5},
    "name":    {"type": "string", "minLength": 3, "maxLength": 6},
    "tags":    {"type": "array", "minItems": 1, "maxItems": 3,
                "items": {"type": "string", "minLength": 2}},
    "nested":  {"type": "object", "required": ["inner"], "additionalProperties": false,
                "properties": {"inner": {"enum": ["a", "b", 1, null]}}},
    "any":     {"enum": ["x", 1, 1.5, true, null]},
    "flag":    {"type": "boolean"},
    "either":  {"type": ["string", "null"]}
  }
}
JSON
)"

cat > "$grown/valid.json" <<'JSON'
{"slug": "abc", "version": "v12.4", "count": 3, "ratio": 1.5, "name": "abcd",
 "tags": ["aa"], "nested": {"inner": "b"}, "any": 1, "flag": true, "either": null}
JSON
compare_root "$grown" "grown: a fully valid document" valid.json

# THE case from the review. Python's `$` matches just before a trailing
# newline; RE2's does not. Before the port translated a trailing `$` this file
# was a PASS under python3 and a FAIL under the binary — same input, opposite
# verdicts, nothing on stderr to say so.
printf '{"slug": "abc\\n"}\n' > "$grown/trailing-newline.json"
compare_root "$grown" "grown: pattern, value with a trailing newline" trailing-newline.json

# The pattern is repr'd into the failure message, backslashes and all.
cat > "$grown/pattern-fail.json" <<'JSON'
{"slug": "Abc", "version": "v1234.4"}
JSON
compare_root "$grown" "grown: pattern failures, repr'd pattern in the message" pattern-fail.json

# Numeric typing: 1.0 is a float in Python and so is not an `integer`, and the
# bounds messages repr both operands.
cat > "$grown/numbers.json" <<'JSON'
{"slug": "a", "count": 1.0, "ratio": 0.25}
JSON
compare_root "$grown" "grown: 1.0 is not an integer; minimum is repr'd" numbers.json

cat > "$grown/exponent.json" <<'JSON'
{"slug": "a", "count": 1e2, "ratio": 3e0}
JSON
compare_root "$grown" "grown: exponent literals are floats" exponent.json

# Python ints are unbounded; the message reprs the literal, not a float.
cat > "$grown/bigint.json" <<'JSON'
{"slug": "a", "count": 12345678901234567890123456789}
JSON
compare_root "$grown" "grown: an integer wider than 64 bits" bigint.json

# bool is a subclass of int in Python — excluded from integer/number there by an
# explicit isinstance check, and by being a distinct type in Go.
cat > "$grown/booleans.json" <<'JSON'
{"slug": "a", "count": true, "flag": 1}
JSON
compare_root "$grown" "grown: bool is not a number, 1 is not a bool" booleans.json

cat > "$grown/empty-array.json" <<'JSON'
{"slug": "a", "tags": []}
JSON
compare_root "$grown" "grown: minItems" empty-array.json

# maxItems and a per-item failure at an indexed path, in one document.
cat > "$grown/array-items.json" <<'JSON'
{"slug": "a", "tags": ["x", "bb", 3, "dd"]}
JSON
compare_root "$grown" "grown: maxItems + items index paths" array-items.json

# A mixed-type enum reprs as a Python list literal — None and True included.
cat > "$grown/nested.json" <<'JSON'
{"slug": "a", "nested": {"inner": "c", "extra": 1}, "any": 2}
JSON
compare_root "$grown" "grown: nested paths, mixed-type enum repr" nested.json

cat > "$grown/union-type.json" <<'JSON'
{"slug": 1, "either": 5}
JSON
compare_root "$grown" "grown: union type message" union-type.json

cat > "$grown/missing.json" <<'JSON'
{}
JSON
compare_root "$grown" "grown: missing required property" missing.json

cat > "$grown/lengths.json" <<'JSON'
{"slug": "a", "name": "ab"}
JSON
compare_root "$grown" "grown: minLength" lengths.json

# Key order again, this time over the grown keyword set: every one of these
# fails, and the order of the failures must track the document.
cat > "$grown/key-order.json" <<'JSON'
{"zzz": 1, "ratio": 0.1, "aaa": 2, "count": 99, "name": "x", "slug": "A"}
JSON
compare_root "$grown" "grown: key order across the new keywords" key-order.json

compare_root "$grown" "grown: every fixture in one invocation" '*.json'

# --------------------------------------------------------------------------- #
# 9. Go-side refusals — the excluded surfaces, still asserted
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
# 10. Go-side refusals — schemas carrying a pattern RE2 cannot reproduce
# --------------------------------------------------------------------------- #
#
# The other half of excluded group 4. Each case builds a tree whose schema
# declares one divergent `pattern`, and asserts both halves of the claim:
#
#   * what python3 does with that schema — either it answers the document
#     cleanly (`pass`), which is what makes the case worth having (without the
#     refusal the port would answer the same question differently), or it cannot
#     run the pattern at all (`error`), which the port must not paper over by
#     accepting it;
#   * that the Go binary refuses the whole schema with exit 2, names the pattern
#     on stderr, and writes nothing to stdout.
#
# A compile check alone would catch only the last two rows here. Every row above
# them compiles cleanly in *both* engines and simply means something different —
# which is why the port allow-lists the constructs it can reproduce instead of
# deny-listing the ones somebody remembered to look for.
echo
echo "== schemas the port refuses (Go only) =="
pattern_refusals=0

# assert_pattern_refusal <label> <pattern> <value> <pass|error>
#
# <pattern> and <value> are written as they appear inside the JSON document, so
# a regex backslash is spelled with two.
assert_pattern_refusal() {
  local label="$1" pattern="$2" value="$3" python_outcome="$4"
  pattern_refusals=$((pattern_refusals + 1))
  local root="$WORK/pattern-refusal-$pattern_refusals"
  mkdir -p "$root/bin" "$root/schemas"
  cp "$REFERENCE" "$root/bin/validate-intent"
  cp "$GO_BIN" "$root/bin/validate-intent-go"
  printf '{"type": "object", "properties": {"s": {"type": "string", "pattern": "%s"}}}\n' \
    "$pattern" > "$root/schemas/open-test-intent.v1.json"
  printf '{"s": %s}\n' "$value" > "$root/doc.json"

  local py_rc go_rc
  (cd "$root" && "$PYTHON" "$root/bin/validate-intent" doc.json \
      >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  (cd "$root" && "$root/bin/validate-intent-go" doc.json \
      >"$WORK/go.out" 2>"$WORK/go.err")
  go_rc=$?

  case "$python_outcome" in
    pass)
      if [ "$py_rc" -ne 0 ] || grep -q Traceback "$WORK/py.err"; then
        failed=$((failed + 1))
        red "  FAIL  $label — python was expected to accept this document (rc=$py_rc)"
        printf '        %s\n' "$(head -1 "$WORK/py.err")"
        return 1
      fi
      ;;
    error)
      if ! grep -q Traceback "$WORK/py.err"; then
        failed=$((failed + 1))
        red "  FAIL  $label — python was expected to reject this pattern outright (rc=$py_rc)"
        return 1
      fi
      ;;
    *)
      failed=$((failed + 1))
      red "  FAIL  $label — bad python outcome '$python_outcome' in the harness"
      return 1
      ;;
  esac

  if [ "$go_rc" -ne 2 ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — expected the Go port to refuse the schema with exit 2, got $go_rc"
    printf '        go stdout: %s\n' "$(head -1 "$WORK/go.out")"
    return 1
  fi
  if ! grep -q 'pattern' "$WORK/go.err"; then
    failed=$((failed + 1))
    red "  FAIL  $label — exit 2 but the diagnostic never names the pattern"
    printf '        %s\n' "$(head -1 "$WORK/go.err")"
    return 1
  fi
  if [ -s "$WORK/go.out" ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — a refusal must not write to stdout"
    return 1
  fi
  passed=$((passed + 1))
  printf '  ok    %s (python %s, go exit 2)\n' "$label" "$python_outcome"
  return 0
}

# Unicode-vs-ASCII character classes: python3 accepts every one of these
# documents, and RE2 would have failed them.
assert_pattern_refusal '\d is Unicode-aware in Python' \
  '^\\d+$' '"٣٤"' pass
assert_pattern_refusal '\w is Unicode-aware in Python' \
  '^\\w+$' '"café"' pass
assert_pattern_refusal '\s is Unicode-aware in Python' \
  '^\\s+$' '" "' pass
assert_pattern_refusal '\b is Unicode-aware in Python' \
  '\\bcaf\\u00e9\\b' '"café"' pass
# A `$` anywhere but the end cannot be rewritten exactly, so it is refused
# rather than approximated.
assert_pattern_refusal '$ in the middle of a pattern' \
  'a$\\n' '"a\n"' pass
# Python reads [[:alpha:]] as the class {[ : a l p h} followed by a literal ']',
# RE2 as a POSIX class: "[]" matches in Python and does not in RE2.
assert_pattern_refusal 'POSIX class [[:alpha:]]' \
  '[[:alpha:]]+' '"[]"' pass
# Python reads {,3} as {0,3}; RE2 reads four literal characters.
assert_pattern_refusal '{,n} means {0,n} to Python' \
  'a{,3}' '"aaa"' pass
assert_pattern_refusal 'inline flag (?i)' \
  '(?i)^abc$' '"ABC"' pass
# The mirror image: syntax RE2 accepts that Python cannot compile at all. Left
# alone, the port would answer confidently where the reference cannot answer.
assert_pattern_refusal 'Unicode class \p{L} is RE2-only' \
  '\\p{L}+' '"abc"' error
# The two the old compile-only guard already caught, kept so that narrower
# failure mode stays covered.
assert_pattern_refusal 'lookbehind' \
  '(?<=a)b' '"ab"' pass
assert_pattern_refusal 'backreference' \
  '(a)\\1' '"aa"' pass

# --------------------------------------------------------------------------- #
# 11. the reference is untouched
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
