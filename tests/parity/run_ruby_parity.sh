#!/usr/bin/env bash
#
# Differential parity harness, RUBY LEG: the specguard-rspec linter vs. the Go
# port.
# ======================================================================
#
#   tests/parity/run_ruby_parity.sh
#
# For every case below this runs
#
#     bin/validate-intent-go --source FILE...             (the port)
#     ruby <gem>/bin/specguard-lint       FILE...         (the second oracle)
#
# from the SAME working directory (this repo's root) with the SAME relative
# paths, and requires their findings and exit codes to agree.
#
# Why this exists
# ---------------
# The roadmap gates its final clause — the Ruby gem's linter becoming a thin
# wrapper around the Go binary — on an explicit precondition: "Prove Ruby ≡ Go
# on the corpus + exit codes + messages. Once proven, ...". Until this file
# existed that proof did not. The gem's own spec/specguard/rspec/
# message_parity_spec.rb pins four hand-transcribed strings against
# Schema#violations; it never runs a scanner, an exit code, or a byte of Go,
# and it says so. Everything else was a sentence in a comment.
#
# This file is the missing half, and it lives HERE — in open-test-intent,
# reaching OUT to the gem — rather than in the gem's RSpec suite, because
# lib/specguard/rspec.rb's `SCHEMA_PATH` forbids the gem a cross-repo runtime
# dependency and spec/specguard/rspec/message_parity_spec.rb's header, under
# "WHAT THIS FILE IS NOT", rules out shelling out from the suite on purpose
# (it would pass on one container and be unrunnable anywhere else). Neither of
# those decisions is disturbed here.
#
# Why it is a SEPARATE script from run_parity.sh
# ----------------------------------------------
# run_parity.sh refuses to start without a Go toolchain, because it rebuilds the
# port before comparing it — correct for a leg whose whole subject is freshly
# written Go. This leg's subject is the GEM, and it has to stay runnable where
# there is no Go toolchain at all, which is the common case on a Ruby machine.
# So it uses the binary it finds and rebuilds only when it can (see "the Go
# participant" below). run_parity.sh invokes this file as its last section, so
# one command still runs everything where both toolchains exist.
#
# NOTE, because a previous write-up of this ticket got it backwards:
# bin/validate-intent-go is NOT committed. It is a build artifact, ignored at
# .gitignore:7. On a fresh checkout with no Go toolchain there is no binary to
# compare against, and this script exits 2 saying exactly that rather than
# reporting a pass it did not earn.
#
#
# The shared surface, and the four differences normalised out of it
# ----------------------------------------------------------------
# `validate-intent --source` is a FIXTURE SELF-TEST; `specguard-lint` is a CI
# LINTER. They deliberately do not print the same report, and those differences
# are ratified at `CLI#report_results` in lib/specguard/rspec/cli.rb. Each is
# named here and removed by exactly one rule, so the exclusions are stated
# rather than implied:
#
#   1. `PASS  <file>:<line>` — Go only. The gem prints no line per healthy
#      annotation; in a large repo that buries the failures.
#   2. `----  <file> — no @intent annotations` — Go only, and NOT named in the
#      ticket that asked for this leg: a file carrying no annotation at all
#      produces this line from the port and nothing from the gem.
#   3. `specguard-lint: checked N spec file(s)` — gem only, LEADING line.
#   4. `specguard-lint: checked N @intent annotations, M malformed[; K file(s)
#      could not be read]` — gem only, TRAILING line. There are TWO of these
#      lines and both go to stdout (stderr is empty); a normaliser that stripped
#      only the trailing one would diff on the leading one every single case.
#
# What is left after those four rules is every byte of every FAIL block, in
# order, plus the exit code. Those must be identical.
#
# WHY THIS IS NOT VACUOUS
# -----------------------
# Rules 1 and 2 delete the port's entire output for a clean file, and rules 3
# and 4 delete the gem's. A valid corpus therefore compares EMPTY-TO-EMPTY, and
# a harness that stopped there would report byte-for-byte parity between two
# programs that had both crashed before printing anything. This project's
# house defect, named in the knowledge base as "vacuous green", is exactly that
# shape, and the harness that only compares what survives normalisation is a
# textbook instance of it.
#
# So the stripped material is not thrown away — it is CROSS-CHECKED against the
# other side, because both halves independently carry the same three counts:
#
#     annotations seen  =  Go's PASS lines + Go's line-scoped FAIL lines
#                       =  the gem's "checked N @intent annotations"
#     malformed         =  Go's line-scoped FAIL lines
#                       =  the gem's "M malformed"
#     unreadable files  =  Go's FAIL lines carrying "could not read file:"
#                       =  the gem's "K file(s) could not be read"
#
# A case only passes when the surviving bytes match AND all three counts agree,
# and the run only passes when the corpus contributed a non-zero number of
# annotations overall. Two silent crashes now fail on the counts; a port that
# stopped emitting PASS lines fails on the counts; a gem whose summary line
# drifts from what it printed fails on the counts.
#
# The "the harness can fail" section additionally proves the comparator can go
# red at all, by running known-unequal inputs through the real normalisation
# and requiring a mismatch.
#
# A NOTE ON CROSS-REPO CITATIONS, in BOTH directions: by NAME, never by number.
#
# gem -> here: tests/parity/check_section_refs.py enforces the number↔name
# agreement for run_parity.sh only — it owns one numbering — so a numeric
# citation of a section in THIS file, and especially one from the other repo
# (lib/specguard/rspec/{scanner,linter}.rb both point back here), is unchecked
# and rots the first time a section is inserted. The names are stable and
# searchable; the numbers are for reading the output, not for citing.
#
# here -> gem: the same rule, and it is not hypothetical. This file first
# shipped citing the gem by line number while the companion commit added 35
# lines above the cited site in the very same changeset — so the citation for
# ratified difference (a) landed pointing at the code for ratified difference
# (b). Nothing catches that: check_section_refs.py reads run_parity.sh section
# numbers, not gem line numbers, and no checker in either repo can, because
# neither repo has the other at test time by construction (that is the whole
# premise above). So cite the gem by SYMBOL — `CLI#report_results`,
# `Scanner.scan_text`, `SCHEMA_PATH` — which survives exactly the edit that
# broke the numbers, and which `grep` resolves in one hop.
#
#
# The two ratified read-failure differences
# -----------------------------------------
# Two inputs make the gem and the port disagree in TEXT while agreeing in
# verdict and exit code. Both are ratified here rather than "fixed", and both
# are ASSERTED — including the assertion that they still differ — so that
# closing one of them fails this file and forces the ratification to be
# retired, instead of leaving a stale exclusion nobody notices.
#
#   (a) A spec file that is not valid UTF-8.
#       port: FAIL <file> — could not read file: 'utf-8' codec can't decode
#             byte 0xe9 in position 117: invalid continuation byte
#       gem:  FAIL <file> — could not read file: invalid UTF-8 byte sequence
#
#       RATIFIED. The port reproduces CPython's UnicodeDecodeError text on
#       purpose (cmd/validate-intent/fileio.go:69); the gem emits a fixed
#       string (`Scanner.scan_text` in lib/specguard/rspec/scanner.rb — (b)'s
#       emitting site is `Scanner.scan_file`, one method up, so a line number
#       here silently sends a reader to the OTHER ratified difference the first
#       time that file grows a comment block; it did). This harness's PYTHON
#       leg already declines to compare that same tail — run_parity.sh's
#       excluded group 1, "Non-UTF-8 input — the PROSE only", excludes it
#       between Python and Go on the grounds that only the tail can differ —
#       so holding the gem to a stricter rule than the port's own oracle would
#       be incoherent. Reproducing the tail would mean porting CPython's
#       decoder-error classification a third time, into a gem whose entire
#       reason for vendoring the schema is to owe open-test-intent nothing at
#       runtime.
#
#       Asserted below: same classification (a read failure), same file, same
#       `FAIL <file> — could not read file: ` prefix, same exit code, and a
#       NON-EMPTY reason on both sides. Only the reason text is unpinned.
#
#   (b) A named file that does not exist.
#       port: (stderr) error: no file(s) match '<path>'      exit 1
#       gem:  (stdout) FAIL <path> — could not read file: No such file or
#                      directory @ rb_sysopen - <path>       exit 1
#
#       RATIFIED, and this one is a difference in KIND, not wording. The port's
#       arguments are GLOB PATTERNS — it expands them itself
#       (bin/validate-intent:493, cmd/validate-intent/main.go:231), so a
#       pattern matching nothing is "no file(s) match" and is a statement about
#       the pattern. The gem's arguments are PATHS: it does no globbing at all
#       (explicit files are "checked as given", `CLI#select`; --changed
#       derives its list from git), so an unopenable named path is a read
#       failure OF THAT PATH, which is why it can name the errno the port
#       cannot. Making the gem emit "no file(s) match" would require giving it
#       a globber first, changing what `specguard-lint 'foo*_spec.rb'` means
#       for every existing user, to import a diagnostic about a concept the
#       gem's CLI does not have.
#
#       What actually matters here is shared and IS asserted below: both refuse
#       to be silently green (exit 1), both name the missing path, and — the
#       case with real teeth — neither aborts the rest of the run, so the good
#       files named alongside a missing one are still fully checked by both.
#
#
# Invocation
# ----------
#     tests/parity/run_ruby_parity.sh
#
#   SPECGUARD_RSPEC   the gem checkout      (default: ../specguard-rspec)
#   GO_BIN            the port              (default: bin/validate-intent-go)
#   RUBY              (default: ruby)
#   GO                only used to build GO_BIN when it is missing (default: go)
#
# Exit codes follow the tools' own contract, which is the whole point:
#
#   0  every case that could run compared and agreed
#   1  at least one case disagreed
#   2  a participant is missing, or is present and cannot run, so NOTHING was
#      compared — never a pass
#
# A THIRD outcome sits inside exit 0 and is deliberately not promoted to a
# failure: a case the environment prevented from running at all — the
# unopenable-file case under root, the corpus-integrity check outside a git
# checkout. Those are red SKIP lines, they are counted, and the verdict line
# repeats the count and says the skipped cases were not verified. Making them
# failures would leave this file permanently red in root containers for a
# reason unrelated to parity, and a harness that is always red is a harness
# nobody reads.
#
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
RUBY="${RUBY:-ruby}"
GO="${GO:-go}"
GO_BIN="${GO_BIN:-$REPO_ROOT/bin/validate-intent-go}"
GEM_ROOT="${SPECGUARD_RSPEC:-$REPO_ROOT/../specguard-rspec}"

WORK="$(mktemp -d)"
trap 'chmod -R u+rwX "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT

passed=0
failed=0
skipped=0
annotations_compared=0

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
dim()   { printf '\033[2m%s\033[0m\n' "$*"; }

# --------------------------------------------------------------------------- #
# preflight: every participant, or exit 2
# --------------------------------------------------------------------------- #
#
# A missing participant is the one outcome this file must never render as a
# pass. It is not a skip and not a warning: nothing was compared, so the only
# honest answer is "could not check", which is exit 2 in both tools' own
# vocabulary. `CLI#run`'s rescue-band comment lists three shipped instances of
# the opposite (SPGD-35, SPGD-52, SPGD-56); a parity harness that skipped
# itself into a green would be the fourth.

if ! command -v "$RUBY" >/dev/null 2>&1; then
  red "error: no Ruby on PATH (set RUBY=/path/to/ruby)"
  red "       nothing was compared — this is NOT a pass"
  exit 2
fi

GEM_LINT="$GEM_ROOT/bin/specguard-lint"
if [ ! -f "$GEM_LINT" ] || [ ! -f "$GEM_ROOT/lib/specguard/rspec.rb" ]; then
  red "error: no specguard-rspec checkout at $GEM_ROOT"
  red "       expected $GEM_LINT and $GEM_ROOT/lib/specguard/rspec.rb"
  red "       set SPECGUARD_RSPEC=/path/to/specguard-rspec"
  red "       nothing was compared — this is NOT a pass"
  exit 2
fi
GEM_ROOT="$(cd "$GEM_ROOT" && pwd -P)"
GEM_LINT="$GEM_ROOT/bin/specguard-lint"

# A checkout on disk is not the same thing as a linter that runs. specguard-lint
# maps "the gem could not load" onto exit 2 on purpose (`CLI#run`), so a
# missing runtime dependency or the wrong GEM_HOME produces a tool that answers
# every input with 2 and inspects nothing. Comparing against that would fill the
# screen with parity failures that are not parity failures — a false RED, which
# is the same defect as a false green wearing the other costume. Find out here,
# once, on a file whose answer is known.
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$WORK/preflight_spec.rb"
"$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$WORK/preflight_spec.rb" \
  >"$WORK/preflight.out" 2>"$WORK/preflight.err"
preflight_rc=$?
if [ "$preflight_rc" != "0" ] || ! grep -q '^specguard-lint: checked ' "$WORK/preflight.out"; then
  red "error: the gem at $GEM_ROOT does not run here"
  red "       it exited $preflight_rc on a file carrying one valid annotation,"
  red "       where the only correct answer is 0 and a summary line."
  if [ -s "$WORK/preflight.err" ]; then
    sed 's/^/       /' "$WORK/preflight.err"
  fi
  red "       Install its dependencies (bundle install in that checkout), or"
  red "       point RUBY at an interpreter that can see them — e.g."
  red "       BUNDLE_GEMFILE=$GEM_ROOT/Gemfile RUBYOPT=-rbundler/setup ..."
  red "       nothing was compared — this is NOT a pass"
  exit 2
fi

# The Go participant.
#
# Rebuild when a toolchain is available, because comparing against a stale
# binary would prove parity with a port nobody is shipping. When there is none,
# fall back to the binary already on disk — that fallback is the whole reason
# this leg is a separate script — and say so loudly, so "the binary may be
# stale" is a visible caveat on the run rather than a silent assumption. With
# neither, there is no port to compare and this is a 2.
if command -v "$GO" >/dev/null 2>&1; then
  dim "building $GO_BIN with $GO ..."
  if ! (cd "$REPO_ROOT" && "$GO" build -o "$GO_BIN" ./cmd/validate-intent); then
    red "error: go build failed"
    red "       nothing was compared — this is NOT a pass"
    exit 2
  fi
elif [ -x "$GO_BIN" ]; then
  dim "note: no Go toolchain on PATH — comparing against the binary already at"
  dim "      $GO_BIN, which was NOT rebuilt and may be older than cmd/."
else
  red "error: no Go toolchain on PATH and no binary at $GO_BIN"
  red "       bin/validate-intent-go is a build artifact (.gitignore:7), not a"
  red "       committed file: build it with 'go build -o bin/validate-intent-go"
  red "       ./cmd/validate-intent' or point GO_BIN at one."
  red "       nothing was compared — this is NOT a pass"
  exit 2
fi

dim "gem:  $GEM_ROOT"
dim "port: $GO_BIN"

# --------------------------------------------------------------------------- #
# the normalisation, in one place
# --------------------------------------------------------------------------- #
#
# Four rules, one per ratified report difference. They are functions rather
# than inline sed calls so that the "the harness can fail" section can exercise
# the SAME code the comparisons use — a self-check against a second, drifting
# copy of the rules would be worth nothing.

# rule 1 (PASS) and rule 2 (----): the port's per-file chatter.
normalize_go() {
  sed -e '/^PASS  /d' -e '/^----  /d'
}

# rules 3 and 4: BOTH of the gem's summary lines, leading and trailing.
normalize_ruby() {
  sed -e '/^specguard-lint: checked /d'
}

# run_go <label-safe-name> [args...] — writes $WORK/<name>.go.{out,err,rc}
run_go() {
  local name="$1"
  shift
  (cd "$REPO_ROOT" && "$GO_BIN" --source "$@" >"$WORK/$name.go.out" 2>"$WORK/$name.go.err")
  printf '%s' "$?" > "$WORK/$name.go.rc"
}

# run_ruby <name> [args...] — writes $WORK/<name>.rb.{out,err,rc}
#
# Run from THIS repo's root with the same relative paths the port got. Both
# tools echo paths exactly as given, so invoking the gem from its own checkout
# with ../open-test-intent/... prefixes would diff every case on the path alone
# and prove nothing. -I is redundant (bin/specguard-lint puts its own lib/ on
# the load path) and passed anyway, so this keeps working if that ever changes.
run_ruby() {
  local name="$1"
  shift
  (cd "$REPO_ROOT" && "$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$@" \
    >"$WORK/$name.rb.out" 2>"$WORK/$name.rb.err")
  printf '%s' "$?" > "$WORK/$name.rb.rc"
}

# --------------------------------------------------------------------------- #
# the counts the normalisation would otherwise discard
# --------------------------------------------------------------------------- #

# count_matching <file> <extended-regex>
count_matching() {
  grep -cE "$2" "$1" 2>/dev/null || true
}

# go_counts <out-file> — echoes "<annotations> <malformed> <unread>"
#
# A read failure prints `FAIL  <path> — could not read file: ...` with no
# `:line`, because nothing in the file was ever line-scoped; every other FAIL
# is one annotation. That distinction is the reference's own (KIND_READ,
# bin/validate-intent:91) and it is what keeps the port's counts comparable
# with the gem's, whose summary line deliberately does not fold unread files
# into the annotation count (`CLI#summary_line`).
go_counts() {
  local out="$1" pass fail_all fail_read
  pass="$(count_matching "$out" '^PASS  ')"
  fail_all="$(count_matching "$out" '^FAIL  ')"
  fail_read="$(count_matching "$out" '^FAIL  .* — could not read file: ')"
  printf '%s %s %s' "$((pass + fail_all - fail_read))" "$((fail_all - fail_read))" "$fail_read"
}

# ruby_counts <out-file> — echoes "<annotations> <malformed> <unread>", or
# nothing at all when the trailing summary line is absent. An absent summary is
# never treated as zero: the gem prints it unconditionally on every run that
# reaches the reporter, so its absence means the run did not get there — an
# exit-2 misuse, most likely — and reading that as "0 annotations, 0 malformed"
# is how a harness agrees with a tool that never ran.
ruby_counts() {
  local out="$1" line ann bad unread
  line="$(grep '^specguard-lint: checked .* @intent annotation' "$out" | tail -n 1)"
  [ -n "$line" ] || return 1
  ann="$(printf '%s' "$line" | sed -n 's/^specguard-lint: checked \([0-9][0-9]*\) @intent annotation.*$/\1/p')"
  bad="$(printf '%s' "$line" | sed -n 's/^.*, \([0-9][0-9]*\) malformed.*$/\1/p')"
  unread="$(printf '%s' "$line" | sed -n 's/^.*; \([0-9][0-9]*\) file[s]* could not be read$/\1/p')"
  [ -n "$ann" ] && [ -n "$bad" ] || return 1
  [ -n "$unread" ] || unread=0
  printf '%s %s %s' "$ann" "$bad" "$unread"
}

# --------------------------------------------------------------------------- #
# the comparison primitive
# --------------------------------------------------------------------------- #

# compare <label> [files...]
compare() {
  local label="$1"
  shift
  local name="case$((passed + failed))"

  run_go  "$name" "$@"
  run_ruby "$name" "$@"

  local go_rc rb_rc
  go_rc="$(cat "$WORK/$name.go.rc")"
  rb_rc="$(cat "$WORK/$name.rb.rc")"

  normalize_go   < "$WORK/$name.go.out" > "$WORK/$name.go.norm"
  normalize_ruby < "$WORK/$name.rb.out" > "$WORK/$name.rb.norm"

  local problems=()

  # Additional finding worth its own branch: `specguard-lint --source FILE`
  # exits 2 with "invalid option: --source" (files are positional), and a
  # harness that mirrored the port's flag would compare two empty reports and
  # call it parity. A gem that cannot LOAD — a missing runtime dependency, the
  # wrong GEM_HOME — lands on the same 2 by design (`CLI#run`), and lands
  # here too. Both are "it checked nothing", which is why the gem's stderr is
  # reproduced below rather than merely noted: the difference between those two
  # causes is in it, and neither is recoverable from a diff of empty reports.
  if [ "$rb_rc" = "2" ]; then
    problems+=("the gem exited 2 — it checked nothing (misuse, or it could not load)")
  fi

  [ "$go_rc" = "$rb_rc" ] || problems+=("exit code: go=$go_rc ruby=$rb_rc")
  cmp -s "$WORK/$name.go.norm" "$WORK/$name.rb.norm" || problems+=("findings differ")
  cmp -s /dev/null "$WORK/$name.rb.err" || problems+=("the gem wrote to stderr, which no shared case should")
  cmp -s /dev/null "$WORK/$name.go.err" || problems+=("the port wrote to stderr, which no shared case should")

  # The half the normalisation deleted.
  local go_c rb_c
  go_c="$(go_counts "$WORK/$name.go.out")"
  if ! rb_c="$(ruby_counts "$WORK/$name.rb.out")"; then
    problems+=("the gem printed no summary line — nothing to cross-check the counts against")
    rb_c=""
  elif [ "$go_c" != "$rb_c" ]; then
    problems+=("counts differ (annotations malformed unread): go=[$go_c] ruby=[$rb_c]")
  fi

  if [ ${#problems[@]} -eq 0 ]; then
    annotations_compared=$((annotations_compared + ${go_c%% *}))
    passed=$((passed + 1))
    printf '  ok    %s\n' "$label"
    return 0
  fi

  failed=$((failed + 1))
  red "  FAIL  $label"
  printf '        files: %s\n' "$*"
  local problem
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  if ! cmp -s "$WORK/$name.go.norm" "$WORK/$name.rb.norm"; then
    printf '        --- findings (-go +ruby), after normalisation ---\n'
    diff -u "$WORK/$name.go.norm" "$WORK/$name.rb.norm" | tail -n +3 | sed 's/^/        /'
  fi
  if [ -s "$WORK/$name.rb.err" ]; then
    printf '        --- the gem'"'"'s stderr ---\n'
    sed 's/^/        /' "$WORK/$name.rb.err"
  fi
  if [ -s "$WORK/$name.go.err" ]; then
    printf '        --- the port'"'"'s stderr ---\n'
    sed 's/^/        /' "$WORK/$name.go.err"
  fi
  return 1
}

# assert <label> <condition-description> — records a pass/fail directly, for
# the ratified differences whose whole content is "these two are NOT equal, in
# this exact way".
pass_case() { passed=$((passed + 1)); printf '  ok    %s\n' "$1"; }
fail_case() {
  failed=$((failed + 1))
  red "  FAIL  $1"
  shift
  local detail
  for detail in "$@"; do printf '        %s\n' "$detail"; done
}
# A case the ENVIRONMENT prevented from running — not a missing participant,
# which is exit 2 above, and not a pass. It is red, it is counted, and the
# verdict line repeats it, so a run that skipped something never reads as a run
# that checked it. Same treatment run_parity.sh gives its own two.
skip_case() {
  skipped=$((skipped + 1))
  red "  SKIP  $1"
  shift
  local detail
  for detail in "$@"; do printf '        %s\n' "$detail"; done
}

# --------------------------------------------------------------------------- #
# 0. the harness can fail
# --------------------------------------------------------------------------- #
#
# Every rule above DELETES lines, and the corpus that matters most is the one
# where both sides are clean and both sides therefore normalise to nothing. If
# a rule were too greedy — say rule 1 matched `^FAIL` through a typo — every
# case would compare empty to empty and this file would report perfect parity
# forever. So the rules are run against inputs whose correct output is known,
# and the comparator is run against inputs that MUST disagree.
echo
echo "== 0. the harness can fail =="

cat > "$WORK/probe.go.out" <<'PROBE'
PASS  a_spec.rb:1
----  b_spec.rb — no @intent annotations
FAIL  a_spec.rb:9
        -> <root>: missing required property 'entity'
FAIL  c_spec.rb — could not read file: boom
PROBE
cat > "$WORK/probe.rb.out" <<'PROBE'
specguard-lint: checked 3 spec files
FAIL  a_spec.rb:9
        -> <root>: missing required property 'entity'
FAIL  c_spec.rb — could not read file: boom
specguard-lint: checked 2 @intent annotations, 1 malformed; 1 file could not be read
PROBE
cat > "$WORK/probe.expected" <<'PROBE'
FAIL  a_spec.rb:9
        -> <root>: missing required property 'entity'
FAIL  c_spec.rb — could not read file: boom
PROBE

normalize_go   < "$WORK/probe.go.out" > "$WORK/probe.go.norm"
normalize_ruby < "$WORK/probe.rb.out" > "$WORK/probe.rb.norm"

if cmp -s "$WORK/probe.go.norm" "$WORK/probe.expected"; then
  pass_case "rules 1-2 delete PASS and ---- and keep every FAIL byte"
else
  fail_case "rules 1-2 deleted or kept the wrong lines" \
    "$(diff -u "$WORK/probe.expected" "$WORK/probe.go.norm" | tail -n +3)"
fi

if cmp -s "$WORK/probe.rb.norm" "$WORK/probe.expected"; then
  pass_case "rules 3-4 delete BOTH summary lines and keep every FAIL byte"
else
  fail_case "rules 3-4 deleted or kept the wrong lines" \
    "$(diff -u "$WORK/probe.expected" "$WORK/probe.rb.norm" | tail -n +3)"
fi

# The counts recovered from the deleted halves must agree with each other on a
# fixture whose answer is known by hand: 1 PASS + 1 line-scoped FAIL = 2
# annotations, 1 malformed, 1 unread.
probe_go="$(go_counts "$WORK/probe.go.out")"
probe_rb="$(ruby_counts "$WORK/probe.rb.out")" || probe_rb="<none>"
if [ "$probe_go" = "2 1 1" ] && [ "$probe_rb" = "2 1 1" ]; then
  pass_case "the counts survive normalisation and agree (2 annotations, 1 malformed, 1 unread)"
else
  fail_case "count extraction is wrong" \
    "expected 2 1 1 from both" "go=[$probe_go] ruby=[$probe_rb]"
fi

# A summary line the gem never printed must NOT read as zero.
if ruby_counts "$WORK/probe.expected" >/dev/null 2>&1; then
  fail_case "an absent summary line was read as a count instead of a failure"
else
  pass_case "an absent gem summary line is a failure, not a silent zero"
fi

# And the comparator must be able to see a real difference.
printf 'FAIL  a_spec.rb:9\n' > "$WORK/probe.left"
printf 'FAIL  a_spec.rb:10\n' > "$WORK/probe.right"
if cmp -s "$WORK/probe.left" "$WORK/probe.right"; then
  fail_case "the comparison primitive cannot tell two different reports apart"
else
  pass_case "the comparison primitive reports a difference when there is one"
fi

# --------------------------------------------------------------------------- #
# 1. the schema both sides validate against
# --------------------------------------------------------------------------- #
#
# Everything below compares two validators. If they were reading DIFFERENT
# schemas, agreement would be a coincidence and disagreement would be
# unreadable — you could not tell a port defect from a stale vendored copy. The
# gem vendors the schema deliberately (`SCHEMA_PATH` in lib/specguard/rspec.rb
# — no cross-repo runtime dependency), which is exactly the arrangement that
# lets the two drift silently. Pin them.
echo
echo "== 1. the vendored schema is the canonical one =="

CANON_SCHEMA="$REPO_ROOT/schemas/open-test-intent.v1.json"
GEM_SCHEMA="$GEM_ROOT/lib/specguard/rspec/schemas/open-test-intent.v1.json"
if [ ! -f "$GEM_SCHEMA" ]; then
  fail_case "the gem's vendored schema is missing" "expected $GEM_SCHEMA"
elif cmp -s "$CANON_SCHEMA" "$GEM_SCHEMA"; then
  pass_case "specguard-rspec vendors schemas/open-test-intent.v1.json byte-for-byte"
else
  fail_case "the gem's vendored schema has drifted from the canonical one" \
    "canonical: $CANON_SCHEMA" "vendored:  $GEM_SCHEMA" \
    "$(diff -u "$CANON_SCHEMA" "$GEM_SCHEMA" | tail -n +3 | head -n 20)"
fi

# The corpus below is also vendored into the gem's own spec/fixtures/, where
# message_parity_spec.rb and the scanner specs read it. Same argument.
for pair in \
  "examples/sources/order_spec.rb:spec/fixtures/order_spec.rb" \
  "examples/sources/invalid/broken_intent_spec.rb:spec/fixtures/broken_intent_spec.rb"
do
  here="$REPO_ROOT/${pair%%:*}"
  there="$GEM_ROOT/${pair##*:}"
  if [ ! -f "$there" ]; then
    fail_case "the gem no longer vendors ${pair##*:}" "expected $there"
  elif cmp -s "$here" "$there"; then
    pass_case "the gem vendors ${pair%%:*} byte-for-byte"
  else
    fail_case "the gem's ${pair##*:} has drifted from ${pair%%:*}" \
      "$(diff -u "$here" "$there" | tail -n +3 | head -n 20)"
  fi
done

# --------------------------------------------------------------------------- #
# 2. the shipped source corpus, one file at a time
# --------------------------------------------------------------------------- #
#
# EVERY file under examples/sources/, at any depth, not only the .rb ones. The
# gem checks the files it is handed without inspecting the extension, and the
# .spec.js and _test.py fixtures carry annotations in comment syntaxes the
# scanner has never been shown by the gem's own suite — which makes them free
# coverage rather than scope creep.
#
# `find -type f`, not a glob, and deliberately: this enumerator decides what
# "the corpus" MEANS, so anything it fails to list is uncovered without ever
# being reported as uncovered. The two globs this replaced each dropped files
# silently — `*.*` requires a dot, so an extensionless fixture vanished, and
# naming `invalid/` explicitly covered exactly the one subdirectory that
# existed the day it was written. That is the same silent-omission shape the
# empty-corpus branch below exists to prevent, just with a smaller blast
# radius, and it does not belong in the one file whose job is to be loud.
echo
echo "== 2. the shipped source corpus, one file at a time =="

corpus=()
while IFS= read -r -d '' fixture; do
  corpus+=("${fixture#"$REPO_ROOT"/}")
done < <(find "$REPO_ROOT/examples/sources" -type f -print0 | sort -z)

if [ ${#corpus[@]} -eq 0 ]; then
  fail_case "examples/sources/ is empty — there is no corpus to compare" \
    "a corpus that vanished must fail here, not quietly compare nothing"
else
  for rel in "${corpus[@]}"; do
    compare "$rel" "$rel"
  done
fi

# --------------------------------------------------------------------------- #
# 3. several files at once — report order and exit-code aggregation
# --------------------------------------------------------------------------- #
#
# The order findings come out in is part of the output, and the exit code of a
# mixed run is the aggregate rather than the last file's. Both are places a
# port and a re-implementation can agree per-file and still disagree here.
echo
echo "== 3. several files at once =="

compare "the whole corpus in one invocation" "${corpus[@]}"
compare "valid then invalid" \
  examples/sources/order_spec.rb examples/sources/invalid/broken_intent_spec.rb
compare "invalid then valid" \
  examples/sources/invalid/broken_intent_spec.rb examples/sources/order_spec.rb
compare "the same file twice (not de-duplicated)" \
  examples/sources/invalid/broken_intent_spec.rb examples/sources/invalid/broken_intent_spec.rb

# --------------------------------------------------------------------------- #
# 4. the extraction and normalisation surface
# --------------------------------------------------------------------------- #
#
# The shipped corpus exercises the happy paths and five failure shapes. These
# are the shapes it does NOT reach, built here with printf so the exact bytes
# are readable beside the reason for them. They are the two implementations'
# only genuinely independent re-derivations of PROTOCOL.md §1 — the port was
# written against the Python reference, the gem was not — so agreement here is
# the strongest evidence in this file.
echo
echo "== 4. extraction and normalisation hazards =="

SRC="$WORK/sources"
mkdir -p "$SRC"

# A file with no annotation at all is not a failure in either tool — and it is
# the case where the port prints `----` and the gem prints nothing, i.e. the
# one rule 2 exists for. Exercised rather than assumed.
printf 'describe Order do\n  it "works" do\n  end\nend\n' > "$SRC/unannotated_spec.rb"
compare "a file with no annotations is not a failure" "$SRC/unannotated_spec.rb"

# The permissive syntax the normalizer exists for: single quotes, bare-word
# keys, trailing commas, and braces/brackets inside the payload's own strings.
cat > "$SRC/permissive_spec.rb" <<'RUBY'
# @intent: {'entity': 'Order', 'action': 'create', 'behavior': "it's got a { brace } and a ] bracket", 'layer': 'unit',}
# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit", preconditions: ["a cart exists", "the cart has items",] }
# @intent: {entity:"Order",action:"create",behavior:"creates an order from a valid cart",layer:"unit"}
RUBY
compare "permissive syntax: quotes, trailing commas, nested brackets" "$SRC/permissive_spec.rb"

# Trailing prose carrying its OWN brace pair: only a balanced, string-aware
# scan captures the right substring. A greedy regex swallows the tail, a lazy
# one truncates mid-object, and both then report a parse error that looks
# nothing like agreement.
printf '# @intent: { entity: "Order", action: "refund", behavior: "restores stock levels when a paid order is refunded", layer: "unit" } \342\200\224 see ADR-14 {\302\2473} for why.\n' \
  > "$SRC/brace_tail_spec.rb"
compare "trailing prose with its own brace pair" "$SRC/brace_tail_spec.rb"

# Two annotations on one line: scanning must resume after the first payload.
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" } and @intent: { entity: "Cart", action: "empty", behavior: "empties the cart when the order is placed", layer: "unit" }\n' \
  > "$SRC/two_on_one_line_spec.rb"
compare "two annotations on one line" "$SRC/two_on_one_line_spec.rb"

# The four extraction failures, in one file so their ORDER is compared too.
cat > "$SRC/extraction_failures_spec.rb" <<'RUBY'
# @intent: no object literal here
# @intent: { entity: "Order", action: "create"
# @intent: { entity: "Order }
# @intent: { entity: "Order"]
RUBY
compare "extraction failures: all four problem strings" "$SRC/extraction_failures_spec.rb"

# minLength counts CODE POINTS, not bytes. This behavior is 12 characters and
# 30 bytes, so a byte-counting implementation clears the minLength of 15 and
# calls the file valid — a disagreement no amount of ASCII corpus would show.
printf '# @intent: { entity: "Order", action: "create", behavior: "\345\217\226\346\266\210\346\263\250\346\226\207\346\230\257\345\217\257\350\203\275\347\232\204", layer: "unit" }\n' \
  > "$SRC/multibyte_spec.rb"
compare "minLength counts code points, not bytes" "$SRC/multibyte_spec.rb"

# Every schema violation shape the shipped corpus does not put in one file:
# a wrong enum value, a missing required property, an unknown property, and a
# short behavior — plus the ORDER they are reported in, which is document order
# for the properties and not alphabetical.
cat > "$SRC/violations_spec.rb" <<'RUBY'
# @intent: { zzz: "last", entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "model" }
# @intent: { action: "create", behavior: "short", layer: "unit" }
RUBY
compare "violation order and shape" "$SRC/violations_spec.rb"

# A payload that is syntactically fine JSON but is not an object.
printf '# @intent: [ "not", "an", "object" ]\n' > "$SRC/not_an_object_spec.rb"
compare "a non-object payload" "$SRC/not_an_object_spec.rb"

# --------------------------------------------------------------------------- #
# 5. ratified difference (a): a spec file that is not valid UTF-8
# --------------------------------------------------------------------------- #
#
# See "The two ratified read-failure differences" in the header. Verdict, file,
# stream, prefix and exit code are pinned; only the reason text is not — and
# the fact that it still differs is itself pinned, so closing the gap breaks
# this case and retires the ratification instead of rotting it.
echo
echo "== 5. ratified difference (a): non-UTF-8 input =="

BAD_UTF8="$SRC/latin1_spec.rb"
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n# caf\351 -- that byte is not valid UTF-8\n' \
  > "$BAD_UTF8"

run_go   "utf8" "$BAD_UTF8"
run_ruby "utf8" "$BAD_UTF8"

go_rc="$(cat "$WORK/utf8.go.rc")"; rb_rc="$(cat "$WORK/utf8.rb.rc")"
go_line="$(grep '^FAIL  ' "$WORK/utf8.go.out" | head -n 1)"
rb_line="$(grep '^FAIL  ' "$WORK/utf8.rb.out" | head -n 1)"
prefix="FAIL  $BAD_UTF8 — could not read file: "
go_tail="${go_line#"$prefix"}"
rb_tail="${rb_line#"$prefix"}"

problems=()
[ "$go_rc" = "1" ] || problems+=("the port did not exit 1 (got $go_rc)")
[ "$rb_rc" = "1" ] || problems+=("the gem did not exit 1 (got $rb_rc)")
[ "$go_line" != "$rb_line" ] || problems+=("the two agree byte-for-byte now — RETIRE this ratification and compare the line directly (header, ratified difference (a))")
[ "$go_tail" != "$go_line" ] || problems+=("the port's line is not 'FAIL <file> — could not read file: ...': $go_line")
[ "$rb_tail" != "$rb_line" ] || problems+=("the gem's line is not 'FAIL <file> — could not read file: ...': $rb_line")
[ -n "$go_tail" ] || problems+=("the port gave no reason at all")
[ -n "$rb_tail" ] || problems+=("the gem gave no reason at all")
[ "$(grep -c '^FAIL  ' "$WORK/utf8.go.out")" = "1" ] || problems+=("the port reported more than the one read failure")
[ "$(grep -c '^FAIL  ' "$WORK/utf8.rb.out")" = "1" ] || problems+=("the gem reported more than the one read failure")
cmp -s /dev/null "$WORK/utf8.go.err" || problems+=("the port wrote to stderr; the reference puts read failures on stdout")
cmp -s /dev/null "$WORK/utf8.rb.err" || problems+=("the gem wrote to stderr; findings belong on stdout (CLI#report_results)")
# The gem must also not count an unread file as an annotation it inspected.
if rb_c="$(ruby_counts "$WORK/utf8.rb.out")"; then
  [ "$rb_c" = "0 0 1" ] || problems+=("the gem's summary should be 0 annotations, 0 malformed, 1 unread — got [$rb_c]")
else
  problems+=("the gem printed no summary line")
fi
[ "$(go_counts "$WORK/utf8.go.out")" = "0 0 1" ] || problems+=("the port's counts should be 0 annotations, 0 malformed, 1 unread — got [$(go_counts "$WORK/utf8.go.out")]")

if [ ${#problems[@]} -eq 0 ]; then
  pass_case "same verdict, same file, same prefix, exit 1 — reason text ratified as different"
  dim "        port: $go_tail"
  dim "        gem:  $rb_tail"
else
  fail_case "ratified difference (a) no longer holds as written" "${problems[@]}"
fi

# --------------------------------------------------------------------------- #
# 6. ratified difference (b): a named file that does not exist
# --------------------------------------------------------------------------- #
#
# See "The two ratified read-failure differences" in the header. This one
# differs in stream and shape, not only wording, because the port's argument is
# a glob PATTERN and the gem's is a PATH. What is shared — and what is actually
# load-bearing for CI — is asserted: neither goes green, both name the file,
# and neither abandons the rest of the run.
echo
echo "== 6. ratified difference (b): a file that does not exist =="

MISSING="$SRC/nope_spec.rb"
rm -f "$MISSING"

run_go   "missing" "$MISSING"
run_ruby "missing" "$MISSING"

go_rc="$(cat "$WORK/missing.go.rc")"; rb_rc="$(cat "$WORK/missing.rb.rc")"
problems=()
[ "$go_rc" = "1" ] || problems+=("the port did not exit 1 (got $go_rc) — a missing file must never be green")
[ "$rb_rc" = "1" ] || problems+=("the gem did not exit 1 (got $rb_rc) — a missing file must never be green")
grep -q "no file(s) match '$MISSING'" "$WORK/missing.go.err" \
  || problems+=("the port no longer writes \"error: no file(s) match '<path>'\" to stderr")
cmp -s /dev/null "$WORK/missing.go.out" \
  || problems+=("the port wrote to stdout; its no-match diagnostic belongs on stderr")
grep -q "^FAIL  $MISSING — could not read file: " "$WORK/missing.rb.out" \
  || problems+=("the gem no longer writes 'FAIL <path> — could not read file: ...' to stdout")
cmp -s /dev/null "$WORK/missing.rb.err" \
  || problems+=("the gem wrote to stderr; findings belong on stdout (CLI#report_results)")
if rb_c="$(ruby_counts "$WORK/missing.rb.out")"; then
  [ "$rb_c" = "0 0 1" ] || problems+=("the gem's summary should be 0 annotations, 0 malformed, 1 unread — got [$rb_c]")
else
  problems+=("the gem printed no summary line")
fi
# If the two ever converge, this ratification is the thing that is now wrong.
if cmp -s "$WORK/missing.go.out" "$WORK/missing.rb.out" \
   && cmp -s "$WORK/missing.go.err" "$WORK/missing.rb.err"; then
  problems+=("the two agree byte-for-byte now — RETIRE this ratification (header, ratified difference (b))")
fi

if [ ${#problems[@]} -eq 0 ]; then
  pass_case "both exit 1 and both name the missing file — shape ratified as different"
else
  fail_case "ratified difference (b) no longer holds as written" "${problems[@]}"
fi

# The case with teeth, and the half of (b) that IS shared: a missing file
# alongside real ones must not abort either run. Compared by count rather than
# by bytes, because the missing file's own report is the ratified difference
# above — everything else about the invocation must agree.
echo
run_go   "mixed" "$MISSING" examples/sources/order_spec.rb
run_ruby "mixed" "$MISSING" examples/sources/order_spec.rb

go_c="$(go_counts "$WORK/mixed.go.out")"
rb_c="$(ruby_counts "$WORK/mixed.rb.out")" || rb_c="<none>"
mixed_go_rc="$(cat "$WORK/mixed.go.rc")"; mixed_rb_rc="$(cat "$WORK/mixed.rb.rc")"
problems=()
# 7 annotations in order_spec.rb, none malformed. The port counts 0 unread (the
# missing file never became a file); the gem counts 1. That single number is
# the whole of the ratified difference, and pinning both sides of it is what
# keeps it a decision rather than a shrug.
[ "$go_c" = "7 0 0" ] || problems+=("the port checked [$go_c], expected [7 0 0] — the missing file aborted the run")
[ "$rb_c" = "7 0 1" ] || problems+=("the gem checked [$rb_c], expected [7 0 1] — the missing file aborted the run")
[ "$mixed_go_rc" = "1" ] || problems+=("the port exited $mixed_go_rc, expected 1")
[ "$mixed_rb_rc" = "1" ] || problems+=("the gem exited $mixed_rb_rc, expected 1")
# The good file's findings must be identical: no FAIL blocks from either, and
# the same PASS/annotation count, already checked above.
normalize_go   < "$WORK/mixed.go.out" > "$WORK/mixed.go.norm"
normalize_ruby < "$WORK/mixed.rb.out" \
  | grep -v "^FAIL  $MISSING — " > "$WORK/mixed.rb.norm"
cmp -s "$WORK/mixed.go.norm" "$WORK/mixed.rb.norm" \
  || problems+=("the good file's findings differ once the missing file's own line is set aside")

if [ ${#problems[@]} -eq 0 ]; then
  annotations_compared=$((annotations_compared + 7))
  pass_case "a missing file does not abort the rest of the run on either side"
else
  fail_case "a missing file changed what else got checked" "${problems[@]}"
fi

# --------------------------------------------------------------------------- #
# 7. an unreadable file
# --------------------------------------------------------------------------- #
#
# The third read-failure shape, and the one where the two DO agree in kind:
# a file that exists and cannot be opened. Under root — which is most container
# CI — chmod 000 does not make a file unreadable, so the case cannot be
# constructed. That is a loud SKIP rather than a failure (the harness would
# otherwise be permanently red for a reason that has nothing to do with parity)
# and rather than silence (a case that did not run must not read as one that
# passed): it is counted, and the verdict line says so.
echo
echo "== 7. an unreadable file =="

UNREADABLE="$SRC/unreadable_spec.rb"
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$UNREADABLE"
chmod 000 "$UNREADABLE"
if [ -r "$UNREADABLE" ]; then
  skip_case "an unopenable file — NOT verified by this run" \
    "chmod 000 did not make the file unreadable, which means this is running" \
    "as root. Nothing about the unopenable-file path was compared."
else
  run_go   "unread" "$UNREADABLE"
  run_ruby "unread" "$UNREADABLE"
  go_rc="$(cat "$WORK/unread.go.rc")"; rb_rc="$(cat "$WORK/unread.rb.rc")"
  problems=()
  [ "$go_rc" = "1" ] || problems+=("the port did not exit 1 (got $go_rc)")
  [ "$rb_rc" = "1" ] || problems+=("the gem did not exit 1 (got $rb_rc)")
  grep -q "^FAIL  $UNREADABLE — could not read file: " "$WORK/unread.go.out" \
    || problems+=("the port did not report it as a read failure")
  grep -q "^FAIL  $UNREADABLE — could not read file: " "$WORK/unread.rb.out" \
    || problems+=("the gem did not report it as a read failure")
  [ "$(go_counts "$WORK/unread.go.out")" = "0 0 1" ] \
    || problems+=("the port's counts should be 0 0 1 — got [$(go_counts "$WORK/unread.go.out")]")
  if rb_c="$(ruby_counts "$WORK/unread.rb.out")"; then
    [ "$rb_c" = "0 0 1" ] || problems+=("the gem's counts should be 0 0 1 — got [$rb_c]")
  else
    problems+=("the gem printed no summary line")
  fi
  if [ ${#problems[@]} -eq 0 ]; then
    pass_case "both classify an unopenable file as a read failure and exit 1"
  else
    fail_case "the two disagree on an unopenable file" "${problems[@]}"
  fi
fi
chmod 644 "$UNREADABLE" 2>/dev/null

# --------------------------------------------------------------------------- #
# 8. the participants are unmodified
# --------------------------------------------------------------------------- #
#
# This leg adds only. If parity were "achieved" by editing the corpus or the
# gem's reporter to match, every case above would still be green — so say so
# rather than assume it. The port itself is deliberately not covered here: it
# is rebuilt from cmd/ above when a toolchain exists, and run_parity.sh already
# guards the Python oracle.
echo
echo "== 8. the corpus is unmodified =="

if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  dirty="$(git -C "$REPO_ROOT" status --porcelain -- examples/sources schemas)"
  if [ -n "$dirty" ]; then
    fail_case "the shared corpus or schema has local modifications" "$dirty"
  else
    pass_case "examples/sources and schemas are unmodified"
  fi
else
  skip_case "the corpus was NOT checked for local modifications" \
    "not a git checkout, so this run cannot tell a genuine agreement from one" \
    "produced by editing examples/sources or schemas to match."
fi

# --------------------------------------------------------------------------- #
# verdict
# --------------------------------------------------------------------------- #
echo
total=$((passed + failed))
if [ "$failed" -gt 0 ]; then
  red "$failed/$total Ruby-parity cases FAILED ($passed passed, $skipped skipped)"
  exit 1
fi
if [ "$passed" -eq 0 ]; then
  red "error: no cases ran — the Ruby leg verified nothing"
  exit 1
fi
# The counts are what make an all-clean corpus non-vacuous, so a run that
# compared zero annotations is not a pass either, however many cases it ran.
if [ "$annotations_compared" -eq 0 ]; then
  red "error: $passed cases ran but not one annotation was compared —"
  red "       the corpus normalised away to nothing, which is not parity"
  exit 1
fi
green "$passed/$total Ruby-parity cases passed ($annotations_compared annotations compared, $skipped skipped)"
if [ "$skipped" -gt 0 ]; then
  dim "note: skipped cases were not verified — see the SKIP lines above"
fi
exit 0
