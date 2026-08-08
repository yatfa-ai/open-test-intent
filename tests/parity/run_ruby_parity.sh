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
# gem -> here: nothing enforces the number↔name agreement for any file, so a
# numeric citation of a section in THIS file, and especially one from the other
# repo (lib/specguard/rspec/{scanner,linter}.rb both point back here), is
# unchecked and rots the first time a section is inserted. The names are stable
# and searchable; the numbers are for reading the output, not for citing.
#
# here -> gem: the same rule, and it is not hypothetical. This file first
# shipped citing the gem by line number while the companion commit added 35
# lines above the cited site in the very same changeset — so the citation for
# ratified difference (a) landed pointing at the code for ratified difference
# (b). Nothing catches that, and no checker in either repo could, because
# neither repo has the other at test time by construction (that is the whole
# premise above). So cite the gem by SYMBOL — `CLI#report_results`,
# `Scanner.scan_text`, `SCHEMA_PATH` — which survives exactly the edit that
# broke the numbers, and which `grep` resolves in one hop.
#
#
# The four ratified message differences
# --------------------------------------
# Four inputs make the gem and the port disagree. Three of them ((a), (b), (c))
# disagree in TEXT while agreeing in verdict and exit code; the fourth, (d),
# disagrees in classification and — in one of its two shapes — in the verdict
# itself, which is why "message differences" is the wrong name for the group
# and is kept only because the first three wear it. All four are ratified here
# rather than "fixed", and all four are ASSERTED — including the assertion that
# they still differ — so that closing one of them fails this file and forces the
# ratification to be retired, instead of leaving a stale exclusion nobody
# notices.
#
# Two are read failures. (c) and (d) are not: they are ANNOTATION-level, and
# they are the ones an ordinary user actually meets, because they need nothing
# more exotic than a typo in an @intent payload.
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
#   (c) An @intent payload that survives normalisation and still is not JSON.
#       port: FAIL <file>:<line> — could not parse annotation: Expecting
#             property name enclosed in double quotes: line 1 column 2 (char 1)
#       gem:  FAIL <file>:<line> — could not parse annotation: expected object
#             key, got 'bad_key}' at line 1 column 2
#
#       RATIFIED, and this is the one difference in this file that is NOT about
#       an unreadable file. It went unnoticed for a whole slice, which is worth
#       recording because the reason is structural rather than careless: the
#       hazards in "the extraction and normalisation surface" all exercise
#       PayloadNormalizer, whose JOB is to make PROTOCOL.md §1's permissive
#       syntax parse SUCCESSFULLY — single quotes, bare-word keys, trailing
#       commas. Every one of those compares byte-identical, correctly. Only a
#       payload that is still broken AFTER normalisation reaches a JSON parser
#       in an error state, and until section 4b existed the corpus contained no
#       such file. A gap in the corpus is invisible by construction; this is the
#       file that says so out loud.
#
#       The cause is the same one as (a), one layer up. `Scanner#parse`
#       interpolates RUBY's `JSON::ParserError#message`; `bin/validate-intent`
#       lets CPython describe the failure and the port reproduces CPython's
#       wording deliberately (cmd/validate-intent/pyjson.go). Reproducing that
#       tail in the gem would mean porting CPython's JSON diagnostics into a
#       gem whose entire reason for vendoring the schema is to owe this repo
#       nothing at runtime — (a)'s argument about CPython's DECODER, applied
#       verbatim to its PARSER, which is why it is ratified rather than closed.
#
#       Asserted below: same classification, same file, the same LINE and even
#       the same COLUMN (unlike a read failure this one is line-scoped, and the
#       two normalisers agree on exactly where the payload went wrong), the same
#       `could not parse annotation: ` prefix, the same counts, and exit 1. Only
#       the tail is unpinned — and the fact that it still differs is pinned.
#
#       NOTE the scope of "same classification" here: it holds for the payloads
#       BOTH parsers reject, which is what 4b's fixtures are. It does not hold
#       in general — see (d), which is the reason why, and which an earlier
#       revision of this header contradicted by stating the general case.
#
#   (d) An @intent payload CPython's `json` accepts and Ruby's `JSON.parse`
#       does not.
#       port: FAIL <file>:<line>
#                     -> <root>: additional property 'x' is not allowed
#       gem:  FAIL <file>:<line> — could not parse annotation: unexpected
#             token 'NaN' at line 1 column 114
#
#       RATIFIED, and it is NOT a message difference — it is the GENERATOR
#       behind (c), and naming it that way is the only reason (c) can be
#       trusted to be about wording. The two implementations parse with two
#       different JSON parsers, and those parsers do not accept the same
#       LANGUAGE. CPython's is strictly the more permissive, so there is a set
#       of payloads one takes and the other refuses, and every member of it
#       diverges by more than its prose.
#
#       Derived rather than guessed, because guessing is what produced (c)'s
#       too-narrow framing and then defended it for a slice: 89,108 documents —
#       cmd/validate-intent/pyjson_fuzz_test.go's own seed corpus and token
#       alphabet, a generated well-formed corpus, and every one of the 65,536
#       single \uXXXX escapes — through both parsers. Widening the sweep did
#       not add a member. Exactly three:
#
#         1. NaN / Infinity / -Infinity. cmd/validate-intent/pyjson.go's "WHAT
#            IT BUYS BEYOND THE PROSE" is explicit that accepting these was the
#            point of that file: an earlier Go decoder rejected them, "which
#            made Go classify such a document as a parse failure where Python
#            reported a schema violation". That is this divergence, described
#            in this repo, before it existed here — closed between Go and
#            Python, reopened between Ruby and Go.
#         2. a HIGH surrogate escape (\ud800-\udbff) not followed by a LOW one.
#            A lone LOW surrogate is accepted by both parsers, so the rule is
#            narrower than "surrogate escapes differ"; the boundary is pinned.
#         3. nesting past Ruby's `max_nesting: 100` default.
#
#       Two shapes, and the second is the strongest disagreement anywhere in
#       this file:
#
#         (i)  the payload is NOT schema-valid. The gem never gets a value to
#              validate and reports a one-line parse failure; the port parses
#              it and reports schema violations. Same file, same line, same
#              counts, same exit code — but a different CLASSIFICATION and a
#              different rendered block, so there is no shared prefix to split
#              and no tail to compare. (c)'s machinery cannot express this.
#         (ii) the payload IS schema-valid. The gem reports it malformed and
#              exits 1; the port passes it and exits 0. Only the file agrees.
#              Reachable through member 2 alone, and provably: a schema-valid
#              payload must put the offending syntax in a value the schema
#              permits, and every such value in
#              schemas/open-test-intent.v1.json is a string or an array of
#              strings — a number and a container cannot occupy a string slot,
#              a surrogate escape can.
#
#       RATIFIED for scope rather than preference, and the gem states the
#       reasoning at `Scanner#parse`: `allow_nan: true` closes member 1 and
#       `max_nesting: false` closes member 3, but member 2 has no such option
#       and would need CPython's string decoder ported into the gem — (a)'s
#       argument a third time — so closing two of three would leave the set
#       inconsistent, and all three would change the gem's DEFAULT path, which
#       the slice adding the backend holds fixed. Whether the gem should adopt
#       CPython's acceptance grammar wholesale is left open there.
#
#       Asserted in 4c, both shapes, including the boundary non-members.
#
#
# The THIRD leg: the gem's own Go backend
# ---------------------------------------
# Everything above compares two IMPLEMENTATIONS. Section 8 compares two
# BACKENDS of the same implementation: `specguard-lint` with
# SPECGUARD_VALIDATE_INTENT pointing at the port, against `specguard-lint` with
# it unset. That is the slice the roadmap's final clause actually needs — "the
# Ruby gem invoking it for linting" — and the claim it makes is stronger than
# anything above, because there is nothing to normalise away: the leading
# selection line, the trailing summary line, every FAIL block, stderr and the
# exit code must be IDENTICAL, byte for byte, with no rules applied at all.
#
# That strength is exactly why it needs its own anti-vacuity guard, and it is a
# different one from the counts above. A gem checkout that predates
# SPECGUARD_VALIDATE_INTENT does not fail when handed it — it IGNORES it, runs
# the Ruby path twice, and compares two identical reports to a perfect green.
# So the preflight below points the variable at a path that does not exist and
# requires exit 2: a gem that honours the variable refuses, and a gem that does
# not answers 0. Nothing else in this file can tell those two apart.
#
# The gem retains its Ruby path deliberately (see SpecGuard::RSpec::
# ValidatorBackend) — the port is a build artifact with no release, so a
# default-on shell-out would break every existing user — which is why "the two
# backends agree" is the property under test rather than "the Ruby code is
# gone".
#
# SIX ENUMERATED DIFFERENCES, three of them read-failure PROSE and three of them
# not, all of them asserted in section 8b rather than discovered:
#
#   (i)   a file that is not valid UTF-8 — ratified difference (a) again, now
#         seen from inside the gem: with the backend on, the gem emits the
#         PORT's decoder text verbatim instead of `Scanner.scan_text`'s fixed
#         string. Same classification, same prefix, same counts, same exit.
#   (ii)  a path that does not exist, and (iii) a path that is not a regular
#         file. Both are the port's `no-match` kind, which has no
#         `Finding::KIND_*` of its own and folds into KIND_READ. The gem cannot
#         name the errno the Ruby path names (`No such file or directory @
#         rb_sysopen - ...`, `Is a directory @ io_fread - ...`) because the
#         backend never told it one, so it says `could not read file: no file
#         at this path` in its own words. Everything else — the stream, the
#         `FAIL <file> — could not read file: ` prefix, the "N file(s) could
#         not be read" clause, the exit code — is identical.
#   (iv)  an @intent payload that survives normalisation and still is not JSON
#         — ratified difference (c), seen from inside the gem. Like (i) it is a
#         PASS-THROUGH: the backend reproduces the port's CPython-shaped
#         diagnostic verbatim where the Ruby path spells it Ruby's way.
#
#         This is not a read failure, and it deserves emphasis rather than a
#         footnote: (i)-(iii) all need an unreadable path to reach at all,
#         whereas (iv) fires on an ordinary typo in an annotation. `parse` is
#         one of the three things the linter exists to report, so for a user
#         flipping the variable on, this — not the read failures — is the
#         difference they will actually see, on every malformed annotation in
#         their CI log. Anything in this repo or the gem claiming the backends
#         differ "only in read-failure messages" is wrong, and was: the claim
#         shipped in both READMEs and in `ValidatorBackend`'s own comment
#         before this entry existed.
#   (v)   and (vi) — ratified difference (d), the JSON ACCEPTANCE SET, seen
#         from inside the gem. These are not messages: in (v) the two backends
#         give the same annotation a different CLASSIFICATION (a one-line parse
#         failure against a block of schema reasons), and in (vi) they give the
#         same file a different VERDICT and a different EXIT CODE.
#
#         (vi) is the only entry in this file where flipping the variable can
#         turn a red run green. It is also the second time the same mistake was
#         caught: (iv) existed because the hazard corpus only held payloads the
#         normalizer RESCUES, and (v)/(vi) exist because the corpus was then
#         extended by exactly the one case a reviewer happened to find, rather
#         than by sweeping the generator. They arrive as a pair because
#         sweeping it produces a pair. The claim that had to be withdrawn this
#         time was narrower and more specific than last time — "with the same
#         classification", "four messages differ in their trailing text only" —
#         which is what a claim looks like just before it is falsified.
#
# Each is asserted to STILL DIFFER, like the ratifications above, so closing one
# fails this file rather than leaving a stale exclusion.
#
# WHAT SECTION 8 PROVES THAT NOTHING ELSE DOES: the gem's arguments are PATHS
# and the port's are GLOB PATTERNS, so the backend escapes every path with the
# port of Python's `glob.escape` before handing it over. `bracket[1]_spec.rb`
# and `star*_spec.rb` are in the corpus below for that reason alone — unescaped,
# the first matches nothing and the second can match OTHER files, and both
# failures look like a clean run.
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
# Section 8's own non-vacuity counter, kept separate from the one above so the
# two legs cannot borrow each other's evidence: a Go-vs-Ruby run that compared
# 300 annotations says nothing about whether the gem's two BACKENDS were ever
# pointed at one.
backend_annotations_compared=0

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

# The gem's Go BACKEND, which is a third participant and needs its own probe.
#
# A gem checkout that predates SPECGUARD_VALIDATE_INTENT does not fail when it
# is set — it ignores it. Section 8 would then run the Ruby path twice, compare
# two identical reports, and report perfect agreement having exercised no Go at
# all. That is this project's house defect ("vacuous green") in the one place
# the rest of the file cannot see, because section 8's whole claim is that two
# runs produce identical bytes.
#
# So: point the variable at a path that does not exist. A gem that honours it
# refuses with exit 2 and `specguard-lint: error: ...`; a gem that ignores it
# lints the file and answers 0. Then point it at the real binary and require a
# clean 0 on the same known-good file the gem preflight used, so "the backend is
# wired" and "the backend works" are two separate findings.
SPECGUARD_VALIDATE_INTENT="$WORK/there-is-no-binary-here" \
  "$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$WORK/preflight_spec.rb" \
  >"$WORK/backend_probe.out" 2>"$WORK/backend_probe.err"
backend_probe_rc=$?
if [ "$backend_probe_rc" != "2" ] || ! grep -q '^specguard-lint: error: ' "$WORK/backend_probe.err"; then
  red "error: the gem at $GEM_ROOT does not honour SPECGUARD_VALIDATE_INTENT"
  red "       pointed at a path that does not exist it exited $backend_probe_rc,"
  red "       where a gem carrying the Go backend exits 2 with"
  red "       'specguard-lint: error: ...' on stderr."
  red "       This checkout predates SpecGuard::RSpec::ValidatorBackend, or the"
  red "       backend no longer refuses a missing binary. Either way section 8"
  red "       would compare the Ruby path against itself and call it parity."
  if [ -s "$WORK/backend_probe.err" ]; then
    sed 's/^/       /' "$WORK/backend_probe.err"
  fi
  red "       nothing was compared — this is NOT a pass"
  exit 2
fi

SPECGUARD_VALIDATE_INTENT="$GO_BIN" \
  "$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$WORK/preflight_spec.rb" \
  >"$WORK/backend_live.out" 2>"$WORK/backend_live.err"
backend_live_rc=$?
if [ "$backend_live_rc" != "0" ] || ! grep -q '^specguard-lint: checked ' "$WORK/backend_live.out"; then
  red "error: the gem cannot run $GO_BIN as its validator backend"
  red "       it exited $backend_live_rc on a file carrying one valid annotation,"
  red "       where the only correct answer is 0 and a summary line."
  if [ -s "$WORK/backend_live.err" ]; then
    sed 's/^/       /' "$WORK/backend_live.err"
  fi
  red "       nothing was compared — this is NOT a pass"
  exit 2
fi
dim "backend: the gem honours SPECGUARD_VALIDATE_INTENT and runs $GO_BIN"

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
  # SPECGUARD_VALIDATE_INTENT is blanked, not merely left alone. Blank means
  # unset to the gem (SpecGuard::RSpec::ValidatorBackend.resolve, following
  # Configuration's blank_to_nil idiom), and an operator who happened to have
  # the backend exported would otherwise make every case above compare the port
  # against ITSELF through the gem — a green run that proved nothing about the
  # Ruby implementation this leg exists to check.
  (cd "$REPO_ROOT" && SPECGUARD_VALIDATE_INTENT= "$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$@" \
    >"$WORK/$name.rb.out" 2>"$WORK/$name.rb.err")
  printf '%s' "$?" > "$WORK/$name.rb.rc"
}

# run_ruby_go <name> [args...] — the same gem, same directory, same arguments,
# with its Go backend switched on. Writes $WORK/<name>.rbgo.{out,err,rc}.
run_ruby_go() {
  local name="$1"
  shift
  (cd "$REPO_ROOT" && SPECGUARD_VALIDATE_INTENT="$GO_BIN" "$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$@" \
    >"$WORK/$name.rbgo.out" 2>"$WORK/$name.rbgo.err")
  printf '%s' "$?" > "$WORK/$name.rbgo.rc"
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

# compare_backends <label> [files...] — the SAME gem, twice, with and without
# its Go backend. No normalisation: this pair has no ratified differences to
# remove, so every byte of stdout, every byte of stderr and the exit code must
# be identical. Anything that survives here survives because it is genuinely the
# same, not because a rule deleted it.
compare_backends() {
  local label="$1"
  shift
  local name="backend$((passed + failed))"

  run_ruby    "$name" "$@"
  run_ruby_go "$name" "$@"

  local rb_rc go_rc
  rb_rc="$(cat "$WORK/$name.rb.rc")"
  go_rc="$(cat "$WORK/$name.rbgo.rc")"

  local problems=()

  # Exit 2 from either side means the gem checked nothing. On the backend side
  # that is the whole failure class this slice had to get right — a missing or
  # broken binary must never be mistaken for a verdict — so it is named rather
  # than left to show up as an empty-report diff.
  [ "$rb_rc" = "2" ] && problems+=("the gem exited 2 on its Ruby path — it checked nothing")
  [ "$go_rc" = "2" ] && problems+=("the gem exited 2 with the Go backend — it checked nothing (see its stderr below)")

  [ "$rb_rc" = "$go_rc" ] || problems+=("exit code: ruby=$rb_rc backend=$go_rc")
  cmp -s "$WORK/$name.rb.out" "$WORK/$name.rbgo.out" || problems+=("stdout differs")
  cmp -s "$WORK/$name.rb.err" "$WORK/$name.rbgo.err" || problems+=("stderr differs")

  # Non-vacuity, the same argument the counts make for the Go-vs-Ruby leg: two
  # runs that both died before printing anything compare equal. The summary
  # line has to be there, and the corpus has to have contributed annotations.
  local rb_c go_c
  if ! rb_c="$(ruby_counts "$WORK/$name.rb.out")"; then
    problems+=("the Ruby path printed no summary line — nothing was checked")
    rb_c=""
  fi
  if ! go_c="$(ruby_counts "$WORK/$name.rbgo.out")"; then
    problems+=("the Go backend printed no summary line — nothing was checked")
    go_c=""
  fi

  if [ ${#problems[@]} -eq 0 ]; then
    backend_annotations_compared=$((backend_annotations_compared + ${rb_c%% *}))
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
  if ! cmp -s "$WORK/$name.rb.out" "$WORK/$name.rbgo.out"; then
    printf '        --- stdout (-ruby +backend) ---\n'
    diff -u "$WORK/$name.rb.out" "$WORK/$name.rbgo.out" | tail -n +3 | sed 's/^/        /'
  fi
  if ! cmp -s "$WORK/$name.rb.err" "$WORK/$name.rbgo.err"; then
    printf '        --- stderr (-ruby +backend) ---\n'
    diff -u "$WORK/$name.rb.err" "$WORK/$name.rbgo.err" | tail -n +3 | sed 's/^/        /'
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
# 4a. the extraction and normalisation surface
# --------------------------------------------------------------------------- #
#
# The shipped corpus exercises the happy paths and five failure shapes. These
# are the shapes it does NOT reach, built here with printf so the exact bytes
# are readable beside the reason for them. They are the two implementations'
# only genuinely independent re-derivations of PROTOCOL.md §1 — the port was
# written against the Python reference, the gem was not — so agreement here is
# the strongest evidence in this file.
#
# NOTE what this section structurally cannot reach, and why 4b exists: every
# hazard below goes THROUGH PayloadNormalizer, whose job is to make permissive
# syntax parse SUCCESSFULLY. A payload it rescues renders identically on both
# sides, which is what these cases prove. A payload it CANNOT rescue reaches a
# JSON parser in an error state, and there the two implementations quote two
# different parsers at each other. No case here builds one, so no case here can
# see that — see 4b.
echo
echo "== 4a. extraction and normalisation hazards =="

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
# 4b. ratified difference (c): a payload normalisation cannot rescue
# --------------------------------------------------------------------------- #
#
# See "The four ratified message differences" in the header. This is the one
# ratified difference that is not about an unreadable file, and the one this
# harness was missing entirely until it was found by hand: 4a's hazards all
# exercise the normalizer's SUCCESS path, so none of them reaches a JSON parser
# in an error state, and the divergence was invisible to a corpus that looked
# thorough.
#
# The two payloads below are the two shapes normalisation cannot fix — a
# bare-word VALUE, and a token that is not a key at all. Everything about the
# verdict is shared and is pinned: classification, file, line, column, prefix,
# counts, exit code. Only the tail is unpinned, and the fact that it still
# differs is itself pinned, so closing the gap breaks this case and retires the
# ratification instead of rotting it.
#
# The first annotation is deliberately VALID: a file of nothing but failures
# would compare two reports that agree only about what broke.
echo
echo "== 4b. ratified difference (c): a payload that is still not JSON =="

PARSE_FAIL="$SRC/parse_failure_spec.rb"
cat > "$PARSE_FAIL" <<'RUBY'
# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }
# @intent: { "entity": "Order", "action": "create", "behavior": "rejects an order missing a customer", "layer": unit }
# @intent: {bad_key}
RUBY

run_go   "parsefail" "$PARSE_FAIL"
run_ruby "parsefail" "$PARSE_FAIL"

go_rc="$(cat "$WORK/parsefail.go.rc")"; rb_rc="$(cat "$WORK/parsefail.rb.rc")"
grep '^FAIL  ' "$WORK/parsefail.go.out" > "$WORK/parsefail.go.fails" || true
grep '^FAIL  ' "$WORK/parsefail.rb.out" > "$WORK/parsefail.rb.fails" || true

# Split each FAIL line at the ratified prefix: the LOCATION half must match, the
# TAIL half must not. A line lacking the prefix survives the sed unchanged,
# which the emptiness checks below then catch.
PARSE_MARK=' — could not parse annotation: '
sed "s/${PARSE_MARK}.*\$//"  "$WORK/parsefail.go.fails" > "$WORK/parsefail.go.loc"
sed "s/${PARSE_MARK}.*\$//"  "$WORK/parsefail.rb.fails" > "$WORK/parsefail.rb.loc"
sed "s/^.*${PARSE_MARK}//"   "$WORK/parsefail.go.fails" > "$WORK/parsefail.go.tail"
sed "s/^.*${PARSE_MARK}//"   "$WORK/parsefail.rb.fails" > "$WORK/parsefail.rb.tail"

problems=()
[ "$go_rc" = "1" ] || problems+=("the port did not exit 1 (got $go_rc)")
[ "$rb_rc" = "1" ] || problems+=("the gem did not exit 1 (got $rb_rc)")
# Non-vacuity: without this, a run where neither side reported anything would
# satisfy every "they are equal" and "they differ" check below trivially.
[ "$(count_matching "$WORK/parsefail.go.fails" '^FAIL  ')" = "2" ] \
  || problems+=("the port did not report exactly the two unparseable annotations")
[ "$(count_matching "$WORK/parsefail.rb.fails" '^FAIL  ')" = "2" ] \
  || problems+=("the gem did not report exactly the two unparseable annotations")
# Both must carry the prefix on EVERY line: if either stopped classifying these
# as parse failures, its tail file would still be non-empty and the "they
# differ" check would pass for entirely the wrong reason.
[ "$(count_matching "$WORK/parsefail.go.fails" "${PARSE_MARK}")" = "2" ] \
  || problems+=("the port's lines are not all '<location> — could not parse annotation: ...'")
[ "$(count_matching "$WORK/parsefail.rb.fails" "${PARSE_MARK}")" = "2" ] \
  || problems+=("the gem's lines are not all '<location> — could not parse annotation: ...'")
# The shared half: same file, same line, same column — the two normalisers agree
# on exactly which annotation broke and where.
cmp -s "$WORK/parsefail.go.loc" "$WORK/parsefail.rb.loc" \
  || problems+=("the two disagree about WHICH annotations failed, or where — that is not a wording difference")
# The ratified half, asserted from both sides.
cmp -s "$WORK/parsefail.go.tail" "$WORK/parsefail.rb.tail" \
  && problems+=("the two agree byte-for-byte now — RETIRE this ratification and move this file into 4a (header, ratified difference (c))")
grep -q '[^[:space:]]' "$WORK/parsefail.go.tail" || problems+=("the port gave no reason at all")
grep -q '[^[:space:]]' "$WORK/parsefail.rb.tail" || problems+=("the gem gave no reason at all")
# 3 annotations examined, 2 malformed, 0 unread — a parse failure is an
# annotation the tool INSPECTED, not a file it could not open, and both sides
# have to file it under the same clause.
if rb_c="$(ruby_counts "$WORK/parsefail.rb.out")"; then
  [ "$rb_c" = "3 2 0" ] || problems+=("the gem's summary should be 3 annotations, 2 malformed, 0 unread — got [$rb_c]")
else
  problems+=("the gem printed no summary line")
fi
[ "$(go_counts "$WORK/parsefail.go.out")" = "3 2 0" ] \
  || problems+=("the port's counts should be 3 annotations, 2 malformed, 0 unread — got [$(go_counts "$WORK/parsefail.go.out")]")
cmp -s /dev/null "$WORK/parsefail.go.err" || problems+=("the port wrote to stderr; findings belong on stdout")
cmp -s /dev/null "$WORK/parsefail.rb.err" || problems+=("the gem wrote to stderr; findings belong on stdout")

if [ ${#problems[@]} -eq 0 ]; then
  annotations_compared=$((annotations_compared + 3))
  pass_case "same annotations, same locations, same counts, exit 1 — reason text ratified as different"
  dim "        port: $(head -n 1 "$WORK/parsefail.go.tail")"
  dim "        gem:  $(head -n 1 "$WORK/parsefail.rb.tail")"
else
  fail_case "ratified difference (c) no longer holds as written" "${problems[@]}"
fi

# --------------------------------------------------------------------------- #
# 4c. ratified difference (d): the JSON ACCEPTANCE SET
# --------------------------------------------------------------------------- #
#
# See "The four ratified message differences" in the header — and note that (d)
# is the one whose name is wrong in that phrase, because it is not a message
# difference at all.
#
# 4b is about how the two spell the SAME refusal. This is about the payloads
# where only one of them refuses. CPython's `json` grammar — which the port
# reproduces on purpose — is strictly more permissive than Ruby's, so there is a
# set of payloads `json.loads` accepts and `JSON.parse` does not, and every
# member of it diverges. That set is the GENERATOR behind (c): (c) is what you
# see when you look at the output, (d) is what is actually happening.
#
# It was derived rather than guessed, which matters because guessing is what
# produced (c)'s too-narrow framing: 89,108 documents — pyjson_fuzz_test.go's
# own seed corpus and token alphabet, a generated well-formed corpus, and every
# one of the 65,536 single \uXXXX escapes — through both parsers. Widening the
# sweep did not add a member. There are exactly three:
#
#   1. NaN / Infinity / -Infinity. pyjson.go's "WHAT IT BUYS BEYOND THE PROSE"
#      is explicit that accepting these was the point: an earlier Go decoder
#      rejected them, "which made Go classify such a document as a parse failure
#      where Python reported a schema violation", and retiring that exclusion is
#      why the file exists. The very divergence it closed between Go and Python
#      is the one that reopens here between Ruby and Go.
#   2. a HIGH surrogate escape (\ud800-\udbff) not followed by a LOW one. A lone
#      LOW surrogate is accepted by both, so the rule is narrower than
#      "surrogate escapes differ" and the boundary is pinned below.
#   3. nesting past Ruby's max_nesting: 100 default.
#
# Two shapes, and the second is why this could not be folded into 4b:
#
#   (i)  the payload is NOT schema-valid. The gem never gets a value, so it
#        reports a one-line parse failure; the port parses it and reports schema
#        violations with `-> ` reasons. Same file, same line, same counts, same
#        exit code — but a different CLASSIFICATION and a different block, so
#        there is no shared prefix to split on and nothing 4b's tail comparison
#        could have measured.
#   (ii) the payload IS schema-valid. The gem reports it malformed and exits 1;
#        the port passes it and exits 0. Only the file agrees.
echo
echo "== 4c. ratified difference (d): payloads only one parser accepts =="

# 101 brackets each way — one past Ruby's limit once the payload object itself
# is counted. Built rather than typed so the count is checkable.
NEST_OPEN="$(printf '[%.0s' $(seq 101))"
NEST_CLOSE="$(printf ']%.0s' $(seq 101))"

# --- (i) not schema-valid: the classification moves ------------------------- #
#
# All three members are here, because the point of enumerating a set is to
# sample all of it rather than the one member somebody happened to find. The
# first annotation is valid on purpose.
ACCEPT_KIND="$SRC/acceptance_set_kind_spec.rb"
{
  printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n'
  printf '# @intent: { "entity": "Order", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit", "x": NaN }\n'
  printf '# @intent: { "entity": "Order", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit", "x": Infinity }\n'
  printf '# @intent: { "entity": "Order", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit", "x": -Infinity }\n'
  printf '# @intent: { "entity": NaN, "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit" }\n'
  printf '# @intent: { "a": "\\ud800" }\n'
  printf '# @intent: { "a": "\\ud800\\ud800" }\n'
  printf '# @intent: { "entity": "Order", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit", "x": %s%s }\n' \
    "$NEST_OPEN" "$NEST_CLOSE"
} > "$ACCEPT_KIND"

run_go   "acceptkind" "$ACCEPT_KIND"
run_ruby "acceptkind" "$ACCEPT_KIND"

grep '^FAIL  ' "$WORK/acceptkind.go.out" > "$WORK/acceptkind.go.fails" || true
grep '^FAIL  ' "$WORK/acceptkind.rb.out" > "$WORK/acceptkind.rb.fails" || true
# The LOCATION half — everything before the em dash, which for the port is the
# whole line. This is the half that must agree, and comparing it is what makes
# "they still agree about which annotation is broken" an assertion rather than a
# hope.
sed "s/${PARSE_MARK}.*\$//" "$WORK/acceptkind.rb.fails" > "$WORK/acceptkind.rb.loc"
cp "$WORK/acceptkind.go.fails" "$WORK/acceptkind.go.loc"

problems=()
[ "$(cat "$WORK/acceptkind.go.rc")" = "1" ] || problems+=("the port did not exit 1 (got $(cat "$WORK/acceptkind.go.rc"))")
[ "$(cat "$WORK/acceptkind.rb.rc")" = "1" ] || problems+=("the gem did not exit 1 (got $(cat "$WORK/acceptkind.rb.rc"))")
# Non-vacuity: seven failures means every member of the set reached the report.
# Fewer means the corpus stopped generating the divergence and every assertion
# below would be measuring an empty list.
[ "$(count_matching "$WORK/acceptkind.go.fails" '^FAIL  ')" = "7" ] \
  || problems+=("the port did not report exactly the seven payloads — got $(count_matching "$WORK/acceptkind.go.fails" '^FAIL  ')")
[ "$(count_matching "$WORK/acceptkind.rb.fails" '^FAIL  ')" = "7" ] \
  || problems+=("the gem did not report exactly the seven payloads — got $(count_matching "$WORK/acceptkind.rb.fails" '^FAIL  ')")
# The shared half: same file, same lines, in the same order.
cmp -s "$WORK/acceptkind.go.loc" "$WORK/acceptkind.rb.loc" \
  || problems+=("the two disagree about WHICH annotations failed, or where — that is more than a classification difference")
# The half that is NOT shared, asserted from both sides. Every gem line is a
# parse failure; not one port line is, and the port's reasons are indented
# `-> ` lines the gem never emits here.
[ "$(count_matching "$WORK/acceptkind.rb.fails" "${PARSE_MARK}")" = "7" ] \
  || problems+=("the gem stopped classifying all seven as parse failures")
[ "$(count_matching "$WORK/acceptkind.go.fails" "${PARSE_MARK}")" = "0" ] \
  || problems+=("the port now reports a parse failure here — RETIRE ratified difference (d)(i): the acceptance sets have converged")
[ "$(count_matching "$WORK/acceptkind.go.out" '^        -> ')" = "15" ] \
  || problems+=("the port's schema reasons changed shape — expected 15 '-> ' lines, got $(count_matching "$WORK/acceptkind.go.out" '^        -> ')")
[ "$(count_matching "$WORK/acceptkind.rb.out" '^        -> ')" = "0" ] \
  || problems+=("the gem emitted schema reasons — RETIRE ratified difference (d)(i)")
# The counts still agree, and that is the point of (i) as opposed to (ii): a
# reader sees the same verdict and the same tally, and only the explanation
# moves.
if ak_rb_c="$(ruby_counts "$WORK/acceptkind.rb.out")"; then
  [ "$ak_rb_c" = "8 7 0" ] || problems+=("the gem's summary should be [8 7 0] — got [$ak_rb_c]")
else
  problems+=("the gem printed no summary line")
fi
[ "$(go_counts "$WORK/acceptkind.go.out")" = "8 7 0" ] \
  || problems+=("the port's counts should be [8 7 0] — got [$(go_counts "$WORK/acceptkind.go.out")]")
cmp -s /dev/null "$WORK/acceptkind.go.err" || problems+=("the port wrote to stderr; findings belong on stdout")
cmp -s /dev/null "$WORK/acceptkind.rb.err" || problems+=("the gem wrote to stderr; findings belong on stdout")

if [ ${#problems[@]} -eq 0 ]; then
  annotations_compared=$((annotations_compared + 8))
  pass_case "(d)(i) same annotations, same lines, same counts, exit 1 — classification ratified as different"
  dim "        gem:  $(head -n 1 "$WORK/acceptkind.rb.fails")"
  dim "        port: $(head -n 1 "$WORK/acceptkind.go.fails")"
  dim "              $(grep -m 1 '^        -> ' "$WORK/acceptkind.go.out")"
else
  fail_case "ratified difference (d)(i) no longer holds as written" "${problems[@]}"
fi

# --- (ii) schema-valid: the VERDICT moves ----------------------------------- #
#
# The strongest divergence in this file: the two tools disagree about whether the
# suite passes. Reachable through member 2 alone, and provably rather than
# observably — a schema-valid payload has to put the offending syntax inside a
# value the schema permits, and every value schemas/open-test-intent.v1.json
# permits is a string or an array of strings. A number and a container cannot
# occupy a string slot; a surrogate escape lives inside one.
#
# The last two annotations are the BOUNDARY and they are load-bearing: a lone
# LOW surrogate and a well-formed pair are accepted by both parsers, so they must
# pass on BOTH sides. Without them this case would be evidence for a rule far
# wider than the one that is true.
ACCEPT_VERDICT="$SRC/acceptance_set_verdict_spec.rb"
{
  printf '# @intent: { "entity": "\\ud800Or", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit" }\n'
  printf '# @intent: { "entity": "\\ud800\\ud800Or", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit" }\n'
  printf '# @intent: { "entity": "\\udc00Or", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit" }\n'
  printf '# @intent: { "entity": "\\ud83d\\ude00Or", "action": "create", "behavior": "creates an order from a valid cart", "layer": "unit" }\n'
} > "$ACCEPT_VERDICT"

run_go   "acceptverdict" "$ACCEPT_VERDICT"
run_ruby "acceptverdict" "$ACCEPT_VERDICT"

problems=()
# The finding itself. Not "the messages differ" — the verdicts do.
[ "$(cat "$WORK/acceptverdict.go.rc")" = "0" ] \
  || problems+=("the port did not exit 0 (got $(cat "$WORK/acceptverdict.go.rc")) — RETIRE ratified difference (d)(ii) if it now rejects these")
[ "$(cat "$WORK/acceptverdict.rb.rc")" = "1" ] \
  || problems+=("the gem did not exit 1 (got $(cat "$WORK/acceptverdict.rb.rc")) — RETIRE ratified difference (d)(ii) if it now accepts these")
[ "$(count_matching "$WORK/acceptverdict.go.out" '^FAIL  ')" = "0" ] \
  || problems+=("the port reported a failure — RETIRE ratified difference (d)(ii)")
[ "$(count_matching "$WORK/acceptverdict.rb.out" '^FAIL  ')" = "2" ] \
  || problems+=("the gem did not report exactly the two high-surrogate payloads — got $(count_matching "$WORK/acceptverdict.rb.out" '^FAIL  ')")
# The BOUNDARY, from both sides. All four annotations are seen by both tools;
# the port passes all four; the gem fails only the two whose high surrogate has
# no low surrogate after it. A gem that failed all four would mean the rule is
# "surrogate escapes", and this is where that would be caught.
[ "$(count_matching "$WORK/acceptverdict.go.out" '^PASS  ')" = "4" ] \
  || problems+=("the port did not pass all four — the boundary annotations are supposed to be accepted by both parsers")
grep -q "^FAIL  $ACCEPT_VERDICT:1 " "$WORK/acceptverdict.rb.out" \
  || problems+=("the gem did not fail the lone high surrogate on line 1")
grep -q "^FAIL  $ACCEPT_VERDICT:2 " "$WORK/acceptverdict.rb.out" \
  || problems+=("the gem did not fail the bad surrogate pair on line 2")
grep -q "^FAIL  $ACCEPT_VERDICT:3 " "$WORK/acceptverdict.rb.out" \
  && problems+=("the gem failed the lone LOW surrogate on line 3 — Ruby accepts it, so the enumerated rule is wrong")
grep -q "^FAIL  $ACCEPT_VERDICT:4 " "$WORK/acceptverdict.rb.out" \
  && problems+=("the gem failed the well-formed surrogate pair on line 4 — the enumerated rule is wrong")
# What still agrees: both saw the same file and found the same four annotations
# in it. The counts are asserted as DIFFERENT, which no other case here does.
if av_rb_c="$(ruby_counts "$WORK/acceptverdict.rb.out")"; then
  [ "$av_rb_c" = "4 2 0" ] || problems+=("the gem's summary should be [4 2 0] — got [$av_rb_c]")
else
  problems+=("the gem printed no summary line")
fi
[ "$(go_counts "$WORK/acceptverdict.go.out")" = "4 0 0" ] \
  || problems+=("the port's counts should be [4 0 0] — got [$(go_counts "$WORK/acceptverdict.go.out")]")
cmp -s /dev/null "$WORK/acceptverdict.go.err" || problems+=("the port wrote to stderr")
cmp -s /dev/null "$WORK/acceptverdict.rb.err" || problems+=("the gem wrote to stderr; findings belong on stdout")

if [ ${#problems[@]} -eq 0 ]; then
  annotations_compared=$((annotations_compared + 4))
  pass_case "(d)(ii) a schema-valid payload only CPython parses: the port exits 0, the gem exits 1 — ratified"
  dim "        gem:  $(grep -m1 '^FAIL  ' "$WORK/acceptverdict.rb.out")"
  dim "        port: passes all four, exit 0"
else
  fail_case "ratified difference (d)(ii) no longer holds as written" "${problems[@]}"
fi

# --------------------------------------------------------------------------- #
# 5. ratified difference (a): a spec file that is not valid UTF-8
# --------------------------------------------------------------------------- #
#
# See "The four ratified message differences" in the header. Verdict, file,
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
# See "The four ratified message differences" in the header. This one
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
# 8. the gem's two backends — the roadmap's final clause
# --------------------------------------------------------------------------- #
#
# See "The THIRD leg: the gem's own Go backend" in the header. Everything above
# compares two implementations and normalises the four report-FORMAT rules out
# of the way (the PASS/---- lines, the selection and summary lines — not to be
# confused with the four ratified MESSAGE differences, which are a different
# list); this compares ONE implementation against itself with its validator
# swapped, so there is nothing to normalise and the claim is exact bytes.
#
# The participants were proven live in the preflight: a gem that ignores
# SPECGUARD_VALIDATE_INTENT exits 2 there and never reaches this section, which
# is the only thing standing between "the backends agree" and "the Ruby path was
# run twice".
echo
echo "== 8a. the gem's Go backend == the gem's Ruby path, byte for byte =="

# Two paths whose names are GLOB METACHARACTERS. The gem's arguments are paths;
# the port's are patterns; the backend escapes every one before handing it over
# (SpecGuard::RSpec::ValidatorBackend.escape_glob, the port of Python's
# glob.escape). Without that escaping the first name is read as a character
# class and matches nothing, and the second is read as a wildcard that can match
# OTHER files — a linter checking something nobody asked about, or nothing at
# all, in both cases reporting itself calmly. Neither failure is visible without
# a file named like this in the corpus.
#
# They are deliberately NOT in the Go-vs-Ruby sections above: there the
# path-vs-pattern difference is the ratified one, and comparing them would be
# asserting the disagreement this file already documents.
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n' \
  > "$SRC/bracket[1]_spec.rb"
printf '# @intent: { entity: "Order", action: "create", behavior: "creates an order from a valid cart", layer: "unit" }\n# @intent: { entity: "Order" }\n' \
  > "$SRC/star*_spec.rb"
printf '# @intent: { entity: "Cart", action: "empty", behavior: "empties the cart when the order is placed", layer: "unit" }\n' \
  > "$SRC/q?_spec.rb"

# Every hazard file section 4a built, plus the three above, plus anything a
# later edit adds to $SRC — enumerated with `find` rather than named, for the
# same reason section 2's corpus is: a list that has to be maintained by hand
# goes stale silently, and a file it forgets is uncovered without ever being
# reported as uncovered.
#
# FOUR exclusions, all by VARIABLE rather than by name, and all because they are
# section 8b's subject — an enumerated difference, not an agreement: $BAD_UTF8
# (difference (i)), $PARSE_FAIL (difference (iv)), $ACCEPT_KIND (difference (v))
# and $ACCEPT_VERDICT (difference (vi)). Dropping a file from this loop is the
# one edit here that can hide a regression, so each exclusion must have a
# matching entry in 8b that asserts the difference from both sides; an exclusion
# with no entry is how the parse divergence went unnoticed in the first place.
#
# $MISSING was deleted and `find -type f` skips it for free; $UNREADABLE had its
# mode restored at the end of section 7 and belongs here.
backend_cases=()
while IFS= read -r -d '' hazard; do
  [ "$hazard" = "$BAD_UTF8" ] && continue
  [ "$hazard" = "$PARSE_FAIL" ] && continue
  [ "$hazard" = "$ACCEPT_KIND" ] && continue
  [ "$hazard" = "$ACCEPT_VERDICT" ] && continue
  backend_cases+=("$hazard")
done < <(find "$SRC" -type f -print0 | sort -z)

if [ ${#backend_cases[@]} -eq 0 ]; then
  fail_case "no hazard files to compare the backends over" \
    "\$SRC is empty, which means sections 4-7 stopped building their fixtures"
else
  for hazard in "${backend_cases[@]}"; do
    compare_backends "${hazard#"$SRC"/}" "$hazard"
  done
fi

# The shipped corpus, one file at a time and then all at once — the same inputs
# section 2 and section 3 put through the port, now put through the gem twice.
for rel in "${corpus[@]}"; do
  compare_backends "$rel" "$rel"
done
compare_backends "the whole corpus in one invocation" "${corpus[@]}"
compare_backends "valid then invalid" \
  examples/sources/order_spec.rb examples/sources/invalid/broken_intent_spec.rb
compare_backends "invalid then valid" \
  examples/sources/invalid/broken_intent_spec.rb examples/sources/order_spec.rb
compare_backends "the same file twice (not de-duplicated)" \
  examples/sources/invalid/broken_intent_spec.rb examples/sources/invalid/broken_intent_spec.rb
# All three metacharacter names in one invocation, so the escaping is exercised
# where one bad pattern could swallow its neighbours.
compare_backends "three glob-metacharacter names at once" \
  "$SRC/bracket[1]_spec.rb" "$SRC/star*_spec.rb" "$SRC/q?_spec.rb"

# --------------------------------------------------------------------------- #
echo
echo "== 8b. the six enumerated backend differences =="
#
# Three are read-failure PROSE. (iv) is an annotation-level parse message. (v)
# and (vi) are not messages at all — they are the two shapes of the JSON
# acceptance-set difference (ratified difference (d)), where the two backends
# classify the same payload differently or return different verdicts on it.
# Each asserts its own shared half and asserts that the difference still holds,
# so closing one fails this file and retires the entry rather than leaving it to
# rot.
#
# Every file excluded from 8a's loop must appear here. That is the invariant
# that was violated before (iv) existed — not by an exclusion without an entry,
# but by the inverse: a divergence that was in NO corpus at all, so nothing was
# excluded and nothing was enumerated, and the silence read as agreement. (v)
# and (vi) were found the same way a second time, by sweeping the generator
# instead of adding a fixture, which is why they arrive as a pair rather than
# one at a time.

# backend_read_difference <label> <path> <expected-backend-tail-or-empty>
#
# Runs the gem both ways over one unreadable path and requires: exit 1 on both,
# exactly one FAIL line on both, the same `FAIL <path> — could not read file: `
# prefix on both, a 0-annotations/0-malformed/1-unread summary on both, empty
# stderr on both — and DIFFERENT tails.
backend_read_difference() {
  local label="$1" path="$2" expected_tail="$3"
  local name="backendread$((passed + failed))"
  # Published so a caller can reach the files this case produced without
  # re-deriving the counter arithmetic, which changes the moment a pass_case or
  # fail_case is added anywhere above.
  LAST_BACKEND_READ_NAME="$name"

  run_ruby    "$name" "$path"
  run_ruby_go "$name" "$path"

  local rb_rc go_rc rb_line go_line prefix rb_tail go_tail rb_c go_c
  rb_rc="$(cat "$WORK/$name.rb.rc")"
  go_rc="$(cat "$WORK/$name.rbgo.rc")"
  rb_line="$(grep '^FAIL  ' "$WORK/$name.rb.out" | head -n 1)"
  go_line="$(grep '^FAIL  ' "$WORK/$name.rbgo.out" | head -n 1)"
  prefix="FAIL  $path — could not read file: "
  rb_tail="${rb_line#"$prefix"}"
  go_tail="${go_line#"$prefix"}"

  local problems=()
  [ "$rb_rc" = "1" ] || problems+=("the Ruby path did not exit 1 (got $rb_rc)")
  [ "$go_rc" = "1" ] || problems+=("the Go backend did not exit 1 (got $go_rc) — a broken backend must be 2, an unreadable file 1")
  [ "$rb_tail" != "$rb_line" ] || problems+=("the Ruby path's line is not '$prefix...': $rb_line")
  [ "$go_tail" != "$go_line" ] || problems+=("the Go backend's line is not '$prefix...': $go_line")
  [ -n "$rb_tail" ] || problems+=("the Ruby path gave no reason at all")
  [ -n "$go_tail" ] || problems+=("the Go backend gave no reason at all")
  [ "$rb_line" != "$go_line" ] \
    || problems+=("the two agree byte-for-byte now — RETIRE this entry and use compare_backends instead (header, THIRD leg)")
  [ "$(count_matching "$WORK/$name.rb.out" '^FAIL  ')" = "1" ] \
    || problems+=("the Ruby path reported more than the one read failure")
  [ "$(count_matching "$WORK/$name.rbgo.out" '^FAIL  ')" = "1" ] \
    || problems+=("the Go backend reported more than the one read failure")
  cmp -s /dev/null "$WORK/$name.rb.err" || problems+=("the Ruby path wrote to stderr; findings belong on stdout")
  cmp -s /dev/null "$WORK/$name.rbgo.err" || problems+=("the Go backend wrote to stderr; findings belong on stdout")
  # The counts are where "same classification" stops being a claim about prose.
  # An unread file contributes no annotation on either side (CLI#summary_line),
  # and the backend's `read`/`no-match` findings have to land in the same clause.
  if rb_c="$(ruby_counts "$WORK/$name.rb.out")"; then
    [ "$rb_c" = "0 0 1" ] || problems+=("the Ruby path's summary should be [0 0 1] — got [$rb_c]")
  else
    problems+=("the Ruby path printed no summary line")
  fi
  if go_c="$(ruby_counts "$WORK/$name.rbgo.out")"; then
    [ "$go_c" = "0 0 1" ] || problems+=("the Go backend's summary should be [0 0 1] — got [$go_c]")
  else
    problems+=("the Go backend printed no summary line")
  fi
  # An expected tail is given where the gem OWNS the wording (the no-match
  # family) and omitted where it passes the port's text straight through.
  if [ -n "$expected_tail" ] && [ "$go_tail" != "$expected_tail" ]; then
    problems+=("the Go backend's wording changed: expected '$expected_tail', got '$go_tail'")
  fi

  if [ ${#problems[@]} -eq 0 ]; then
    pass_case "$label"
    dim "        ruby path:  $rb_tail"
    dim "        go backend: $go_tail"
  else
    fail_case "$label — the enumerated difference no longer holds as written" "${problems[@]}"
  fi
}

# (i) not valid UTF-8. The gem's Ruby path emits `Scanner.scan_text`'s fixed
# string; with the backend on it emits the PORT's CPython-shaped decoder text
# verbatim, which is why no expected tail is pinned here — the text is the
# port's to change, and ratified difference (a) in section 5 is where it is
# held to account.
backend_read_difference "(i) non-UTF-8 input: the backend emits the port's decoder text" "$BAD_UTF8" ""

# The same file, seen from the other side: with the backend on, the gem's line
# must be exactly the port's line for it. This is what makes (i) a PASS-THROUGH
# rather than a third independent wording.
utf8_go_line="$(grep '^FAIL  ' "$WORK/utf8.go.out" | head -n 1)"
utf8_backend_line="$(grep '^FAIL  ' "$WORK/$LAST_BACKEND_READ_NAME.rbgo.out" 2>/dev/null | head -n 1)"
if [ -n "$utf8_go_line" ] && [ "$utf8_go_line" = "$utf8_backend_line" ]; then
  pass_case "the backend passes the port's read-failure text through unaltered"
else
  fail_case "the backend did not reproduce the port's read-failure line" \
    "port:    $utf8_go_line" "backend: $utf8_backend_line"
fi

# (ii) a path that does not exist. Section 6 ratified this between the PORT and
# the gem as a difference in kind (stderr/no-match vs stdout/read failure).
# Between the gem's two BACKENDS it is neither — both put a read failure on
# stdout under the same prefix. Only the tail moves, because the port answered
# `no-match` and never told the gem an errno.
rm -f "$MISSING"
backend_read_difference "(ii) a path that does not exist maps no-match onto a read failure" \
  "$MISSING" "no file at this path"

# (iii) a path that is not a regular file. The port's ExpandFiles drops
# directories before anything opens them, so this is `no-match` too; the Ruby
# path gets EISDIR from File.read. Same fold, different errno — worth its own
# case because a reader who only saw (ii) would reasonably assume no-match means
# "absent".
NOT_A_FILE="$SRC/a_directory_spec.rb"
rm -rf "$NOT_A_FILE"
mkdir -p "$NOT_A_FILE"
backend_read_difference "(iii) a directory named like a spec file maps no-match onto a read failure" \
  "$NOT_A_FILE" "no file at this path"
rmdir "$NOT_A_FILE"

# (iv) an @intent payload that survives normalisation and still is not JSON —
# ratified difference (c), now seen from inside the gem. Not a read failure:
# this is the parse message, the one that fires on an ordinary typo, and the
# reason this entry exists is that its absence let both READMEs and
# `ValidatorBackend`'s own comment claim the backends differed only in
# read-failure prose.
#
# It gets its own block rather than reusing backend_read_difference because
# nothing about it is a read failure: the finding is line-scoped, it counts as
# an annotation rather than an unread file, and there are two of them.
echo
run_ruby    "backendparse" "$PARSE_FAIL"
run_ruby_go "backendparse" "$PARSE_FAIL"

bp_rb_rc="$(cat "$WORK/backendparse.rb.rc")"
bp_go_rc="$(cat "$WORK/backendparse.rbgo.rc")"
grep '^FAIL  ' "$WORK/backendparse.rb.out"   > "$WORK/backendparse.rb.fails"   || true
grep '^FAIL  ' "$WORK/backendparse.rbgo.out" > "$WORK/backendparse.rbgo.fails" || true
sed "s/${PARSE_MARK}.*\$//" "$WORK/backendparse.rb.fails"   > "$WORK/backendparse.rb.loc"
sed "s/${PARSE_MARK}.*\$//" "$WORK/backendparse.rbgo.fails" > "$WORK/backendparse.rbgo.loc"
sed "s/^.*${PARSE_MARK}//"  "$WORK/backendparse.rb.fails"   > "$WORK/backendparse.rb.tail"
sed "s/^.*${PARSE_MARK}//"  "$WORK/backendparse.rbgo.fails" > "$WORK/backendparse.rbgo.tail"
# Everything that is NOT the ratified tail — the leading selection line, the
# locations, the trailing summary — must be identical. Blanking the tail on both
# sides and comparing whole files is what makes "only the tail moves" an
# assertion about the WHOLE report rather than about the lines this block
# happened to look at.
sed "s/${PARSE_MARK}.*\$/${PARSE_MARK}<ratified>/" "$WORK/backendparse.rb.out"   > "$WORK/backendparse.rb.masked"
sed "s/${PARSE_MARK}.*\$/${PARSE_MARK}<ratified>/" "$WORK/backendparse.rbgo.out" > "$WORK/backendparse.rbgo.masked"

problems=()
[ "$bp_rb_rc" = "1" ] || problems+=("the Ruby path did not exit 1 (got $bp_rb_rc)")
[ "$bp_go_rc" = "1" ] || problems+=("the Go backend did not exit 1 (got $bp_go_rc) — a broken backend must be 2, a malformed annotation 1")
[ "$(count_matching "$WORK/backendparse.rb.fails" "${PARSE_MARK}")" = "2" ] \
  || problems+=("the Ruby path did not report exactly two parse failures")
[ "$(count_matching "$WORK/backendparse.rbgo.fails" "${PARSE_MARK}")" = "2" ] \
  || problems+=("the Go backend did not report exactly two parse failures")
cmp -s "$WORK/backendparse.rb.loc" "$WORK/backendparse.rbgo.loc" \
  || problems+=("the two backends disagree about WHICH annotations failed, or where")
cmp -s "$WORK/backendparse.rb.masked" "$WORK/backendparse.rbgo.masked" \
  || problems+=("the reports differ somewhere other than the ratified tail")
cmp -s "$WORK/backendparse.rb.tail" "$WORK/backendparse.rbgo.tail" \
  && problems+=("the two agree byte-for-byte now — RETIRE this entry and let 8a's loop pick \$PARSE_FAIL up (header, THIRD leg)")
grep -q '[^[:space:]]' "$WORK/backendparse.rb.tail"   || problems+=("the Ruby path gave no reason at all")
grep -q '[^[:space:]]' "$WORK/backendparse.rbgo.tail" || problems+=("the Go backend gave no reason at all")
# A parse failure is an annotation examined, not a file unread — identical
# counts on both, and specifically NOT in the "could not be read" clause.
if bp_rb_c="$(ruby_counts "$WORK/backendparse.rb.out")"; then
  [ "$bp_rb_c" = "3 2 0" ] || problems+=("the Ruby path's summary should be [3 2 0] — got [$bp_rb_c]")
else
  problems+=("the Ruby path printed no summary line")
fi
if bp_go_c="$(ruby_counts "$WORK/backendparse.rbgo.out")"; then
  [ "$bp_go_c" = "3 2 0" ] || problems+=("the Go backend's summary should be [3 2 0] — got [$bp_go_c]")
else
  problems+=("the Go backend printed no summary line")
fi
cmp -s "$WORK/backendparse.rb.err" "$WORK/backendparse.rbgo.err" || problems+=("stderr differs")
cmp -s /dev/null "$WORK/backendparse.rbgo.err" || problems+=("the Go backend wrote to stderr; findings belong on stdout")

if [ ${#problems[@]} -eq 0 ]; then
  backend_annotations_compared=$((backend_annotations_compared + 3))
  pass_case "(iv) a payload that is still not JSON: same annotations and counts, parse text ratified as different"
  dim "        ruby path:  $(head -n 1 "$WORK/backendparse.rb.tail")"
  dim "        go backend: $(head -n 1 "$WORK/backendparse.rbgo.tail")"
else
  fail_case "(iv) the enumerated parse difference no longer holds as written" "${problems[@]}"
fi

# The same file, seen from the other side — the counterpart of the pass-through
# check (i) carries. The backend's parse lines must be EXACTLY the port's, which
# is what makes (iv) a pass-through of a text the port owns rather than a third
# independent spelling this gem would then have to maintain. Section 4b already
# ran the port over this very file.
if cmp -s "$WORK/parsefail.go.fails" "$WORK/backendparse.rbgo.fails"; then
  pass_case "the backend passes the port's parse-failure text through unaltered"
else
  fail_case "the backend did not reproduce the port's parse-failure lines" \
    "$(diff -u "$WORK/parsefail.go.fails" "$WORK/backendparse.rbgo.fails" | tail -n +3 | sed 's/^/          /')"
fi

# (v) and (vi) — the JSON acceptance-set difference, ratified difference (d),
# now seen from inside the gem. Section 4c established it between the PORT and
# the gem's Ruby path; these two entries establish that flipping
# SPECGUARD_VALIDATE_INTENT reproduces it exactly, which is what makes it a
# property of the backend a user can turn on rather than a curiosity about two
# separate programs.
#
# Neither is a wording difference, so neither can use backend_read_difference or
# (iv)'s tail-masking: in (v) the two reports have no shared prefix to split on,
# and in (vi) one of them is empty.
echo
run_ruby    "backendacceptkind" "$ACCEPT_KIND"
run_ruby_go "backendacceptkind" "$ACCEPT_KIND"

grep '^FAIL  ' "$WORK/backendacceptkind.rb.out"   > "$WORK/backendacceptkind.rb.fails"   || true
grep '^FAIL  ' "$WORK/backendacceptkind.rbgo.out" > "$WORK/backendacceptkind.rbgo.fails" || true
sed "s/${PARSE_MARK}.*\$//" "$WORK/backendacceptkind.rb.fails" > "$WORK/backendacceptkind.rb.loc"
cp "$WORK/backendacceptkind.rbgo.fails" "$WORK/backendacceptkind.rbgo.loc"

problems=()
[ "$(cat "$WORK/backendacceptkind.rb.rc")" = "1" ] \
  || problems+=("the Ruby path did not exit 1 (got $(cat "$WORK/backendacceptkind.rb.rc"))")
[ "$(cat "$WORK/backendacceptkind.rbgo.rc")" = "1" ] \
  || problems+=("the Go backend did not exit 1 (got $(cat "$WORK/backendacceptkind.rbgo.rc")) — a broken backend must be 2, a malformed annotation 1")
[ "$(count_matching "$WORK/backendacceptkind.rb.fails" '^FAIL  ')" = "7" ] \
  || problems+=("the Ruby path did not report all seven members of the set")
[ "$(count_matching "$WORK/backendacceptkind.rbgo.fails" '^FAIL  ')" = "7" ] \
  || problems+=("the Go backend did not report all seven members of the set")
# The shared half: same annotations, same lines, same order.
cmp -s "$WORK/backendacceptkind.rb.loc" "$WORK/backendacceptkind.rbgo.loc" \
  || problems+=("the two backends disagree about WHICH annotations failed, or where")
# The half that is not shared, from both sides.
[ "$(count_matching "$WORK/backendacceptkind.rb.fails" "${PARSE_MARK}")" = "7" ] \
  || problems+=("the Ruby path stopped classifying all seven as parse failures")
[ "$(count_matching "$WORK/backendacceptkind.rbgo.fails" "${PARSE_MARK}")" = "0" ] \
  || problems+=("the Go backend now reports a parse failure — RETIRE entry (v)")
[ "$(count_matching "$WORK/backendacceptkind.rbgo.out" '^        -> ')" = "15" ] \
  || problems+=("the Go backend's schema reasons changed shape — expected 15 '-> ' lines")
[ "$(count_matching "$WORK/backendacceptkind.rb.out" '^        -> ')" = "0" ] \
  || problems+=("the Ruby path emitted schema reasons — RETIRE entry (v)")
# Identical counts and exit code: (v)'s whole claim is that only the explanation
# moves.
bak_rb_c="$(ruby_counts "$WORK/backendacceptkind.rb.out")"   || bak_rb_c="<none>"
bak_go_c="$(ruby_counts "$WORK/backendacceptkind.rbgo.out")" || bak_go_c="<none>"
[ "$bak_rb_c" = "8 7 0" ] || problems+=("the Ruby path's summary should be [8 7 0] — got [$bak_rb_c]")
[ "$bak_go_c" = "8 7 0" ] || problems+=("the Go backend's summary should be [8 7 0] — got [$bak_go_c]")
cmp -s "$WORK/backendacceptkind.rb.out" "$WORK/backendacceptkind.rbgo.out" \
  && problems+=("the two agree byte-for-byte now — RETIRE entry (v) and let 8a's loop pick \$ACCEPT_KIND up")
cmp -s /dev/null "$WORK/backendacceptkind.rbgo.err" || problems+=("the Go backend wrote to stderr; findings belong on stdout")
cmp -s "$WORK/backendacceptkind.rb.err" "$WORK/backendacceptkind.rbgo.err" || problems+=("stderr differs")
# The pass-through half: the backend's lines must be EXACTLY the port's, so this
# is the port's classification reproduced rather than a third behaviour.
cmp -s "$WORK/acceptkind.go.fails" "$WORK/backendacceptkind.rbgo.fails" \
  || problems+=("the backend did not reproduce the port's own lines for this file")

if [ ${#problems[@]} -eq 0 ]; then
  backend_annotations_compared=$((backend_annotations_compared + 8))
  pass_case "(v) a payload only CPython parses: same annotations and counts, CLASSIFICATION ratified as different"
  dim "        ruby path:  $(head -n 1 "$WORK/backendacceptkind.rb.fails")"
  dim "        go backend: $(head -n 1 "$WORK/backendacceptkind.rbgo.fails")"
  dim "                    $(grep -m 1 '^        -> ' "$WORK/backendacceptkind.rbgo.out")"
else
  fail_case "(v) the enumerated classification difference no longer holds as written" "${problems[@]}"
fi

# (vi) The one entry in this file where the two backends disagree about whether
# the run passed. Everything else here — every ratification, every enumerated
# difference — shares the exit code; this does not, and that is the finding.
echo
run_ruby    "backendacceptverdict" "$ACCEPT_VERDICT"
run_ruby_go "backendacceptverdict" "$ACCEPT_VERDICT"

problems=()
[ "$(cat "$WORK/backendacceptverdict.rb.rc")" = "1" ] \
  || problems+=("the Ruby path did not exit 1 (got $(cat "$WORK/backendacceptverdict.rb.rc")) — RETIRE entry (vi) if it now accepts these")
[ "$(cat "$WORK/backendacceptverdict.rbgo.rc")" = "0" ] \
  || problems+=("the Go backend did not exit 0 (got $(cat "$WORK/backendacceptverdict.rbgo.rc")) — RETIRE entry (vi) if it now rejects these")
[ "$(count_matching "$WORK/backendacceptverdict.rb.out" '^FAIL  ')" = "2" ] \
  || problems+=("the Ruby path did not report exactly the two high-surrogate payloads")
[ "$(count_matching "$WORK/backendacceptverdict.rbgo.out" '^FAIL  ')" = "0" ] \
  || problems+=("the Go backend reported a failure — RETIRE entry (vi)")
# The boundary, from inside the gem this time. Lines 3 and 4 are accepted by
# both parsers and must be clean on BOTH backends; if the Ruby path failed them
# the enumerated rule would be wider than the truth and (vi) would be describing
# the wrong thing.
grep -q "^FAIL  $ACCEPT_VERDICT:3 " "$WORK/backendacceptverdict.rb.out" \
  && problems+=("the Ruby path failed the lone LOW surrogate — the enumerated rule is wrong")
grep -q "^FAIL  $ACCEPT_VERDICT:4 " "$WORK/backendacceptverdict.rb.out" \
  && problems+=("the Ruby path failed the well-formed surrogate pair — the enumerated rule is wrong")
# What IS shared, stated rather than left implied: the selection line and the
# annotation count. Both backends read the same file and found the same four
# annotations; they disagree about how many of them are broken.
bav_rb_c="$(ruby_counts "$WORK/backendacceptverdict.rb.out")"   || bav_rb_c="<none>"
bav_go_c="$(ruby_counts "$WORK/backendacceptverdict.rbgo.out")" || bav_go_c="<none>"
[ "$bav_rb_c" = "4 2 0" ] || problems+=("the Ruby path's summary should be [4 2 0] — got [$bav_rb_c]")
[ "$bav_go_c" = "4 0 0" ] || problems+=("the Go backend's summary should be [4 0 0] — got [$bav_go_c]")
[ "$(head -n 1 "$WORK/backendacceptverdict.rb.out")" = "$(head -n 1 "$WORK/backendacceptverdict.rbgo.out")" ] \
  || problems+=("the two backends disagree about the SELECTION as well, which is not part of this ratification")
cmp -s /dev/null "$WORK/backendacceptverdict.rbgo.err" || problems+=("the Go backend wrote to stderr")
cmp -s "$WORK/backendacceptverdict.rb.err" "$WORK/backendacceptverdict.rbgo.err" || problems+=("stderr differs")

if [ ${#problems[@]} -eq 0 ]; then
  backend_annotations_compared=$((backend_annotations_compared + 4))
  pass_case "(vi) a SCHEMA-VALID payload only CPython parses: the backend exits 0 where the Ruby path exits 1 — ratified"
  dim "        ruby path:  $(grep -m 1 '^FAIL  ' "$WORK/backendacceptverdict.rb.out")"
  dim "        go backend: no findings, exit 0"
else
  fail_case "(vi) the enumerated verdict difference no longer holds as written" "${problems[@]}"
fi

# The case with teeth, and the half that IS shared: an unreadable path alongside
# real ones must not abort either backend, and everything except its own line
# must be identical.
echo
run_ruby    "backendmixed" "$MISSING" examples/sources/order_spec.rb
run_ruby_go "backendmixed" "$MISSING" examples/sources/order_spec.rb

problems=()
mixed_rb_rc="$(cat "$WORK/backendmixed.rb.rc")"
mixed_go_rc="$(cat "$WORK/backendmixed.rbgo.rc")"
mixed_rb_c="$(ruby_counts "$WORK/backendmixed.rb.out")" || mixed_rb_c="<none>"
mixed_go_c="$(ruby_counts "$WORK/backendmixed.rbgo.out")" || mixed_go_c="<none>"
# 7 annotations in order_spec.rb, none malformed, one file unread. IDENTICAL on
# both backends — unlike section 6's port-vs-gem crossing, where the port
# counted 0 unread because the missing file never became a file.
[ "$mixed_rb_c" = "7 0 1" ] || problems+=("the Ruby path checked [$mixed_rb_c], expected [7 0 1]")
[ "$mixed_go_c" = "7 0 1" ] || problems+=("the Go backend checked [$mixed_go_c], expected [7 0 1]")
[ "$mixed_rb_rc" = "1" ] || problems+=("the Ruby path exited $mixed_rb_rc, expected 1")
[ "$mixed_go_rc" = "1" ] || problems+=("the Go backend exited $mixed_go_rc, expected 1")
grep -v "^FAIL  $MISSING — " "$WORK/backendmixed.rb.out"   > "$WORK/backendmixed.rb.norm"
grep -v "^FAIL  $MISSING — " "$WORK/backendmixed.rbgo.out" > "$WORK/backendmixed.rbgo.norm"
cmp -s "$WORK/backendmixed.rb.norm" "$WORK/backendmixed.rbgo.norm" \
  || problems+=("the good file's report differs once the unreadable path's own line is set aside")
cmp -s "$WORK/backendmixed.rb.err" "$WORK/backendmixed.rbgo.err" \
  || problems+=("stderr differs")

if [ ${#problems[@]} -eq 0 ]; then
  backend_annotations_compared=$((backend_annotations_compared + 7))
  pass_case "an unreadable path does not abort the rest of the run on either backend"
else
  fail_case "an unreadable path changed what else got checked" "${problems[@]}"
fi

# --------------------------------------------------------------------------- #
echo
echo "== 8c. the backend refuses rather than guessing =="
#
# Exit 1 means "an annotation is malformed" and the contract spends it on
# nothing else (SpecGuard::RSpec::CLI). A backend that cannot produce a verdict
# — no binary, a binary that will not run, output that is not a report — must
# therefore be 2, and must say `specguard-lint: error:` rather than reaching the
# CLI's `internal error:` backstop, which reads as a bug to file instead of a
# variable to fix.
#
# The gem's own suite covers this exhaustively against stub binaries
# (spec/specguard/rspec/validator_backend_spec.rb). What is here is the part
# that suite structurally cannot do: the same refusals with the REAL binary and
# the real gem entrypoint on the same machine.

# backend_refusal <label> <binary> [args...]
backend_refusal() {
  local label="$1" binary="$2"
  shift 2
  local name="backendrefuse$((passed + failed))"
  local rc

  (cd "$REPO_ROOT" && SPECGUARD_VALIDATE_INTENT="$binary" "$RUBY" -I"$GEM_ROOT/lib" "$GEM_LINT" "$@" \
    >"$WORK/$name.out" 2>"$WORK/$name.err")
  rc=$?

  local problems=()
  [ "$rc" = "2" ] || problems+=("exited $rc, expected 2 — exit 1 is spent on 'an annotation is malformed'")
  grep -q '^specguard-lint: error: ' "$WORK/$name.err" \
    || problems+=("no 'specguard-lint: error: ' line on stderr")
  grep -q 'internal error' "$WORK/$name.err" \
    && problems+=("reported as an internal error, which sends the reader to file a gem bug")
  grep -q '^FAIL  ' "$WORK/$name.out" \
    && problems+=("printed findings from a run that produced no verdict")

  if [ ${#problems[@]} -eq 0 ]; then
    pass_case "$label"
  else
    fail_case "$label" "${problems[@]}" "$(sed 's/^/          /' "$WORK/$name.err" | head -n 3)"
  fi
}

backend_refusal "a binary that does not exist is exit 2" \
  "$WORK/definitely-not-here" examples/sources/order_spec.rb
backend_refusal "a path that is a directory is exit 2" \
  "$WORK" examples/sources/order_spec.rb

printf '#!/bin/sh\necho "this is not a JSON document"\n' > "$WORK/not-a-validator"
chmod +x "$WORK/not-a-validator"
backend_refusal "a binary emitting unparseable output is exit 2" \
  "$WORK/not-a-validator" examples/sources/order_spec.rb

printf '#!/bin/sh\necho "error: could not load schema /nope" >&2\nexit 2\n' > "$WORK/failing-validator"
chmod +x "$WORK/failing-validator"
backend_refusal "a binary exiting 2 is exit 2, not 1" \
  "$WORK/failing-validator" examples/sources/order_spec.rb

# Death by signal has no exit status at all, which is the one shape a naive
# `status.exitstatus == 1` check reads as nil and a naive `!= 0` reads as a
# verdict. It is neither.
printf '#!/bin/sh\nkill -9 $$\n' > "$WORK/suicidal-validator"
chmod +x "$WORK/suicidal-validator"
backend_refusal "a binary killed by a signal is exit 2" \
  "$WORK/suicidal-validator" examples/sources/order_spec.rb

# A binary that answers a `--source --json` request with the TEXT report, and
# with the exit code that report deserves. The port refuses out-of-scope --json
# surfaces itself; a future one that fell through to the wrong renderer would
# hand the gem prose alongside a perfectly correct exit 1 — the quietest failure
# available, because the exit code agrees and only the payload is wrong.
{
  printf '#!/bin/sh\n'
  printf "echo 'FAIL  a_spec.rb:9'\n"
  printf "echo '        -> <root>: missing required property '\\\\''entity'\\\\'''\n"
  printf 'exit 1\n'
} > "$WORK/text-mode-validator"
chmod +x "$WORK/text-mode-validator"
backend_refusal "a binary answering --json with the text report is exit 2" \
  "$WORK/text-mode-validator" examples/sources/invalid/broken_intent_spec.rb

# Non-vacuity for the whole of section 8: two runs that both printed nothing
# compare equal, so a section that compared no annotation compared nothing.
echo
if [ "$backend_annotations_compared" -eq 0 ]; then
  fail_case "section 8 compared zero annotations between the two backends" \
    "every case normalised away to nothing, which is not agreement"
else
  pass_case "section 8 compared $backend_annotations_compared annotations across the two backends"
fi

# --------------------------------------------------------------------------- #
# 9. the participants are unmodified
# --------------------------------------------------------------------------- #
#
# This leg adds only. If parity were "achieved" by editing the corpus or the
# gem's reporter to match, every case above would still be green — so say so
# rather than assume it. The port itself is deliberately not covered here: it
# is rebuilt from cmd/ above when a toolchain exists, and run_parity.sh already
# guards the Python oracle.
echo
echo "== 9. the corpus is unmodified =="

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
dim   "      section 8 compared $backend_annotations_compared annotations across the gem's two backends"
if [ "$skipped" -gt 0 ]; then
  dim "note: skipped cases were not verified — see the SKIP lines above"
fi
exit 0
