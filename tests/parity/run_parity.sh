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
# One surface is a documented superset rather than an equality, and it is the
# only one: `--help` prints the shared usage block plus a Go-only trailer
# documenting `--version` and `--schema-source` (excluded groups 5 and 6
# below). That is not a "close enough" tier either — section 7 ("--help")
# splits the port's stdout at the reference's exact byte length and requires
# the shared half to be identical and the trailer to equal a stated
# expectation. Both halves are compared; the split is where they are compared
# against different things.
#
# Python is the oracle here, and the port is proven by differential testing
# against it rather than by a hand-written expectation file that could encode
# the same mistake twice.
#
# This paragraph used to go on to say that specguard-rspec "has no ported
# validator logic yet, so there is no second implementation to cross-check
# against". That stopped being true: the gem's linter is complete end to end
# (lib/specguard/rspec/{scanner,linter,schema}.rb), which makes it a genuinely
# independent re-derivation of PROTOCOL.md §1 — written against the spec, not
# against this port. It is now cross-checked, in tests/parity/run_ruby_parity.sh
# and in section 19 ("the Ruby leg — specguard-lint vs. the port") below.
#
# That leg is a SEPARATE script rather than more sections in this file, for one
# reason: this file refuses to start without a Go toolchain (it rebuilds the
# port before comparing it, which is right for a leg whose subject is the Go
# code), while the Ruby leg's subject is the GEM and has to stay runnable on a
# Ruby machine with no Go installed. See that file's header for the four report
# differences it normalises out, the two read-failure differences it ratifies,
# and — because the normalisation deletes both sides' output for a clean file —
# how it avoids comparing nothing to nothing and calling it parity.
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
# SIX groups of inputs are deliberately NOT compared against Python. Each is
# excluded for a stated reason, and each is still asserted against on the Go
# side (see "Go-side refusals" at the bottom) so the exclusion cannot quietly
# become "untested".
#
# (Six, and the arithmetic has never gone in one direction, so it is worth
# spelling out: slice 1 listed five, slice 2 retired two of them — see RETIRED
# EXCLUSIONS below — and slice 1's separate "unsupported schema construct"
# group folded into group 3 when the `pattern` port landed, which left four.
# Slice 5 (SPGD-131) then ADDED a group, when the Go binary gained an embedded
# schema and stopped failing on trees the Python script still cannot run in,
# which made five; slice 4 (SPGD-123) then retired the recursive-glob group
# when `**` was implemented, which brought it back to four. Slice 6 (SPGD-141)
# then ADDED group 5, the `--version` flag, which made five again, and slice 19
# (SPGD-301) ADDED group 6, `--schema-source`. The count is right; it just does
# not arithmetic down from five in one step.)
#
#   1. Non-UTF-8 input — the PROSE only.
#      A file whose bytes do not decode raises UnicodeDecodeError in Python, and
#      the message names a reason ("invalid start byte", "invalid continuation
#      byte", ...) that CPython classifies more finely than the port does. The
#      CLASSIFICATION and the exit code are exact — such a file is a read
#      failure in both — so only the tail of the message can differ.
#
#   2. Modes outside slices 1-2: stdin (`-`), and `--json` for *adopter*
#      (FILE...) mode. Not implemented yet. They are refused with exit 2 rather
#      than falling through, where `-` would be read as a filename and produce a
#      confident, correctly-formatted, wrong answer.
#
#   3. Schemas carrying a `pattern` Go's RE2 engine cannot reproduce exactly.
#      validate() evaluates `pattern` with re.search (bin/validate-intent:205).
#      Python's engine and RE2 are different languages even where both compile
#      the same source: Python's `$` also matches before a trailing newline,
#      `\d`/`\w`/`\s`/`\b` are Unicode-aware in Python and ASCII-only in RE2,
#      `[[:alpha:]]` and `\p{L}` are RE2-only, and `{,n}` means `{0,n}` to
#      Python and three literal characters to RE2. A second, sharper kind sits
#      alongside those: constructs RE2 compiles that Python cannot parse at
#      all, where the port would not merely disagree with the reference but
#      answer a question the reference raises an exception on — `\p{L}`, and a
#      quantifier applied to a bare `^`/`\A` (`^*`, `\A{2}`), which RE2 reads
#      as "match empty anywhere" and Python rejects with "nothing to repeat".
#      The port accepts only the constructs that provably agree (rewriting a
#      trailing `$` to `(?:\n?\z)`, its exact equivalent) and REFUSES the whole
#      schema with exit 2 otherwise — see cmd/validate-intent/pypattern.go. The
#      accepted half is compared here, over a grown schema, in section 9 ("a
#      grown schema — the validator keywords the shipped one does not
#      declare"); the refused half is asserted in section 17 ("Go-side
#      refusals — schemas carrying a pattern RE2 cannot reproduce"), including
#      a check on what Python does with each of those schemas, so the refusal
#      is a real divergence being declined rather than a failure both
#      implementations share.
#
#   4. A tree with no schemas/ directory beside the binary.
#      The Go port embeds schemas/open-test-intent.v1.json (see schema.go at the
#      module root) and falls back to that copy when — and only when — the file
#      at <exe>/../schemas/ is ABSENT. That is the entire point of SPGD-131: a
#      released binary at /usr/local/bin/validate-intent has no repo around it,
#      resolved its schema to /usr/local/schemas/open-test-intent.v1.json, and
#      before the fix exited 2 in every working mode — adopter, --source,
#      self-test, --json. Only --help worked. The Python reference has no such
#      fallback and still fails on such a tree, by design, so the two genuinely
#      diverge and cannot be compared.
#
#      This exclusion is NARROW, and the narrowness is the design. A schema that
#      is PRESENT but unreadable, malformed or uncompilable is still compared,
#      in section 8 ("OS-level failures"): a file that exists is a deliberate
#      override, and falling back past a broken one would answer the user's own
#      mistake with a clean green report. Note what that narrowness preserves —
#      the crossing of "the schema fails to load" with "bare --json" in
#      section 8 ("OS-level failures"),
#      the crossing that once caught main.go refusing --json BEFORE LoadSchema,
#      is NOT lost to this exclusion. It moved from a schema-less tree to a
#      malformed-schema one and stayed a byte-for-byte comparison.
#
#      Asserted on the Go side in section 16 ("Go-side refusals — the excluded
#      surfaces, still asserted"), which pins what the binary does instead for
#      each of the five argument sets that used to be compared here — including
#      the two that still FAIL. A bare self-test on such a tree exits 1 with
#      four "no fixtures match" lines, because the fixture corpus is not
#      embedded: that is SPGD-56's empty-fixture guard working correctly, and it
#      is asserted rather than assumed so nobody later reads the exclusion as a
#      claim that everything works out there.
#
#      One caveat worth stating plainly, in the same spirit as the decoder
#      caveat below: this harness proves the embedded schema BEHAVES like the
#      canonical one on the inputs it happens to reach. It does NOT prove they
#      are the same bytes. That is pinned by SHA256 in schema_test.go at the
#      module root, and THIS HARNESS DOES NOT RUN IT — run `go test ./...`
#      separately (./... , not ./cmd/... , or the root package's guard is
#      skipped and the pin verifies nothing).
#
#   5. `--version`.
#      A Go-only flag (slice 6, SPGD-141, cmd/validate-intent/version.go). The
#      reference has no such flag: it reads `--version` as a filename, reports
#      "no file(s) match '--version'" and exits 1 — the code the contract
#      reserves for "at least one annotation is malformed". That is faithfully
#      ported behaviour and NOT a defect to fix on either side; it is simply the
#      wrong answer for a released binary being asked what it is. The natural CI
#      preflight `validate-intent --version || echo "not installed"` reports a
#      malformed annotation that does not exist.
#
#      This group is the ODD ONE OUT. Every other excluded surface is a REFUSAL
#      — exit 2, a diagnostic on stderr, nothing on stdout. `--version` exits 0
#      and writes to stdout, so assert_refusal fails it on all three counts and
#      could not be reused. It is asserted by assert_version_line instead, in
#      section 16 ("Go-side refusals — the excluded surfaces, still asserted"),
#      whose header prose had to stop saying every member of it exits 2. That
#      helper pins the exit code, the empty stderr, the single line, and — the
#      point of the flag — that the identity token is neither empty nor a
#      placeholder.
#
#      The crossing WITH `--help` is not excluded, because it does not diverge:
#      `--help` wins over everything in both implementations, so the reference
#      prints usage for `--help --version` exactly as the port does. It is
#      compared in section 7 ("--help") rather than asserted here, which is what
#      keeps "--help still wins" from becoming a Go-side claim about the port
#      checked only against itself.
#
#      `--help` is also where this flag is DOCUMENTED, and that is the one place
#      a Go-only surface reaches a compared output. The row cannot go in the
#      shared usage constant: that text is printed on refusals too, three of
#      which are compared, and the matched edit to bin/validate-intent that
#      would normally restore them is unavailable — the reference has no
#      --version to document, and a usage block advertising a flag that answers
#      "no file(s) match" would be a worse falsehood than none. So the row is a
#      TRAILER appended on the --help path alone
#      (cmd/validate-intent/version.go), and section 7's compare_help asserts it
#      as an exact expectation while still comparing the shared block
#      byte-for-byte. Section 16 pins what the flag DOES; section 7 pins what
#      --help SAYS about it. Neither is described without being checked.
#
#      That trailer now carries TWO rows — group 6 below is documented in the
#      same constant, for the same reason and under the same comparison.
#
#   6. `--schema-source`.
#      The second Go-only flag (slice 19, SPGD-301,
#      cmd/validate-intent/schemasource.go), excluded for exactly the reason
#      group 5 is: the reference has no such flag and reads it as a filename,
#      reporting "no file(s) match '--schema-source'" with exit 1.
#
#      What it adds is the answer to the hedge group 5's trailer makes. That
#      trailer has to say the reported digest names the contract the artifact
#      CARRIES and not the one a run ENFORCED, because `--version` returns above
#      LoadSchema and cannot know which copy wins. This flag runs the real
#      loader and reports the resolved origin — an absolute path, or
#      `<embedded schema>` — plus the SHA-256 of the bytes actually loaded.
#
#      The difference is this harness's own normal operating mode, not an edge
#      case: compare_root and assert_pattern_refusal plant synthetic schemas
#      beside the binary, so most of the schema coverage in this file runs
#      against a contract the embedded digest has never seen.
#
#      Like group 5 it SUCCEEDS rather than refuses — exit 0, one line on
#      stdout, nothing on stderr — so assert_refusal cannot cover it either. It
#      is asserted by assert_schema_source_in in section 16, which pins the
#      origin AND the digest against a sha256sum/shasum computed here, on three
#      trees chosen so that the answer must differ between them: the repo (a
#      real schema beside the binary), an install prefix (no schemas/, so the
#      embedded copy), and a tree carrying a schema that is valid and DIFFERENT
#      (where the reported digest must not equal `--version`'s — the whole point
#      of the flag, and the one assertion a wrong implementation cannot pass by
#      printing something plausible).
#
#      Its FAILURE is not excluded from comparison in spirit: on a tree whose
#      schema exists and cannot be loaded it must exit 2 with the diagnostic
#      that is itself compared against python3 in section 8 ("OS-level
#      failures"). Section 16 asserts that by running the verdict path on the
#      same tree and requiring the two stderr streams to be byte-identical,
#      rather than by restating the message here where it could drift.
#
#      The crossing with `--help` is not excluded, for the same reason group 5's
#      is not: the reference's --help loop pre-empts the argument, so both print
#      usage and it is a real comparison in section 7. The crossing with
#      `--version` IS Go-side (Python has neither flag) and is asserted in
#      section 16: `--version` wins, because three external consumers already
#      parse its output and a new flag must not change what any crossing of the
#      two prints.
#
# RETIRED EXCLUSIONS — slice 2 (SPGD-102) closed two of slice 1's five, and
# slice 4 (SPGD-123) closed a third:
#
#   * Recursive `**` glob patterns. Slice 1 refused them with exit 2 rather than
#     downgrading `**` to a single `*`, because a downgrade would quietly check
#     a smaller set of files and still report a clean pass. THAT rule has not
#     changed and is not up for revisiting; what changed is that the port no
#     longer needs an approximation, because it now implements
#     `glob.glob(pattern, recursive=True)` itself — zero-segment matches, hidden
#     directories skipped at every level of the descent, symlinked directories
#     followed — in cmd/validate-intent/pyglob.go. The refusal was a whole-argv
#     precheck, so retiring it retires `**` for adopter mode and `--source`
#     together. Compared here in section 5 ("recursive `**` — the surface a file
#     COUNT cannot tell apart from a bug").
#
#   * Malformed JSON prose. check_file and check_source_file both embed the raw
#     exception text ("could not read/parse JSON: %s", "could not parse
#     annotation: %s"), which slice 1 could not reproduce with encoding/json.
#     `--source` made that unaffordable — an unparseable payload is what a
#     typo'd annotation produces, i.e. the case adopters actually hit — so the
#     port now carries its own CPython-compatible decoder
#     (cmd/validate-intent/pyjson.go) that reproduces json.JSONDecodeError's
#     message, line, column and character offset. Compared here in section 12
#     ("malformed payloads and documents — the retired exclusions").
#
#     Caveat worth stating plainly: that section compares the decoder only
#     through the CLI, on the payloads the corpus and fixtures happen to reach
#     it with. The broad evidence for that decoder is a differential fuzzer
#     against python3's json.loads in cmd/validate-intent/pyjson_fuzz_test.go,
#     and THIS HARNESS DOES NOT RUN IT — run `go test ./cmd/validate-intent`
#     separately. Passing every case here is not by itself a claim about the
#     decoder's general correctness. (Deliberately not stated as a case count:
#     the count moves whenever cases are added, and a stale figure in prose
#     reads like evidence long after it has stopped being any.)
#
#   * `NaN` / `Infinity` / `-Infinity` literals. Python's json accepts them and
#     encoding/json rejected them, so the two classified such a document
#     differently (schema violation vs parse failure). The new decoder accepts
#     them exactly as Python does. Compared here in section 12 ("malformed
#     payloads and documents — the retired exclusions").
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
# 5. recursive `**` — the surface a file COUNT cannot tell apart from a bug
# --------------------------------------------------------------------------- #
#
# `**` was excluded from this harness for the first three slices and refused
# with exit 2 (see RETIRED EXCLUSIONS in the header). It is now compared like
# everything else, over a committed fixture tree — tests/parity/globtree/ —
# built to carry the four shapes that separate a real port from a plausible one:
#
#   spec/a.json                  `**` expanding to ZERO path segments
#   spec/requests/admin/d.json   depth 2, and deliberately the only INVALID
#                                document in the tree — so a port that misses it
#                                exits 0 where the reference exits 1, instead of
#                                just printing a shorter list of passes
#   spec/linked -> real          a SYMLINKED directory, which Python descends
#   spec/.secret/f.json          a HIDDEN directory, which Python does not
#
# The last two are why every case here compares the whole rendered report and
# never a file count. Both obvious Go implementations were measured against this
# tree: filepath.Glob("spec/**/*.json") wrongly includes .secret/f.json and
# misses both spec/a.json and the depth-2 file, while filepath.WalkDir plus a
# suffix filter returns the SAME NUMBER OF PATHS as Python and a different set —
# it drops everything under spec/linked, because fs.WalkDir does not follow
# symlinks and Python does. Sanity-checking by count certifies the second one.
# cmd/validate-intent/pyglob_test.go pins the sorted path list at the unit
# level; this section pins what the user actually sees.
#
# The cases run from inside the tree, so the reported paths are the relative
# ones the patterns were written with. Both implementations still resolve their
# schema from their own location in bin/, which the working directory does not
# affect.
echo
echo "== recursive ** globbing =="
GLOB_TREE="$REPO_ROOT/tests/parity/globtree"

# compare_tree <label> [args...] — compare from inside the glob fixture tree.
compare_tree() {
  local label="$1"
  shift
  compare_in "$GLOB_TREE" "$REFERENCE" "$GO_BIN" "$label" "$@"
}

compare_tree "** across the fixture tree" 'spec/**/*.json'
# `**` is not "at least one directory" — the case a hand-rolled recursion drops.
compare_tree "** matching zero segments"  'spec/**/a.json'
# ... and the same claim where dropping it changes the ANSWER rather than the
# list: this pattern's only match is the zero-segment one.
compare_tree "** whose only match is the zero-segment one" 'spec/**/[a].json'
# A `**` that is not a whole component is an ordinary wildcard and must NOT
# recurse. Downgrading the recursive form to this is the thing that stayed
# forbidden when the refusal was lifted.
compare_tree "non-component ** stays a plain wildcard" 'spec/re**s/*.json'
# A trailing separator matches directories only; _expand_files then drops all of
# them, so this is a no-match diagnostic and exit 1 — not an empty clean pass.
compare_tree "**/ matches directories only" 'spec/**/'
# A trailing `**` yields the directory itself plus every descendant, files and
# directories alike; the directories are filtered out downstream.
compare_tree "trailing **"                'spec/**'
compare_tree "bare **"                    '**'
compare_tree "leading **"                 '**/*.json'
compare_tree "leading ** with a trailing slash" '**/'
# The hidden rule belongs to the wildcard, not to the path: `**` never descends
# into spec/.secret, but naming it literally still reaches inside.
compare_tree "hidden directory named literally" 'spec/**/.secret/*.json'
# Python does not de-duplicate across two recursive components. Being tidier
# than the oracle is a divergence like any other.
compare_tree "two ** components duplicate matches" 'spec/**/**/*.json'
# A missing root is a no-match. Note this case does NOT pin the existence guard
# in globRecursive — it answers empty either way, since the trailing `*.json`
# finds nothing inside a directory that does not exist.
compare_tree "** under a missing directory" 'nope/**/*.json'
# The two shapes where the recursive component is LAST, covering both reasons
# isDir can be false: the path is missing, and the path exists but is a regular
# file. At THIS layer they are ordinary agreement checks — the guard is not
# observable through the CLI at all, because ExpandFiles' isFile filter drops
# `nope/` and `spec/a.json/` before either side prints anything, so removing the
# guard leaves the harness fully green. The guard is pinned in pyglob_test.go
# (`nope/**`, `spec/a.json/**`), which calls PyGlob directly and does see it.
compare_tree "trailing ** under a missing directory" 'nope/**'
compare_tree "trailing ** rooted at a file"          'spec/a.json/**'
compare_tree "** mixed with an ordinary pattern" 'spec/**/*.json' 'spec/*.json'
compare_tree "** mixed with a no-match"   'spec/**/*.json' 'nope/**/*.json'

# _expand_files is mode-agnostic (bin/validate-intent:457-464), so `**` is not
# an adopter-mode feature: --source globs through the very same expander. These
# are the README's own quickstart lines (README.md:46, 52, 56), and the second
# one is where the symlink matters most — capture_spec.rb is reported twice,
# once by its real path and once through spec/linked.
compare_tree "** under --source"          --source 'spec/**/*_spec.rb'
compare_tree "** under -s"                -s 'spec/**/*_spec.rb'
compare_tree "** under --source --json"   --source 'spec/**/*_spec.rb' --json

# Finally, from the repo root and over the shipped corpus: the exact invocation
# the README documents, which until this slice answered exit 2 with a refusal
# where the reference answered exit 1 with a verdict. Note the shape of that
# old divergence — 2 is "usage error, no verdict produced", so a CI step keyed
# on "non-zero means violations" read the refusal as a lint failure.
compare "recursive glob over the shipped examples" 'examples/**/*.json'
compare "recursive glob, source files"     --source 'examples/**/*.rb'

# --------------------------------------------------------------------------- #
# 6. no-match diagnostics — never a silent pass
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
# 7. --help
# --------------------------------------------------------------------------- #
#
# This is the ONE place the port's stdout is deliberately a SUPERSET of the
# reference's, and the arrangement below is what keeps that from costing any
# coverage.
#
# The block both implementations share is `usage` (cmd/validate-intent/main.go)
# and `USAGE` (bin/validate-intent). Appended to it on the --help path — and on
# no other path — is a Go-only trailer documenting `--version` (excluded group
# 5, cmd/validate-intent/version.go) and `--schema-source` (excluded group 6,
# cmd/validate-intent/schemasource.go). The first is what scripts/install.sh
# runs to prove the artifact executes on the target host; the second is the only
# way to ask which schema a run on this host actually enforces. On a host
# holding one binary and no repo, `--help` is the whole documentation set.
#
# compare_help does NOT relax the comparison to accommodate that. It splits the
# port's stdout at the EXACT byte length of the reference's and holds both
# halves to an exact expectation:
#
#   * the PREFIX must equal the reference's stdout byte-for-byte — so the two
#     shared texts still can only move together, and either one moving alone is
#     still red. That is the whole reason this comparison is worth having, and
#     it is why the shared last line was corrected in both files in one commit
#     (SPGD-279) rather than by loosening anything here;
#   * the REMAINDER must equal $WORK/help-trailer.expected, exactly. Not matched
#     by pattern, not merely stripped: a stripped trailer is an unchecked one,
#     and the house rule in this file is that an excluded surface is still
#     asserted.
#
# Note what confining the trailer to --help protects. The refusal paths print
# the shared block ALONE, and three of those are compared: `--source` with no
# FILE and `-s` with no FILE (section 10), `--json` in self-test mode
# (section 11), `--source --json` with no FILE (section 14). Had the --version
# row gone into the shared constant instead, all of them would have gone red
# alongside this section — and no matched edit to bin/validate-intent could
# have restored them, because the reference has no --version to document. It
# reads the flag as a filename and reports "no file(s) match".

# The Go-only trailer, written out here so the check below is an equality
# against a stated expectation rather than a shape.
cat > "$WORK/help-trailer.expected" <<'TRAILER'

Go port only — the Python reference has no such flags:

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
TRAILER

# compare_help <label> [args...] — for invocations where --help wins and the
# usage block goes to STDOUT. stderr and the exit code are compared exactly as
# `compare` does; stdout is split as described above.
compare_help() {
  local label="$1"
  shift

  local py_rc go_rc
  (cd "$REPO_ROOT" && "$PYTHON" "$REFERENCE" "$@" >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  (cd "$REPO_ROOT" && "$GO_BIN" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
  go_rc=$?

  local shared
  shared="$(wc -c < "$WORK/py.out" | tr -d ' ')"
  head -c "$shared" "$WORK/go.out" > "$WORK/go.shared"
  tail -c "+$((shared + 1))" "$WORK/go.out" > "$WORK/go.trailer"

  local problems=()
  [ "$py_rc" = "$go_rc" ] || problems+=("exit code: python=$py_rc go=$go_rc")
  cmp -s "$WORK/py.err" "$WORK/go.err" || problems+=("stderr differs")
  cmp -s "$WORK/py.out" "$WORK/go.shared" \
    || problems+=("the shared usage block differs")
  cmp -s "$WORK/help-trailer.expected" "$WORK/go.trailer" \
    || problems+=("the Go-only --version trailer differs")

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
  if ! cmp -s "$WORK/py.out" "$WORK/go.shared"; then
    printf '        --- shared usage block (-python +go) ---\n'
    diff -u "$WORK/py.out" "$WORK/go.shared" | tail -n +3 | sed 's/^/        /'
  fi
  if ! cmp -s "$WORK/help-trailer.expected" "$WORK/go.trailer"; then
    printf '        --- Go-only trailer (-want +got) ---\n'
    diff -u "$WORK/help-trailer.expected" "$WORK/go.trailer" | tail -n +3 | sed 's/^/        /'
  fi
  if ! cmp -s "$WORK/py.err" "$WORK/go.err"; then
    printf '        --- stderr (-python +go) ---\n'
    diff -u "$WORK/py.err" "$WORK/go.err" | tail -n +3 | sed 's/^/        /'
  fi
  return 1
}

echo
echo "== help =="
compare_help "--help"                   --help
compare_help "-h"                       -h
compare_help "--help wins over a file"  'examples/*.json' --help
compare_help "--help wins over a missing file" nope.json -h
# --help wins over --version too, in either order. This is a real COMPARISON and
# not a Go-side assertion, which is the whole reason it is worth having:
# `--version` is a Go-only flag (excluded group 5), so the tempting move is to
# check it only against the port. But the reference has an answer here — its own
# --help loop pre-empts the argument before anything reads it as a filename — so
# the two agree on the shared block byte-for-byte, and a port that let --version
# win would go red against the oracle rather than against a hand-written
# expectation that could encode the same mistake twice. What the port prints
# here is the usage block plus the trailer — never the version line — so a
# regression that let the flag win is caught on both halves of the split.
compare_help "--help wins over --version"        --help --version
compare_help "--help wins over --version (reversed)" --version --help
compare_help "-h wins over --version"            --version -h
# The same crossing for the second Go-only surface (excluded group 6). It is
# worth its own three cases rather than being assumed to follow from the three
# above: `--schema-source` is answered by a SEPARATE loop in run(), below
# --version's, so "help wins" is a property of where that loop sits and not of a
# rule the file applies once. And unlike --version this flag TOUCHES THE DISK —
# a port that let it win here would not merely print the wrong thing, it would
# make `--help` fail on a tree whose schema cannot be loaded, which is precisely
# the tree an adopter reaches for --help on.
compare_help "--help wins over --schema-source"        --help --schema-source
compare_help "--help wins over --schema-source (reversed)" --schema-source --help
compare_help "-h wins over --schema-source"            --schema-source -h

# --------------------------------------------------------------------------- #
# 8. OS-level failures
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

# A schema that fails to LOAD. Both implementations derive the schema path from
# their own location, so copying both into a tree carrying a schema of our
# choosing makes them compute the same path and read the same bytes.
#
# This was a tree with NO schemas/ directory at all until SPGD-131. It cannot be
# any more: the Go port now embeds the schema (schema.go) and falls back to that
# copy when the file is ABSENT, so a schema-less tree is exactly where the two
# are supposed to diverge — that is excluded group 4, and what the Go binary
# does there instead is pinned in section 16 ("Go-side refusals — the excluded
# surfaces, still asserted").
#
# A MALFORMED schema replaces it, and is a strictly better probe:
#
#   * it still exercises the schema-load failure path in both implementations,
#     byte for byte, JSONDecodeError prose included;
#   * it is the case the new fallback must NOT swallow. A file that exists is a
#     deliberate override; falling back past a broken one would turn the user's
#     own typo into a clean green run against a schema they never wrote — a tool
#     failure wearing the costume of a content failure, and the more dangerous
#     kind because it SUCCEEDS;
#   * unlike the unreadable-schema case below it needs no permission bit, so it
#     does not evaporate into a SKIP when this suite runs as root. That matters
#     more here than anywhere else in the file: root is precisely where a check
#     of "the fallback does not fire on a broken schema" would otherwise be
#     established by watching a case that never ran fail to complain.
badschema_root="$WORK/badschema-json"
mkdir -p "$badschema_root/bin" "$badschema_root/schemas"
cp "$REFERENCE" "$badschema_root/bin/validate-intent"
cp "$GO_BIN" "$badschema_root/bin/validate-intent-go"
printf '{ "type": "object",\n' > "$badschema_root/schemas/open-test-intent.v1.json"
cp "$REPO_ROOT/examples/unit-order-total.json" "$badschema_root/thing.json"
compare_in "$badschema_root" "$badschema_root/bin/validate-intent" \
  "$badschema_root/bin/validate-intent-go" "malformed schema (exit 2)" thing.json

# The crossing of the two exit-2 paths, which neither one alone covers.
#
# The harness compares schema-load failures (just above) and it compares the
# self-test `--json` refusal (section 11, "self-test (bare invocation)") — but
# for a long time never both at once, and that is precisely where the port
# drifted: main.go originally refused `--json` BEFORE loading the schema, so on
# a tree whose schema would not load it answered "--json is not supported in
# self-test mode" plus the usage block where Python answers "could not load
# schema ...". Same exit code, different stderr, and no case in the suite that
# would notice. Both orderings are exercised here.
#
# Read this next part before touching the tree above. This crossing only has
# teeth while the schema genuinely fails to load in BOTH implementations. On a
# schema-LESS tree it no longer does — Go finds its embedded copy, the load
# succeeds, and the case would pass whichever side of LoadSchema the `--json`
# refusal sat on. Moving these cases to a malformed schema is what keeps the
# assertion sharp; putting them back on a schema-less tree would leave five
# green cases that had quietly stopped testing the thing they are named for.
compare_in "$badschema_root" "$badschema_root/bin/validate-intent" \
  "$badschema_root/bin/validate-intent-go" "malformed schema + bare --json (schema load wins)" --json
compare_in "$badschema_root" "$badschema_root/bin/validate-intent" \
  "$badschema_root/bin/validate-intent-go" "malformed schema + self-test (no args)"
compare_in "$badschema_root" "$badschema_root/bin/validate-intent" \
  "$badschema_root/bin/validate-intent-go" "malformed schema + --source --json" --source thing.json --json
compare_in "$badschema_root" "$badschema_root/bin/validate-intent" \
  "$badschema_root/bin/validate-intent-go" "malformed schema + --source with no argument" --source

# The counterweight to the embedded fallback: a schema that IS present must
# still be the one that governs.
#
# The 21 compare_root cases below already depend on this, but they depend on it
# implicitly — each places a schema and would go red if it were ignored, which
# makes them a good regression net and a poor statement of intent. This case
# states it, and states it in the only form that cannot be satisfied by
# accident: the override ACCEPTS a document the canonical schema REJECTS. `{}`
# has none of the four required properties, so a binary that quietly used its
# embedded copy would answer FAIL/1 here where the override says PASS/0. An
# override that merely differed could be passed by agreeing.
#
# It is a comparison rather than a Go-side assertion because Python reads the
# same override, so both must accept `{}` — which also pins that the fallback
# introduced no divergence on the path where a file exists.
override_root="$(make_schema_root override-beats-embedded <<'JSON'
{
  "type": "object"
}
JSON
)"
printf '{}\n' > "$override_root/empty.json"
compare_root "$override_root" "an on-disk schema beats the embedded copy (it accepts {})" empty.json

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
# 9. a grown schema — the validator keywords the shipped one does not declare
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
    "grouped": {"type": "string", "pattern": "(?:^)+[a-z]+(^)*$"},
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

# A quantifier on a *grouped* anchor is legal in Python and in RE2 alike, so it
# must survive the refusal that rejects the bare `^*` form (section 17,
# "Go-side refusals — schemas carrying a pattern RE2 cannot reproduce"). Both
# documents are compared, so an over-refusal here is a loud failure rather than
# a silently smaller accepted language: the port would exit 2 where the
# reference answers.
cat > "$grown/grouped-anchor.json" <<'JSON'
{"slug": "a", "grouped": "abc"}
JSON
compare_root "$grown" "grown: quantified grouped anchor, matching" grouped-anchor.json

cat > "$grown/grouped-anchor-bad.json" <<'JSON'
{"slug": "a", "grouped": "AB1"}
JSON
compare_root "$grown" "grown: quantified grouped anchor, failing" grouped-anchor-bad.json

# Key order again, this time over the grown keyword set: every one of these
# fails, and the order of the failures must track the document.
cat > "$grown/key-order.json" <<'JSON'
{"zzz": 1, "ratio": 0.1, "aaa": 2, "count": 99, "name": "x", "slug": "A"}
JSON
compare_root "$grown" "grown: key order across the new keywords" key-order.json

compare_root "$grown" "grown: every fixture in one invocation" '*.json'

# --------------------------------------------------------------------------- #
# 10. --source over the shipped corpus
# --------------------------------------------------------------------------- #
#
# The mode's own acceptance criteria: every valid source fixture, every invalid
# one, each file individually and as globs. The em dash (U+2014) in
# "correctly rejected — ..." and "FAIL file:line — problem" is load-bearing
# output, so these comparisons are also what pins the port's non-ASCII bytes.
echo
echo "== --source over the shipped corpus =="
compare "--source examples/sources/*"          --source 'examples/sources/*'
compare "--source examples/sources/invalid/*"  --source 'examples/sources/invalid/*'
compare "-s alias"                             -s 'examples/sources/*'
for fixture in "$REPO_ROOT"/examples/sources/*.* "$REPO_ROOT"/examples/sources/invalid/*; do
  [ -f "$fixture" ] || continue
  rel="${fixture#"$REPO_ROOT"/}"
  compare "--source $rel" --source "$rel"
done
# Both sets in one invocation: output order and exit-code aggregation.
compare "--source valid then invalid" \
  --source 'examples/sources/*' 'examples/sources/invalid/*'
compare "--source invalid then valid" \
  --source 'examples/sources/invalid/*' 'examples/sources/*'
# --source shares _run_over_patterns with adopter mode, so its no-match
# diagnostic and repr'd pattern must match too.
compare "--source no-match"          --source 'nope/*.rb'
compare "--source no-match mixed"    --source 'examples/sources/*' 'nope/*.rb'
compare "--source a directory"       --source 'examples/sources'
# --source with no FILE argument is a usage error (exit 2) with USAGE on stderr.
compare "--source with no argument"  --source
compare "-s with no argument"        -s
# A JSON file read as *source* carries no @intent token: the `----` line, which
# is not a failure.
compare "--source over a .json file" --source examples/unit-order-total.json
# Reading a source file as adopter JSON is the mirror image, and now compares
# byte for byte including json.JSONDecodeError's wording (retired exclusion).
compare "adopter over a .rb file"    examples/sources/order_spec.rb

# --------------------------------------------------------------------------- #
# 11. self-test (bare invocation)
# --------------------------------------------------------------------------- #
#
# 24 PASS lines then "12/12 fixtures matched expectation." Those two numbers
# count different things ON PURPOSE — `checked` counts FIXTURES (8 JSON + 4
# source files) while the source fixtures print one line per ANNOTATION. A port
# that "fixed" the arithmetic would fail right here.
echo
echo "== self-test =="
compare "bare invocation (self-test)"
# --json is refused in self-test mode by the REFERENCE (exit 2 + usage on
# stderr), so this is a reproduced refusal, not a port limitation — which is why
# it is compared rather than listed as a Go-side-only assertion.
compare "self-test refuses --json" --json

# --------------------------------------------------------------------------- #
# 12. malformed payloads and documents — the retired exclusions
# --------------------------------------------------------------------------- #
#
# Slice 1 excluded json.JSONDecodeError's wording and the NaN/Infinity literals.
# Slice 2 needed the first closed (an unparseable annotation payload is the
# commonest real `--source` failure) and got the second for free, so both are
# now compared byte for byte — message, line, column and character offset.
#
# The char offset is where DIVERGENCE 4 becomes observable. Every offset CPython
# reports is an index into the decoded str, i.e. a CODE POINT offset. The scanner
# functions themselves turn out to be latent under byte indexing — every byte of
# a multi-byte UTF-8 sequence is >= 0x80, so none can be mistaken for a quote or
# a brace — but the decoder's offsets are not: a payload carrying `café` before
# the syntax error reports one lower char offset than the same payload carrying
# `ascii`, and a byte-indexed decoder gets that wrong by exactly the number of
# non-ASCII characters preceding it.
echo
echo "== malformed payloads and documents =="

malformed="$WORK/malformed"
mkdir -p "$malformed"

cat > "$malformed/truncated.json" <<'JSON'
{"entity": "Order",
JSON
compare "adopter: truncated JSON document" "$malformed/truncated.json"

printf '{"entity": "Order" "action": "create"}\n' > "$malformed/missing-comma.json"
compare "adopter: missing comma" "$malformed/missing-comma.json"

printf '{entity: "Order"}\n' > "$malformed/bare-key.json"
compare "adopter: bare key is not JSON" "$malformed/bare-key.json"

printf '{"entity": "Order",}\n' > "$malformed/trailing-comma.json"
compare "adopter: trailing comma" "$malformed/trailing-comma.json"

printf '\n\n  {"entity" 1}\n' > "$malformed/multiline.json"
compare "adopter: error on line 3 (lineno/colno)" "$malformed/multiline.json"

# A non-ASCII character BEFORE the error: the char offset is code points.
printf '{"entity": "caf\303\251", "action" 1}\n' > "$malformed/nonascii-offset.json"
compare "adopter: char offset counts code points" "$malformed/nonascii-offset.json"
printf '{"entity": "ascii", "action" 1}\n' > "$malformed/ascii-offset.json"
compare "adopter: the same document in ASCII (offset differs by design)" \
  "$malformed/ascii-offset.json"

# The retired NaN/Infinity exclusion: Python parses these and reports a SCHEMA
# violation; a port that rejected them would report a parse failure instead.
printf '{"entity": NaN, "action": Infinity, "behavior": -Infinity, "layer": "unit"}\n' \
  > "$malformed/nonfinite.json"
compare "adopter: NaN/Infinity parse and fail on schema grounds" "$malformed/nonfinite.json"

# Unterminated and invalid string escapes.
printf '{"entity": "Order\n' > "$malformed/unterminated-string.json"
compare "adopter: unterminated string" "$malformed/unterminated-string.json"
printf '{"entity": "a\\qb"}\n' > "$malformed/bad-escape.json"
compare "adopter: invalid backslash escape" "$malformed/bad-escape.json"
printf '{"entity": "a\\u12"}\n' > "$malformed/bad-uescape.json"
compare 'adopter: invalid \uXXXX escape' "$malformed/bad-uescape.json"
printf '{"entity": "Order"} trailing\n' > "$malformed/extra-data.json"
compare "adopter: extra data after the document" "$malformed/extra-data.json"
printf '\n' > "$malformed/empty.json"
compare "adopter: an empty document" "$malformed/empty.json"

# --------------------------------------------------------------------------- #
# 13. --source hazards: the four divergences, the thin ice, the known defect
# --------------------------------------------------------------------------- #
#
# These fixtures are BUILT HERE with printf rather than checked in, because most
# of them turn on characters that are invisible in a file (\v, \x1d, \x1f, NEL).
# A reviewer can see the exact bytes and the reason for them side by side; in a
# checked-in fixture they would be a mystery nobody could verify by reading.
echo
echo "== --source hazards =="

SRC="$WORK/sources"
mkdir -p "$SRC"

# -- DIVERGENCE 1: str.splitlines() ----------------------------------------- #
# The first physical line carries \v (0x0b), \f (0x0c), \x1d and NEL (U+0085).
# Python's splitlines() breaks on ALL of them, so the annotation that follows is
# on line 6. strings.Split(text, "\n") sees ONE line, and would report line 2 —
# a specific, confident, wrong file:line pointing at innocent code.
printf 'a\013b\014c\035d\302\205e\n# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/splitlines_spec.rb"
compare "divergence 1: \\v \\f \\x1d NEL shift the line number" --source "$SRC/splitlines_spec.rb"

# U+2028 / U+2029 are terminators to splitlines() too, and are multi-byte — so
# this also exercises the terminator scan over runes rather than bytes.
printf 'x\342\200\250y\342\200\251z\n# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/splitlines_unicode_spec.rb"
compare "divergence 1: U+2028/U+2029 are terminators too" --source "$SRC/splitlines_unicode_spec.rb"

# \r\n counts as ONE terminator, and a trailing terminator yields no extra line.
printf 'one\r\ntwo\r\n# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\r\n' \
  > "$SRC/splitlines_crlf_spec.rb"
compare "divergence 1: CRLF is one terminator" --source "$SRC/splitlines_crlf_spec.rb"

# -- DIVERGENCE 2: str.isspace() -------------------------------------------- #
# Python's isspace() set is Go's unicode.IsSpace PLUS U+001C-U+001F. Of those
# four, U+001C/1D/1E are ALSO splitlines() terminators, so they can never survive
# inside a single line and can never reach normalize_payload's lookaheads.
# U+001F is the only character in the delta that can — which makes it the whole
# of divergence 2's reachable surface, and both lookaheads are exercised with it.
#
# Line 1: the trailing-comma lookahead. With isspace() the comma is dropped;
# without it the comma stays, and the two produce DIFFERENT parse errors.
# Line 2: the bare-word-key lookahead. With isspace() `entity` is recognised as a
# key and quoted; without it, it is left bare — again a different parse error.
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit"\037, }\n# @intent: { entity\037: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/isspace_spec.rb"
compare "divergence 2: U+001F is whitespace to Python, not to Go" --source "$SRC/isspace_spec.rb"

# The three delta characters that ARE line terminators, proving the claim above:
# they cut the annotation in half rather than reaching the lookahead.
printf '# @intent: { entity\034: "Order" }\n# @intent: { entity\036: "Order" }\n' \
  > "$SRC/isspace_terminators_spec.rb"
compare "divergence 2: U+001C/U+001E terminate the line instead" \
  --source "$SRC/isspace_terminators_spec.rb"

# -- DIVERGENCE 3: json.dumps() --------------------------------------------- #
# _BARE_WORD_RE is [A-Za-z_$][A-Za-z0-9_$]*, i.e. ASCII — so `café` matches only
# `caf`, and `a<b>&c` matches `a`, `b` and `c` separately with only `c` in key
# position (normalizing to `{a<b>&"c": "y"}`). Both then fail to parse, and the
# exact JSONDecodeError is the assertion.
printf '# @intent: { caf\303\251: "x" }\n# @intent: { a<b>&c: "y" }\n' \
  > "$SRC/bareword_spec.rb"
compare "divergence 3: café and a<b>&c bare-word keys" --source "$SRC/bareword_spec.rb"

# The same two characters where json.dumps' escaping WOULD show, if the bare-word
# class ever widened: inside a quoted key and a quoted value, passed through
# untouched by the normalizer.
printf '# @intent: { "caf\303\251": "a<b>&c", entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/escaping_spec.rb"
compare "divergence 3: non-ASCII and <>& survive quoting untouched" \
  --source "$SRC/escaping_spec.rb"

# -- DIVERGENCE 4: code-point vs byte indexing ------------------------------ #
# A backslash escape immediately before a multi-byte character — the `i += 2`
# at bin/validate-intent:275 that steps over one CODE POINT in Python and would
# step over one BYTE in a naive port.
printf '# @intent: { entity: "Order", action: "a\\\\\303\251b", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/escape_multibyte_spec.rb"
compare "divergence 4: backslash before a multi-byte character" \
  --source "$SRC/escape_multibyte_spec.rb"

# The observable half: a parse error whose char offset follows non-ASCII text.
# The ASCII twin is compared alongside it so the offsets are visibly different
# numbers rather than a single value nobody can check.
printf '# @intent: { entity: "caf\303\251", action: xyz, behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/offset_nonascii_spec.rb"
printf '# @intent: { entity: "ascii", action: xyz, behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/offset_ascii_spec.rb"
compare "divergence 4: parse-error offset after non-ASCII" --source "$SRC/offset_nonascii_spec.rb"
compare "divergence 4: the ASCII twin"                     --source "$SRC/offset_ascii_spec.rb"

# -- THE THIN ICE: brace-balanced capture ----------------------------------- #
# examples/sources/order_spec.rb:57 is the shipped instance; these isolate the
# shape. A greedy regex captures through the trailing `{§3}` and fails; a lazy
# one truncates the payload mid-object. Only a balanced, string-aware scan is
# right. (The captured substring itself is asserted directly in
# cmd/validate-intent/source_test.go — a PASS line alone would not catch a port
# that swallowed the tail and still validated.)
printf '# @intent: { entity: "Order", action: "refund", behavior: "restores stock levels when a paid order is refunded", layer: "unit" } \342\200\224 see ADR-14 {\302\2473} for why.\n' \
  > "$SRC/brace_tail_spec.rb"
compare "thin ice: trailing prose with its own brace pair" --source "$SRC/brace_tail_spec.rb"

# Braces and quotes INSIDE the payload's own strings, plus a nested object and
# array, plus the permissive forms the normalizer exists for.
cat > "$SRC/permissive_spec.rb" <<'RUBY'
# @intent: {'entity': 'Order', 'action': 'create', 'behavior': "it's got a { brace } and a ] bracket", 'layer': 'unit',}
# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit", preconditions: ["a cart exists", "the cart has items",] }
# @intent: {entity:"Order",action:"create",behavior:"creates an order from a valid cart",layer:"unit"}
RUBY
compare "permissive syntax: quotes, trailing commas, nested brackets" \
  --source "$SRC/permissive_spec.rb"

# Two annotations on ONE line: extract_intents resumes scanning at `pos = end`.
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" } and @intent: { entity: "Cart", action: "empty", behavior: "empties the cart when the order is placed", layer: "unit" }\n' \
  > "$SRC/two_on_one_line_spec.rb"
compare "two annotations on one line" --source "$SRC/two_on_one_line_spec.rb"

# Extraction failures: no brace at all, an unterminated object, an unterminated
# string, and unbalanced brackets — the four ValueError paths.
cat > "$SRC/extraction_failures_spec.rb" <<'RUBY'
# @intent: no object literal here
# @intent: { entity: "Order", action: "create"
# @intent: { entity: "Order }
# @intent: { entity: "Order"]
RUBY
compare "extraction failures: all four problem strings" \
  --source "$SRC/extraction_failures_spec.rb"

# A file with no annotations at all: the `----` line, exit 0, not a failure.
printf 'describe Order do\n  it "works" do\n  end\nend\n' > "$SRC/unannotated_spec.rb"
compare "a file with no annotations is not a failure" --source "$SRC/unannotated_spec.rb"

# -- THE KNOWN DEFECT, REPRODUCED ------------------------------------------- #
# extract_intents cannot tell an @intent: inside a STRING LITERAL from one in a
# real comment. The phantom and the real comment produce BYTE-IDENTICAL output —
# and that identity is the proof the extractor cannot distinguish them. The port
# must INHERIT this: "fixing" it here would fail parity, and changing it is a
# protocol-level decision for bin/validate-intent first.
#
# PROBE HYGIENE (this cost the ticket's author real time): the host must be
# SINGLE-quoted. A double-quoted Ruby host makes the line carry backslash-escaped
# {\"entity\"...}, and the validator answers "unterminated object literal" —
# which reads exactly like the defect being absent. It is not; the payload never
# reaches the string-literal question. The positive control runs alongside so
# the identity is asserted, not assumed.
printf "x = '# @intent: { entity: \"Order\", action: \"create\", behavior: \"creates an order from a valid cart\", layer: \"unit\" }'\n" \
  > "$SRC/phantom_spec.rb"
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/real_comment_spec.rb"
compare "known defect: @intent inside a string literal still PASSes" --source "$SRC/phantom_spec.rb"
compare "positive control: the same annotation in a real comment"   --source "$SRC/real_comment_spec.rb"
# The identity itself: strip the differing filename and require the rest to be
# byte-identical between the two. Without this the pair above could both be
# green while the port quietly started telling them apart.
"$PYTHON" "$REFERENCE" --source "$SRC/phantom_spec.rb" 2>/dev/null \
  | sed 's#.*phantom_spec.rb#FILE#' > "$WORK/phantom.py"
"$GO_BIN" --source "$SRC/real_comment_spec.rb" 2>/dev/null \
  | sed 's#.*real_comment_spec.rb#FILE#' > "$WORK/real.go"
if cmp -s "$WORK/phantom.py" "$WORK/real.go"; then
  passed=$((passed + 1))
  printf '  ok    known defect: phantom and real comment are indistinguishable\n'
else
  failed=$((failed + 1))
  red "  FAIL  known defect: the port started distinguishing a string literal from a comment"
  diff -u "$WORK/phantom.py" "$WORK/real.go" | tail -n +3 | sed 's/^/        /'
fi

# A file that cannot be read: check_source_file's prefix is "could not read
# file:", NOT check_file's "could not read/parse JSON:".
unreadable_src="$SRC/unreadable_spec.rb"
cp "$SRC/real_comment_spec.rb" "$unreadable_src"
chmod 000 "$unreadable_src"
if [ -r "$unreadable_src" ]; then
  skipped=$((skipped + 1))
  red "  SKIP  unreadable source — chmod 000 did not make it unreadable (running as root?)"
else
  compare "--source over an unreadable file" --source "$unreadable_src"
fi
chmod 644 "$unreadable_src"

# --------------------------------------------------------------------------- #
# 14. --source --json
# --------------------------------------------------------------------------- #
#
# The whole rendering path is new in slice 2 (slice 1 has no reporter to reuse —
# it refuses --json outright), so every shape of the document is compared:
# passing findings, schema findings, extraction findings, parse findings, a
# no-match finding, a read finding, and the file-with-no-annotations rule.
#
# json.dumps(indent=2) is reproduced by hand rather than with encoding/json,
# which escapes <>& and does NOT escape non-ASCII — the exact opposite of
# json.dumps on both counts. The em dashes in the extraction messages make that
# visible here rather than theoretical.
echo
echo "== --source --json =="
compare "--json: all passing"        --source examples/sources/order_spec.rb --json
compare "--json: mixed failures"     --source 'examples/sources/invalid/*' --json
compare "--json: the whole corpus"   --source 'examples/sources/*' 'examples/sources/invalid/*' --json
compare "--json: position-independent (leading)" --json --source 'examples/sources/*'
compare "--json: a file with no annotations"     --source "$SRC/unannotated_spec.rb" --json
compare "--json: no-match becomes a finding"     --source 'nope/*.rb' --json
compare "--json: no-match mixed with matches"    --source 'examples/sources/*' 'nope/*.rb' --json
compare "--json: extraction failures"            --source "$SRC/extraction_failures_spec.rb" --json
compare "--json: parse failures (em dash + escaping)" --source "$SRC/bareword_spec.rb" --json
compare "--json: non-ASCII in an error string"   --source "$SRC/offset_nonascii_spec.rb" --json
# --source --json with no FILE argument is still the usage error, not an empty
# document: a consumer must not get a clean-looking `"ok": true` for a run that
# was never given anything to check.
compare "--json: --source with no argument"      --source --json
chmod 000 "$unreadable_src"
if [ -r "$unreadable_src" ]; then
  skipped=$((skipped + 1))
  red "  SKIP  --json read finding — chmod 000 did not make the file unreadable (running as root?)"
else
  compare "--json: an unreadable file is a read finding" --source "$unreadable_src" --json
fi
chmod 644 "$unreadable_src"

# --------------------------------------------------------------------------- #
# 15. the self-test's empty-fixture-set guard
# --------------------------------------------------------------------------- #
#
# A fixture set matching NOTHING must be an error, not a vacuous pass: dropping
# examples/invalid/ alone turns 12/12 into a greener-reading 8/8 with the
# validator's ability to reject anything now wholly unexercised. Each set is
# guarded independently, every empty one is reported, and the run fails before
# checking anything — so these trees assert the diagnostics as well as the exit
# code.
echo
echo "== self-test empty-fixture-set guard =="

# make_fixture_root <name> — a tree holding both implementations, the real
# schema, and whichever fixture directories the caller then populates.
make_fixture_root() {
  local root="$WORK/$1"
  mkdir -p "$root/bin" "$root/schemas"
  cp "$REFERENCE" "$root/bin/validate-intent"
  cp "$GO_BIN" "$root/bin/validate-intent-go"
  cp "$REPO_ROOT/schemas/open-test-intent.v1.json" "$root/schemas/"
  printf '%s' "$root"
}

# Nothing at all: all four sets empty, all four diagnostics.
bare_root="$(make_fixture_root selftest-bare)"
compare_root "$bare_root" "self-test: every fixture set empty"

# Only the valid JSON examples present: the other three are reported.
partial_root="$(make_fixture_root selftest-partial)"
mkdir -p "$partial_root/examples"
cp "$REPO_ROOT"/examples/*.json "$partial_root/examples/"
compare_root "$partial_root" "self-test: three sets empty"

# Everything present: the full green path, in a tree of our own.
full_root="$(make_fixture_root selftest-full)"
mkdir -p "$full_root/examples/invalid" "$full_root/examples/sources/invalid"
cp "$REPO_ROOT"/examples/*.json "$full_root/examples/"
cp "$REPO_ROOT"/examples/invalid/*.json "$full_root/examples/invalid/"
cp "$REPO_ROOT"/examples/sources/*.* "$full_root/examples/sources/" 2>/dev/null
cp "$REPO_ROOT"/examples/sources/invalid/* "$full_root/examples/sources/invalid/"
compare_root "$full_root" "self-test: a complete fixture tree elsewhere"

# A source fixture with ZERO extractable annotations must be a MISMATCH, not a
# pass — that is what a silently-broken extractor looks like.
noann_root="$(make_fixture_root selftest-noann)"
mkdir -p "$noann_root/examples/invalid" "$noann_root/examples/sources/invalid"
cp "$REPO_ROOT"/examples/*.json "$noann_root/examples/"
cp "$REPO_ROOT"/examples/invalid/*.json "$noann_root/examples/invalid/"
cp "$REPO_ROOT"/examples/sources/*.* "$noann_root/examples/sources/" 2>/dev/null
cp "$REPO_ROOT"/examples/sources/invalid/* "$noann_root/examples/sources/invalid/"
printf 'describe Order do\nend\n' > "$noann_root/examples/sources/unannotated_spec.rb"
compare_root "$noann_root" "self-test: a fixture with no annotations is a mismatch"

# --------------------------------------------------------------------------- #
# 16. Go-side refusals — the excluded surfaces, still asserted
# --------------------------------------------------------------------------- #
#
# These are the inputs listed as excluded in the header. They are not compared
# against Python (the outputs differ by design), but "excluded" must not mean
# "unchecked": each is pinned here to what the port does instead.
#
# Most of them are REFUSALS — exit 2, a diagnostic on stderr, nothing on stdout
# — and assert_refusal below is the helper for those. The failure that shape
# guards against is a fall-through: `--source foo.rb` reaching adopter mode and
# being reported as a filename that matched nothing, which looks like a real
# answer.
#
# One is NOT a refusal, and the distinction is deliberate rather than an
# oversight in the prose. `--version` (excluded group 5, slice 6 / SPGD-141) is
# a Go-only surface that SUCCEEDS: exit 0, a line on stdout, nothing on stderr.
# assert_refusal asserts the exact opposite of all three, so it could not be
# stretched to cover it — assert_version_line below is its counterpart, and it
# checks the same three streams with the expectations inverted, plus the one
# thing a refusal never has to prove: that the payload on stdout actually says
# something.
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

assert_refusal "stdin mode" -
assert_refusal "--json for adopter mode" --json 'examples/*.json'
assert_refusal "--json for adopter mode, anywhere on the line" 'examples/*.json' --json

# The mirror of the list above: the surfaces slice 2 IMPLEMENTED must NOT be
# refused any more. Without this, deleting a mode's dispatch would leave every
# comparison in sections 10-15 unrun and the refusal assertions still green — a
# suite that got smaller without going red.
assert_not_refused() {
  local label="$1"
  shift
  local rc
  (cd "$REPO_ROOT" && "$GO_BIN" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
  rc=$?
  if [ "$rc" -eq 2 ] && grep -q 'not implemented in the Go port' "$WORK/go.err"; then
    failed=$((failed + 1))
    red "  FAIL  $label — still refused as unimplemented"
    printf '        %s\n' "$(head -1 "$WORK/go.err")"
    return 1
  fi
  passed=$((passed + 1))
  printf '  ok    %s (implemented)\n' "$label"
  return 0
}

assert_not_refused "self-test mode is implemented"
assert_not_refused "source mode (--source) is implemented" --source 'examples/sources/*'
assert_not_refused "source mode (-s) is implemented" -s 'examples/sources/*'
assert_not_refused "--source --json is implemented" --source 'examples/sources/*' --json
# Slice 4's own entries. The refusals these replace were a WHOLE-ARGV precheck,
# so it was never enough to delete one of them: `**` had to stop being refused
# in adopter mode, in --source, and as a bare argument alike.
assert_not_refused "recursive glob is implemented" 'examples/**/*.json'
assert_not_refused "recursive glob under --source is implemented" --source 'examples/**/*.rb'
assert_not_refused "recursive glob, bare, is implemented" '**'

# --------------------------------------------------------------------------- #
# Excluded group 5: --version, the Go-only surface that SUCCEEDS
# --------------------------------------------------------------------------- #
#
# assert_refusal above cannot be reused for this one. It asserts exit 2, a
# non-empty stderr and an EMPTY stdout; `--version` is exit 0, an empty stderr
# and a non-empty stdout, so it fails all three by design. This is the inverted
# counterpart.
#
# It checks five things, and the last two are the ones that matter:
#
#   * exit 0 — the whole point of the flag. Before slice 6 (SPGD-141) this
#     surface exited 1, the code the contract reserves for "at least one
#     annotation is malformed", so `validate-intent --version || echo "not
#     installed"` reported a content failure for a tool failure;
#   * stderr empty — a version report is not a diagnostic;
#   * stdout is exactly ONE line, matching
#     `validate-intent <identity> (<goversion> <goos>/<goarch>) schema sha256:<hex>`;
#   * the IDENTITY is the right one. Not merely "non-empty" — an assertion that
#     the line is non-blank would pass on `validate-intent  (go1.22 linux/amd64)`
#     and on a binary reporting an unexpanded build template, which is this
#     project's house defect wearing a version number. The harness builds the
#     port with a plain `go build` and no ldflags (see the build step at the top
#     of this file), so tier 2 of resolveVersion applies and the identity must
#     be exactly the HEAD revision git reports, optionally suffixed `-dirty`.
#     That makes every run of this harness a live exercise of the VCS fallback
#     rather than a one-off claim made once at review time;
#   * the SCHEMA DIGEST is the right one, held to the same standard for the same
#     reason. "64 hex characters" is the version-number defect one field to the
#     right: a digest of the wrong file, of a stale embed, or of an empty asset
#     is 64 hex characters and answers the one question this token exists to
#     answer with a plausible lie. It is compared against the SHA-256 of
#     $REPO_ROOT/schemas/open-test-intent.v1.json — the canonical contract this
#     build was made from.
#
# Why $REPO_ROOT and not the cwd this function is handed. The three call sites
# pass three different working directories, and two of them would make a
# cwd-relative read of schemas/open-test-intent.v1.json actively wrong: from `/`
# (the install-prefix case) there is no such file, and from
# $versionbadschema_root there is one and it is DELIBERATELY CORRUPT. Reading it
# there would assert the binary reports the digest of a file it must not have
# read — inverting the check into a guarantee of failure.
#
# That third site is the gift, and it is used rather than tolerated: it is the
# one tree where the on-disk schema exists and cannot be loaded, so a reported
# digest that still equals the CANONICAL one is direct positive evidence that
# the answer came from the embedded copy with no disk access at all.
#
# Where the expectation cannot be computed — no git, or not a checkout, which is
# the extracted-tarball case where resolveVersion's third tier legitimately
# answers `unknown`; or no SHA-256 tool on PATH — the affected check degrades to
# shape only and says so, rather than silently asserting less.
echo
echo "== --version reports an identity (Go only) =="

want_identity=""
if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  want_identity="$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || true)"
fi
if [ -z "$want_identity" ]; then
  dim "note: not a git checkout — --version's identity is checked for shape only,"
  dim "      not against a revision (resolveVersion's third tier applies here)"
fi

# The canonical contract's digest, computed the same way scripts/install.sh
# computes one: sha256sum (GNU coreutils) or shasum -a 256 (BSD/macOS), in that
# order, because a stock macOS has no sha256sum and many minimal Linux images
# have no shasum. The result is validated as 64 lowercase hex before it is
# trusted as an expectation — an expectation silently set to "" would turn the
# comparison below into no comparison at all.
# sha256_hex <file> — the file's SHA-256 as 64 lowercase hex, or the empty
# string when it cannot be computed: no tool on PATH, an unreadable file, or
# output that is not a digest. Every caller must treat "" as "no expectation
# available" and say so, because an expectation silently set to "" turns the
# comparison it feeds into no comparison at all.
#
# A function rather than three copies of the same pipeline: this is now used for
# the canonical contract below and, in the --schema-source section, for two
# synthetic schemas whose digests must be computed the same way if comparing
# them is to mean anything.
sha256_hex() {
  local file="$1" sum=""
  [ -r "$file" ] || { printf ''; return; }
  if command -v sha256sum >/dev/null 2>&1; then
    sum="$(sha256sum "$file" 2>/dev/null || true)"
  elif command -v shasum >/dev/null 2>&1; then
    sum="$(shasum -a 256 "$file" 2>/dev/null || true)"
  fi
  sum="${sum%% *}"
  case "$sum" in
    *[!0-9a-f]*|"") sum="" ;;
  esac
  [ "${#sum}" -eq 64 ] || sum=""
  printf '%s' "$sum"
}

canonical_schema="$REPO_ROOT/schemas/open-test-intent.v1.json"
want_schema="$(sha256_hex "$canonical_schema")"
if [ -z "$want_schema" ]; then
  dim "note: no SHA-256 tool on PATH (neither sha256sum nor shasum), or"
  dim "      schemas/open-test-intent.v1.json is unreadable — --version's schema"
  dim "      digest is checked for shape only, not against the canonical contract"
fi

# assert_version_line_in <cwd> <bin> <label> [args...]
assert_version_line_in() {
  local cwd="$1" gobin="$2" label="$3"
  shift 3

  local rc
  (cd "$cwd" && "$gobin" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
  rc=$?

  local problems=()
  [ "$rc" -eq 0 ] || problems+=("exit code: want 0, got $rc")
  [ ! -s "$WORK/go.err" ] \
    || problems+=("stderr: want empty, got '$(head -c 80 "$WORK/go.err")'")

  local count
  count="$(wc -l < "$WORK/go.out" | tr -d ' ')"
  [ "$count" = "1" ] || problems+=("stdout: want exactly one line, got $count")

  local line identity schema
  line="$(head -1 "$WORK/go.out")"
  if [[ "$line" =~ ^validate-intent\ ([^[:space:]]+)\ \(go[^[:space:]]+\ [^[:space:]/]+/[^[:space:]/]+\)\ schema\ sha256:([0-9a-f]{64})$ ]]; then
    identity="${BASH_REMATCH[1]}"
    schema="${BASH_REMATCH[2]}"
    case "$identity" in
      *'$'*|*'{'*|*'}'*|*'%!'*)
        problems+=("identity '$identity' looks like an unexpanded build template") ;;
    esac
    if [ -n "$want_identity" ] \
       && [ "$identity" != "$want_identity" ] \
       && [ "$identity" != "$want_identity-dirty" ]; then
      problems+=("identity: want '$want_identity' (optionally '-dirty'), got '$identity'")
    fi
    if [ -n "$want_schema" ] && [ "$schema" != "$want_schema" ]; then
      problems+=("schema digest: want '$want_schema' (schemas/open-test-intent.v1.json), got '$schema'")
    fi
  else
    problems+=("stdout: '$line' is not 'validate-intent <identity> (<goversion> <goos>/<goarch>) schema sha256:<hex>'")
  fi

  if [ ${#problems[@]} -eq 0 ]; then
    passed=$((passed + 1))
    printf '  ok    %s (%s)\n' "$label" "$line"
    return 0
  fi

  failed=$((failed + 1))
  red "  FAIL  $label"
  printf '        args: %s\n' "$*"
  local problem
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  return 1
}

# assert_version_line <label> [args...] — the common case: from the repo root.
assert_version_line() {
  local label="$1"
  shift
  assert_version_line_in "$REPO_ROOT" "$GO_BIN" "$label" "$@"
}

assert_version_line "--version"                              --version
# Position independence, for the same reason --json has it: a user who has
# learned that --json goes anywhere will write `validate-intent FILE --version`,
# and a version flag that only worked in first position would answer that with
# "no file(s) match '--version'" and exit 1 — the exact false red this surface
# exists to remove.
assert_version_line "--version after a file"                 'examples/*.json' --version
assert_version_line "--version between two arguments"        nope.json --version also.json
assert_version_line "--version with --json"                  --json --version
assert_version_line "--version after --source"               --source 'examples/sources/*' --version
# The flag is answered before anything reads a positional, so arguments that
# would otherwise decide the exit code never get to. Two shapes, deliberately,
# because they fail differently: a REFUSAL (exit 2, stdin mode) and a real
# REPORT (exit 1 — since slice 4, SPGD-123, `examples/**/*.json` is expanded
# rather than refused, and over this corpus it finds genuine failures). Both
# must lose to --version, and asserting only the refusal would stop proving that
# the moment a refusal is retired — which is exactly what happened to `**`.
assert_version_line "--version ahead of the stdin refusal"   --version -
assert_version_line "--version ahead of a recursive-glob report" --version 'examples/**/*.json'

# The release-artifact case, and the one this whole slice is for: a copy
# installed under a prefix with NO repo around it, invoked from an unrelated
# working directory. Slice 5 (SPGD-131) is on record for exactly this gap — a
# binary that worked in <repo>/bin/ and failed everywhere else — so `--version`
# is proven where it will actually be run rather than only where it is built.
installprefix="$WORK/installprefix"
mkdir -p "$installprefix/bin"
cp "$GO_BIN" "$installprefix/bin/validate-intent"
if [ -e "$installprefix/schemas" ] || [ -e "$installprefix/examples" ]; then
  failed=$((failed + 1))
  red "  FAIL  install-prefix case — this tree must have no schemas/ or examples/, and it does"
fi
assert_version_line_in "/" "$installprefix/bin/validate-intent" \
  "--version from an install prefix, run from /" --version

# What the case above does NOT prove, and this one does.
#
# It is tempting to read "no schemas/ beside it, and it still answers" as
# proving that `--version` is handled ABOVE LoadSchema in run(). It is not: the
# port embeds the schema (SPGD-131), so the load SUCCEEDS on a schema-less tree
# and the assertion would stay green with --version moved anywhere below it.
#
# The probe has to be a schema that EXISTS and cannot be loaded — the same
# distinction excluded group 4 draws, for the same reason. On this tree
# LoadSchema genuinely fails, so a `--version` that had drifted below it would
# answer `could not load schema ...` on stderr with exit 2, and every check in
# assert_version_line fires at once.
#
# This one is a Go-side assertion rather than a comparison because Python has no
# --version to compare against. The reference's behaviour on this tree is
# already pinned, as a comparison, in section 8 ("OS-level failures").
#
# Since SPGD-290 this case carries a second proof at no extra cost. The line now
# reports the SHA-256 of the schema the binary carries, and assert_version_line_in
# compares it against $REPO_ROOT's canonical file — so on THIS tree, where the
# on-disk schema exists, is a different file, and cannot even be parsed, a pass
# says the digest was computed from the embedded copy and the corrupt file beside
# the binary was never opened. A digest read from disk here could not match, and
# a digest read from disk anywhere else would not be a claim about the artifact.
versionbadschema_root="$WORK/version-badschema"
mkdir -p "$versionbadschema_root/bin" "$versionbadschema_root/schemas"
cp "$GO_BIN" "$versionbadschema_root/bin/validate-intent"
printf '{ "type": "object",\n' > "$versionbadschema_root/schemas/open-test-intent.v1.json"
# The premise, asserted rather than assumed: if this schema ever became loadable
# the case below would still pass, while having stopped testing the ordering it
# is named for.
#
# Written with a temp file rather than a pipe on purpose. This file runs under
# `set -o pipefail`, so `binary ... | grep -q ...` takes the BINARY's exit code
# — 2 here, by design — and the `if` reads that as "the pattern did not match"
# no matter what the pattern did. The first draft of this check did exactly
# that and reported the probe broken while the probe was fine.
(cd "$versionbadschema_root" && "$versionbadschema_root/bin/validate-intent" \
  thing.json >"$WORK/probe.out" 2>"$WORK/probe.err")
if grep -q 'could not load schema' "$WORK/probe.err"; then
  passed=$((passed + 1))
  printf '  ok    the ordering probe'"'"'s schema really does fail to load\n'
else
  failed=$((failed + 1))
  red "  FAIL  the ordering probe's schema loads fine — it can no longer prove the ordering"
  printf '        stderr: %s\n' "$(head -1 "$WORK/probe.err")"
fi
assert_version_line_in "$versionbadschema_root" "$versionbadschema_root/bin/validate-intent" \
  "--version is answered above LoadSchema (unloadable schema present)" --version

# --------------------------------------------------------------------------- #
# Excluded group 6: --schema-source, the Go-only surface that reads the disk
# --------------------------------------------------------------------------- #
#
# The flag `--version` cannot be: it runs the real loader and reports the schema
# a run on this host ENFORCES — resolved origin plus the SHA-256 of the bytes
# actually loaded from it.
#
# Three trees, chosen because the correct answer DIFFERS on each, which is what
# makes this block impossible to satisfy by printing something plausible:
#
#   * the repo, where a real schema sits beside the binary — origin is that
#     path, digest is the canonical contract's;
#   * an install prefix with no schemas/ — origin is <embedded schema>, digest
#     EQUALS --version's, which is success criterion 2;
#   * a tree carrying a valid but DIFFERENT schema — origin is that file, digest
#     is that file's, and it must NOT equal --version's. That case is the slice:
#     carried-versus-enforced becomes observable, and an implementation that
#     reported the compiled-in digest next to the on-disk path fails here and
#     nowhere else.
#
# Both halves of the line are held to a computed expectation. "64 hex
# characters" would pass for a digest of the wrong file, and an origin checked
# only for non-emptiness would pass for a binary that printed its own path — the
# version-number defect one field to the left.
#
# Where sha256_hex cannot produce an expectation (no sha256sum and no shasum),
# the affected check degrades to shape only and says so, rather than silently
# asserting less. The ORIGIN is still compared in that case: it needs no tool.
echo
echo "== --schema-source reports the ENFORCED schema (Go only) =="

# assert_schema_source_in <cwd> <bin> <label> <want-origin> <want-digest> [args...]
#
# <want-digest> may be empty, meaning "no expectation available — check the
# shape only". <want-origin> is always compared.
assert_schema_source_in() {
  local cwd="$1" gobin="$2" label="$3" want_origin="$4" want_digest="$5"
  shift 5

  local rc
  (cd "$cwd" && "$gobin" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
  rc=$?

  local problems=()
  [ "$rc" -eq 0 ] || problems+=("exit code: want 0, got $rc")
  [ ! -s "$WORK/go.err" ] \
    || problems+=("stderr: want empty, got '$(head -c 80 "$WORK/go.err")'")

  local count
  count="$(wc -l < "$WORK/go.out" | tr -d ' ')"
  [ "$count" = "1" ] || problems+=("stdout: want exactly one line, got $count")

  # The origin may contain spaces (it is a path), so the line is split from the
  # RIGHT: the digest is the last field, the origin is everything between
  # 'schema ' and it. This is the same extraction the flag's documentation
  # promises a consumer can do, so asserting it here checks the promise too.
  local line origin digest
  line="$(head -1 "$WORK/go.out")"
  digest="${line##* }"
  origin="${line% *}"
  origin="${origin#schema }"

  case "$line" in
    "schema "*" sha256:"*) ;;
    *) problems+=("stdout: '$line' is not 'schema <origin> sha256:<hex>'") ;;
  esac
  digest="${digest#sha256:}"
  case "$digest" in
    *[!0-9a-f]*|"") problems+=("digest '$digest' is not lowercase hex") ;;
  esac
  [ "${#digest}" -eq 64 ] || problems+=("digest '$digest' is not 64 characters")

  [ "$origin" = "$want_origin" ] \
    || problems+=("origin: want '$want_origin', got '$origin'")
  if [ -n "$want_digest" ] && [ "$digest" != "$want_digest" ]; then
    problems+=("digest: want '$want_digest', got '$digest'")
  fi

  if [ ${#problems[@]} -eq 0 ]; then
    passed=$((passed + 1))
    printf '  ok    %s (%s)\n' "$label" "$line"
    return 0
  fi

  failed=$((failed + 1))
  red "  FAIL  $label"
  printf '        args: %s\n' "$*"
  local problem
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  return 1
}

# schema_source_digest_of <cwd> <bin> — the digest the flag reports there, or ""
# if it did not answer. Used to COMPARE two trees' answers rather than to check
# either one, which is why it is separate from the assertion above.
schema_source_digest_of() {
  local cwd="$1" gobin="$2"
  local line
  line="$( (cd "$cwd" && "$gobin" --schema-source 2>/dev/null) | head -1)"
  case "$line" in
    "schema "*" sha256:"*) printf '%s' "${line##*sha256:}" ;;
    *) printf '' ;;
  esac
}

# 1. The repo itself: a real schema sits at $REPO_ROOT/schemas, and the binary
#    lives in $REPO_ROOT/bin, so the executable-relative path resolves to it.
assert_schema_source_in "$REPO_ROOT" "$GO_BIN" \
  "--schema-source names the repo's schema" \
  "$canonical_schema" "$want_schema" --schema-source

# Position independence, for the same reason --version has it.
assert_schema_source_in "$REPO_ROOT" "$GO_BIN" \
  "--schema-source after a file" \
  "$canonical_schema" "$want_schema" 'examples/*.json' --schema-source
assert_schema_source_in "$REPO_ROOT" "$GO_BIN" \
  "--schema-source ahead of the stdin refusal" \
  "$canonical_schema" "$want_schema" --schema-source -

# --version still wins when both are given. Three consumers parse that line
# (scripts/install.sh, scripts/build-release.sh, specguard-rspec's identity
# probe), so the newer flag must not change what any crossing of the two
# prints. Asserted with the --version helper, which is the point: the expected
# output is the version line, unchanged.
assert_version_line "--version wins over --schema-source"        --version --schema-source
assert_version_line "--version wins over --schema-source (reversed)" --schema-source --version

# 2. Success criterion 2: the install layout. No schemas/ beside the binary, so
#    the embedded copy is what a run enforces — and the digest must therefore
#    EQUAL the one --version reports. $installprefix is the tree built for the
#    --version case above, already asserted to carry no schemas/.
assert_schema_source_in "/" "$installprefix/bin/validate-intent" \
  "--schema-source from an install prefix, run from / (the embedded copy)" \
  "<embedded schema>" "$want_schema" --schema-source

# 3. Success criterion 1, and the reason the flag exists: a tree whose schema is
#    valid, loadable and DIFFERENT from the compiled-in one. The origin must be
#    that file and the digest must be that file's — which is to say it must NOT
#    be the digest --version reports on the same binary.
#
#    pwd -P because the binary derives its path from os.Executable(): if $WORK
#    sits under a symlinked prefix (/tmp on macOS), the string the binary prints
#    is the resolved one and an unresolved expectation would fail for a reason
#    that has nothing to do with the port.
schemasource_root="$WORK/schema-source-override"
mkdir -p "$schemasource_root/bin" "$schemasource_root/schemas"
schemasource_root="$(cd "$schemasource_root" && pwd -P)"
cp "$GO_BIN" "$schemasource_root/bin/validate-intent"
override_schema="$schemasource_root/schemas/open-test-intent.v1.json"
printf '{"type": "object"}\n' > "$override_schema"
override_digest="$(sha256_hex "$override_schema")"
if [ -z "$override_digest" ]; then
  dim "note: no SHA-256 tool on PATH — the overriding schema's digest is checked"
  dim "      for shape only. The carried-vs-enforced comparison below is unaffected:"
  dim "      it compares the binary's own two answers and needs no external tool."
fi

# The premise, asserted rather than assumed. If this schema ever stopped
# differing from the canonical one — someone copies the real file here, or the
# canonical contract becomes `{"type": "object"}` — every assertion below would
# still pass while having stopped testing the difference it is named for.
if [ -n "$override_digest" ] && [ -n "$want_schema" ] && [ "$override_digest" = "$want_schema" ]; then
  failed=$((failed + 1))
  red "  FAIL  the override schema is identical to the canonical one — it cannot show carried≠enforced"
fi
# And it must actually LOAD: a schema that fails to load would make the flag
# exit 2, and "the digest differs" would be established by a case that never
# produced a digest.
(cd "$schemasource_root" && "$schemasource_root/bin/validate-intent" \
  --schema-source >"$WORK/probe.out" 2>"$WORK/probe.err")
if [ -s "$WORK/probe.out" ]; then
  passed=$((passed + 1))
  printf '  ok    the override schema loads (so the comparison below has a digest to make)\n'
else
  failed=$((failed + 1))
  red "  FAIL  the override schema did not load — the case below cannot compare digests"
  printf '        stderr: %s\n' "$(head -1 "$WORK/probe.err")"
fi

assert_schema_source_in "$schemasource_root" "$schemasource_root/bin/validate-intent" \
  "--schema-source names an overriding schema and digests ITS bytes" \
  "$override_schema" "$override_digest" --schema-source

# The comparison the whole slice is for, made explicitly rather than left to be
# inferred from two expectations that happen to differ: on this tree the same
# binary reports one digest for what it CARRIES and another for what it
# ENFORCES.
carried_digest="$(schema_source_digest_of "$installprefix" "$installprefix/bin/validate-intent")"
enforced_digest="$(schema_source_digest_of "$schemasource_root" "$schemasource_root/bin/validate-intent")"
if [ -z "$carried_digest" ] || [ -z "$enforced_digest" ]; then
  failed=$((failed + 1))
  red "  FAIL  carried-vs-enforced — one of the two runs did not report a digest"
elif [ "$carried_digest" = "$enforced_digest" ]; then
  failed=$((failed + 1))
  red "  FAIL  carried-vs-enforced — the same digest ($carried_digest) on both trees"
  printf '        A schema beside the binary is winning on one of them, so the two\n'
  printf '        must differ. They do not, which is the defect this flag exists to expose.\n'
else
  passed=$((passed + 1))
  printf '  ok    carried (%s…) differs from enforced (%s…)\n' \
    "${carried_digest:0:12}" "${enforced_digest:0:12}"
fi

# Success criterion 3: a schema that EXISTS and cannot be loaded is not papered
# over with the embedded copy and does not become a clean report. It must exit 2
# with the SAME diagnostic a verdict run emits on that tree — and that
# diagnostic is itself compared against python3 in section 8 ("OS-level
# failures"), so requiring equality here inherits the comparison rather than
# restating the message where it could drift.
#
# $versionbadschema_root is the tree built above: its schema is deliberately
# truncated, and the probe there has already asserted it really does fail to
# load.
(cd "$versionbadschema_root" && "$versionbadschema_root/bin/validate-intent" \
  --schema-source >"$WORK/ss.out" 2>"$WORK/ss.err")
ss_rc=$?
(cd "$versionbadschema_root" && "$versionbadschema_root/bin/validate-intent" \
  thing.json >"$WORK/verdict.out" 2>"$WORK/verdict.err")
verdict_rc=$?

problems=()
[ "$ss_rc" -eq 2 ] || problems+=("exit code: want 2, got $ss_rc")
[ ! -s "$WORK/ss.out" ] \
  || problems+=("stdout: want empty, got '$(head -c 80 "$WORK/ss.out")'")
grep -q 'could not load schema' "$WORK/ss.err" \
  || problems+=("stderr: want the could-not-load diagnostic, got '$(head -c 80 "$WORK/ss.err")'")
[ "$ss_rc" = "$verdict_rc" ] \
  || problems+=("exit code differs from the verdict path's: $ss_rc vs $verdict_rc")
cmp -s "$WORK/ss.err" "$WORK/verdict.err" \
  || problems+=("stderr differs from the verdict path's on the same tree")

if [ ${#problems[@]} -eq 0 ]; then
  passed=$((passed + 1))
  printf '  ok    --schema-source fails exactly as the verdict path does (exit 2: %s)\n' \
    "$(head -1 "$WORK/ss.err")"
else
  failed=$((failed + 1))
  red "  FAIL  --schema-source on an unloadable schema"
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  diff -u "$WORK/verdict.err" "$WORK/ss.err" | tail -n +3 | sed 's/^/        /'
fi

# --------------------------------------------------------------------------- #
# Excluded group 4: a tree with no schemas/ directory beside the binary
# --------------------------------------------------------------------------- #
#
# These five argument sets USED to be compared, in section 8 ("OS-level
# failures"), against a tree with no schemas/ directory. Since SPGD-131 the Go
# binary embeds the schema and falls back to it when the file is absent, so it
# no longer fails there and Python still does. That is a real divergence, and it
# is the divergence the change exists to create: a released binary has no repo
# around it.
#
# Deleting the five cases would have been the easy move and the wrong one. One
# of them — the crossing of a failed schema load with a bare `--json` — is on
# record as having caught a real ordering drift in main.go, and coverage with a
# history of catching something is the last coverage to discard quietly. So the
# crossing stayed a byte-for-byte COMPARISON (section 8, "OS-level failures",
# now runs it against a
# malformed schema, which fails to load in both), and what the Go binary does on
# a schema-less tree is pinned HERE, argument set for argument set.
#
# "Excluded" must not mean "unchecked", and it must not mean "assumed to work"
# either: two of the five still fail, and they are asserted failing.
echo
echo "== excluded group 4: no schemas/ beside the binary (Go only) =="

noschema_root="$WORK/noschema"
mkdir -p "$noschema_root/bin"
cp "$REFERENCE" "$noschema_root/bin/validate-intent"
cp "$GO_BIN" "$noschema_root/bin/validate-intent-go"
cp "$REPO_ROOT/examples/unit-order-total.json" "$noschema_root/thing.json"

# The group's premise, asserted rather than assumed. If a schemas/ directory
# ever appeared in this tree, every expectation below would still hold — Go
# would read the file and answer the same way — while the block had stopped
# testing the fallback at all. A test whose setup silently stopped applying is
# the quietest way to lose coverage.
if [ -e "$noschema_root/schemas" ]; then
  failed=$((failed + 1))
  red "  FAIL  excluded group 4 — this tree must have NO schemas/ directory, and it has one"
fi

# assert_no_schema_tree <label> <want-rc> <want-stdout> <want-stderr> [args...]
#
# <want-stdout>/<want-stderr> are substrings that must appear, or the literal
# EMPTY meaning that stream must be empty. Both are always checked: asserting an
# exit code alone would pass a binary that exited 0 having printed nothing,
# which is the shape this whole file exists to refuse.
#
# It also asserts, first, that python3 STILL fails to load the schema on this
# tree with these arguments. Without that the case could sit in the "excluded"
# list long after the divergence had gone, and nobody would find out — an
# exclusion that is no longer a divergence is comparison coverage given up for
# free, which is the expensive half of this file being paid for nothing.
assert_no_schema_tree() {
  local label="$1" want_rc="$2" want_out="$3" want_err="$4"
  shift 4

  local py_rc
  (cd "$noschema_root" && "$PYTHON" "$noschema_root/bin/validate-intent" "$@" \
    >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  if [ "$py_rc" -ne 2 ] || ! grep -q 'could not load schema' "$WORK/py.err"; then
    failed=$((failed + 1))
    red "  FAIL  $label — the reference no longer diverges here (exit $py_rc)"
    printf '        This case is excluded because python3 cannot run on a schema-less\n'
    printf '        tree. It now can, so the case belongs in a compare_in, not here.\n'
    return 1
  fi

  local rc
  (cd "$noschema_root" && "$noschema_root/bin/validate-intent-go" "$@" \
    >"$WORK/go.out" 2>"$WORK/go.err")
  rc=$?

  local problems=()
  [ "$rc" = "$want_rc" ] || problems+=("exit code: want $want_rc, got $rc")

  if [ "$want_out" = "EMPTY" ]; then
    [ ! -s "$WORK/go.out" ] || problems+=("stdout: want empty, got '$(head -c 80 "$WORK/go.out")'")
  else
    grep -qF -- "$want_out" "$WORK/go.out" || problems+=("stdout: want it to contain '$want_out'")
  fi

  if [ "$want_err" = "EMPTY" ]; then
    [ ! -s "$WORK/go.err" ] || problems+=("stderr: want empty, got '$(head -c 80 "$WORK/go.err")'")
  else
    grep -qF -- "$want_err" "$WORK/go.err" || problems+=("stderr: want it to contain '$want_err'")
  fi

  if [ ${#problems[@]} -eq 0 ]; then
    passed=$((passed + 1))
    printf '  ok    %s (exit %s)\n' "$label" "$rc"
    return 0
  fi

  failed=$((failed + 1))
  red "  FAIL  $label"
  printf '        args: %s\n' "$*"
  local problem
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  return 1
}

# The two that now WORK — the release-asset case, and the reason for the change.
# Before SPGD-131 both of these exited 2 with "could not load schema".
assert_no_schema_tree "adopter mode works with no schemas/ (was exit 2)" \
  0 "PASS  thing.json" EMPTY thing.json
assert_no_schema_tree "--source --json works with no schemas/ (was exit 2)" \
  0 '"mode": "source"' EMPTY --source thing.json --json

# The ordering crossing, Go side. Python answers "could not load schema"; the
# port loads its embedded copy and therefore REACHES the self-test --json
# refusal. Both exit 2; the reasons differ, and that is the divergence.
#
# Note what this can and cannot prove. It pins the message, so a fall-through
# would be caught. It does NOT prove the refusal still sits below LoadSchema —
# on this tree the load succeeds either way. The malformed-schema crossing in
# section 8 ("OS-level failures")
# is what proves the ordering, and it must stay there.
assert_no_schema_tree "bare --json still refuses: self-test is not a --json surface" \
  2 EMPTY "--json is not supported in self-test mode" --json

# The two that still FAIL, asserted failing.
#
# Fixing the schema did not make the binary self-contained: RepoRoot() resolves
# the fixture CORPUS the same executable-relative way, and examples/ is not
# embedded. So a bare self-test on a schema-less tree finds no fixtures and says
# so, four times, and exits 1. That is SPGD-56's empty-fixture guard working as
# designed — a self-test that verified nothing must not report success — and
# --help already calls this mode "self-test the in-repo fixtures". Whether a
# released binary should carry the corpus is a real question and a separate
# slice. Until then, this assertion is what keeps "the schema is embedded" from
# being read as "the binary is self-contained".
assert_no_schema_tree "bare self-test still fails loudly: the corpus is not embedded" \
  1 EMPTY "no fixtures match 'examples/*.json'"
assert_no_schema_tree "--source with no argument still refuses" \
  2 EMPTY "--source requires at least one FILE/glob argument" --source

# --------------------------------------------------------------------------- #
# 17. Go-side refusals — schemas carrying a pattern RE2 cannot reproduce
# --------------------------------------------------------------------------- #
#
# The other half of excluded group 3. Each case builds a tree whose schema
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
# A quantifier on a bare `^`/`\A` is the same shape, and lands in the worst
# direction: RE2 reads `^*` as "match empty anywhere", so the constraint is
# satisfied by every input, while Python raises "nothing to repeat" before it
# can check anything. A silent PASS against an oracle that crashes.
assert_pattern_refusal 'quantified ^ (nothing to repeat)' \
  '^*' '"abc"' error
assert_pattern_refusal 'quantified \A (nothing to repeat)' \
  '\\A{2}' '"abc"' error
# The two the old compile-only guard already caught, kept so that narrower
# failure mode stays covered.
assert_pattern_refusal 'lookbehind' \
  '(?<=a)b' '"ab"' pass
assert_pattern_refusal 'backreference' \
  '(a)\\1' '"aa"' pass

# --------------------------------------------------------------------------- #
# 18. the reference is untouched
# --------------------------------------------------------------------------- #
#
# If the port were "made to pass" by editing the oracle, every comparison above
# would still be green — so the oracle's integrity is checked explicitly rather
# than assumed.
#
# What this checks is the WORKING TREE, not the history: the reference is not
# frozen forever, and SPGD-279 edited it deliberately (the shared last line of
# the usage block, corrected in both implementations in one commit). What is
# forbidden is an uncommitted local tweak to the oracle while the harness runs,
# which is the shape a "made to pass" edit actually takes. A committed,
# reviewed change to the reference leaves this green; an edit made to get out of
# a red run does not.
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
# 19. the Ruby leg — specguard-lint vs. the port
# --------------------------------------------------------------------------- #
#
# The second oracle. Everything above proves the port reproduces PYTHON; this
# runs tests/parity/run_ruby_parity.sh, which proves it agrees with an
# implementation that was never written from it — the specguard-rspec gem's
# linter, derived from PROTOCOL.md directly. Two implementations agreeing is
# evidence; three, one of which shares no ancestry with the other two, is
# considerably better evidence.
#
# It is run LAST and as a subprocess because it is a standalone script with its
# own exit contract, its own participants, and its own reasons to be
# unrunnable. Its output is passed through verbatim rather than summarised: the
# reason a Ruby case failed is not recoverable from a count.
#
# Its three exit codes are mapped, deliberately, onto three different verdicts
# here — 0 is one pass, 1 is one failure, and 2 is "a participant is missing or
# cannot run". That last one is NOT folded into the pass count. It is a red
# SKIP, it survives to the verdict line, and the verdict says the skipped cases
# were not verified, because the one thing this harness must never do is report
# a green on behalf of a leg that never ran.
echo
echo "== the Ruby leg (specguard-lint vs. the port) =="
RUBY_LEG="$REPO_ROOT/tests/parity/run_ruby_parity.sh"
if [ ! -x "$RUBY_LEG" ]; then
  failed=$((failed + 1))
  red "  FAIL  $RUBY_LEG is missing or not executable"
else
  # GO_BIN is passed explicitly so the leg compares the binary this run just
  # built, rather than resolving one of its own.
  if GO_BIN="$GO_BIN" "$RUBY_LEG"; then
    passed=$((passed + 1))
    printf '  ok    the Ruby leg passed (see its own output above)\n'
  else
    ruby_leg_rc=$?
    if [ "$ruby_leg_rc" = "2" ]; then
      skipped=$((skipped + 1))
      red "  SKIP  the Ruby leg could not run (exit 2) — see its diagnostic above."
      red "        Nothing about specguard-lint was verified by this run. Point"
      red "        SPECGUARD_RSPEC at a specguard-rspec checkout to include it."
    else
      failed=$((failed + 1))
      red "  FAIL  the Ruby leg reported a disagreement (exit $ruby_leg_rc)"
    fi
  fi
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
