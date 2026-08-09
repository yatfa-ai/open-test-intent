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
# documenting `--version` and `--schema-source` (excluded groups 4 and 5
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
# Point cases vs. sweeps
# ----------------------
# Most sections below are hand-picked argument sets. That is enough for surfaces
# with a small number of distinct states, and it is NOT enough for the JSON
# scanner: a real divergence in the \uXXXX escape bound survived 228 hand-picked
# cases because reaching it needs a document truncated at one exact offset.
# Section 17b ("truncation sweep — every prefix of a \uXXXX-bearing document")
# therefore sweeps EVERY prefix of a set of documents rather than choosing
# prefixes a human thought were interesting. Where a bug class is
# defined by an offset or a boundary, prefer a sweep — the point case that
# reproduces today's bug is also the point case that misses tomorrow's.
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
# SEVEN groups of inputs are deliberately NOT compared against Python. Each is
# excluded for a stated reason, and each is still asserted against on the Go
# side (see "Go-side refusals" at the bottom) so the exclusion cannot quietly
# become "untested".
#
# (Seven, and the arithmetic has never gone in one direction, so it is worth
# spelling out: slice 1 listed five; slice 2 retired two of them — see RETIRED
# EXCLUSIONS below — and slice 1's separate "unsupported schema construct"
# group folded into the `pattern` group when that port landed, which left four.
# Slice 5 (SPGD-131) then ADDED a group, when the Go binary gained an embedded
# schema and stopped failing on trees the Python script still cannot run in,
# which made five; slice 4 (SPGD-123) retired the recursive-glob group when `**`
# was implemented, back to four; slice 6 (SPGD-141) ADDED `--version` and slice
# 19 (SPGD-301) ADDED `--schema-source`, which made six. Slice 3 (SPGD-107) —
# this one, which lands late because it is the largest — then retired two more,
# the unported modes and the non-UTF-8 PROSE, leaving four, and added three of
# its own across three review rounds: PYTHONIOENCODING (round 5), which had been
# a refusal asserted in section 16 ("Go-side refusals — the excluded surfaces,
# still asserted") without ever being written down here; the LOCALE group
# (round 6), a variable nothing here had ever set; and stderr's error handler
# (round 8), which is the one group below that is a KNOWN DIVERGENCE rather than
# a refusal. The count is right; it just does not arithmetic down from five in
# one step.)
#
#   1. Schemas carrying a `pattern` Go's RE2 engine cannot reproduce exactly.
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
#   2. A tree with no schemas/ directory beside the binary.
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
#      Asserted on the Go side in section 16d ("excluded group 2 — a tree with
#      no schemas/ directory beside the binary"), which pins what it does for
#      each of the five argument sets that used to be compared here — including
#      the two that still REFUSE. A bare self-test on such a tree now exits 0
#      with "12/12 fixtures matched expectation.", because SPGD-315 embedded the
#      fixture corpus on the same absent/present rule: that is the release-asset
#      case, and it is asserted rather than assumed so nobody later reads the
#      exclusion as a claim that everything works out there — or, since the
#      inversion, as a claim that it always did.
#
#      One caveat worth stating plainly, in the same spirit as the decoder
#      caveat below: this harness proves the embedded schema and corpus BEHAVE
#      like the canonical ones on the inputs they happen to reach. It does NOT
#      prove they are the same bytes. That is pinned by SHA256 in schema_test.go
#      and corpus_test.go at the module root, and THIS HARNESS DOES NOT RUN
#      THEM — run `go test ./...` separately (./... , not ./cmd/... , or the
#      root package's guards are skipped and the pins verify nothing).
#
#   3. A PYTHONIOENCODING this port cannot reproduce — either field of it.
#      The variable is `ENCODING:HANDLER` and it configures sys.stdin, sys.stdout
#      and sys.stderr together. The port implements exactly UTF-8 with the
#      `strict` and `surrogateescape` handlers; anything else it refuses with
#      exit 2 in EVERY mode rather than answering, so these environments cannot
#      be compared.
#
#      Both fields earn their place, and the second one is here because it was
#      MISSING rather than declined. `replace` and `ignore` succeed in both
#      directions and each writes different bytes. `latin-1`, `cp1252`, `ascii`
#      and `utf-16` carry the `strict` handler — which the port DOES reproduce,
#      so the handler gate passes them — but a codec it does not implement:
#      iso8859-1 decodes bytes UTF-8 rejects, so the reference reports `schema`
#      where the port reported `read`, with the same exit code. Until slice 3's
#      round-5 review this harness varied the handler across four spellings and
#      never varied the encoding at all, which is the difference between an
#      excluded axis and an untested one — and an untested axis reads exactly
#      like a passing one.
#
#      NARROW in the direction that matters: the six CPython aliases of utf-8
#      (`utf8`, `utf_8`, `U8`, `utf`, `cp65001`, `utf8_ucs2`/`ucs4`, and any
#      punctuation spelling normalising to them) resolve to the codec the port
#      implements, so they are NOT excluded — they stay byte-for-byte
#      comparisons, in section 15f ("the ENCODING half of PYTHONIOENCODING").
#      An exclusion drawn on the spelling rather than the codec would have
#      quietly dropped eleven live cases.
#
#      `--help` splits the two fields, and the split is measured: it is still
#      COMPARED under an unreproducible handler (the usage block is entirely
#      UTF-8-representable, so no handler can alter it) and REFUSED under an
#      unreproducible encoding (that block carries an em dash, which kills the
#      reference under `latin-1` and re-encodes under `utf-16`).
#
#      That same section 15f ("the ENCODING half of PYTHONIOENCODING") also pins
#      the PREMISE, python3 against python3: the codecs this group excludes must
#      genuinely change the reference's own answer. A refusal whose reason has
#      expired is over-refusal, and nothing else here would notice.
#
#      One acknowledged NON-parity inside this group: an encoding CPython does
#      not know at all (`PYTHONIOENCODING=bogus`) makes the interpreter die in
#      init_stdio_encoding with exit 1 before bin/validate-intent's first line
#      runs, where the port exits 2 naming the encoding. Neither produces a
#      report, so no consumer is handed a wrong answer, and reproducing a
#      CPython startup failure is not a behaviour of this binary.
#
#
#   4. `--version`.
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
#      section 16b ("--version — the excluded surface that SUCCEEDS"),
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
#      That trailer now carries TWO rows — group 5 below is documented in the
#      same constant, for the same reason and under the same comparison.
#
#   5. `--schema-source`.
#      The second Go-only flag (slice 19, SPGD-301,
#      cmd/validate-intent/schemasource.go), excluded for exactly the reason
#      group 4 is: the reference has no such flag and reads it as a filename,
#      reporting "no file(s) match '--schema-source'" with exit 1.
#
#      What it adds is the answer to the hedge group 4's trailer makes. That
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
#      Like group 4 it SUCCEEDS rather than refuses — exit 0, one line on
#      stdout, nothing on stderr — so assert_refusal cannot cover it either. It
#      is asserted by assert_schema_source_in in section 16c ("--schema-source
#      — the second Go-only surface that SUCCEEDS"), which pins the
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
#      The crossing with `--help` is not excluded, for the same reason group 4's
#      is not: the reference's --help loop pre-empts the argument, so both print
#      usage and it is a real comparison in section 7. The crossing with
#      `--version` IS Go-side (Python has neither flag) and is asserted in
#      section 16c: `--version` wins, because three external consumers already
#      parse its output and a new flag must not change what any crossing of the
#      two prints.
#
#
#   6. An environment whose LOCALE gives CPython a default codec that is not
#      UTF-8. The same question as group 3, one level down and one variable
#      earlier: when PYTHONIOENCODING names no codec, CPython takes one from the
#      locale — and uses that SAME codec for sys.getfilesystemencoding(), which
#      decodes sys.argv and every directory listing. `PYTHONUTF8=0 LC_ALL=C`
#      makes all of them `ascii`, with nothing in PYTHONIOENCODING set at all, so
#      group 3's gate cannot see it; measured, the reference then dies on its own
#      `--help` (UnicodeEncodeError on the usage block's em dash, rc 1, 0 bytes)
#      where the port printed 824 clean bytes and exited 0.
#
#      The port refuses any environment it cannot PROVE resolves to UTF-8. That
#      whitelist is knowingly WIDER than CPython's rule, and the reason is worth
#      stating because it bounds what this exclusion covers: CPython's rule is
#      libc's `nl_langinfo(CODESET)` for the locale `setlocale(LC_CTYPE, "")`
#      resolved to, and whether that locale is INSTALLED changes the answer —
#      `PYTHONUTF8=0 LC_ALL=en_US.UTF-8` is `ascii` here precisely because
#      en_US.UTF-8 is not installed. A cgo-free Go binary can ask neither
#      question. So some environments CPython would have answered are refused
#      too: `PYTHONUTF8=0` alongside a genuinely UTF-8 locale, and a locale name
#      carrying no codeset. Those are visible exit 2s naming the variable, which
#      is the direction this gate is allowed to be wrong in.
#
#      Section 17c ("the locale's default codec") pins both halves — the PREMISE
#      python3-against-python3 (the refused environments must genuinely change
#      the reference's answer) and the OVER-REFUSAL direction (the accepted ones
#      are still compared byte-for-byte, `--help` included).
#      cmd/validate-intent/pylocale_test.go runs the same matrix against python3
#      and fails on an UNDER-refusal only.
#
#      One measured subtlety, because it looks like an inconsistency until you
#      know it: PEP 538's C-locale coercion rewrites LC_CTYPE to C.UTF-8 and is
#      SKIPPED when LC_ALL is set. So `LC_ALL=C` resolves to ascii while the same
#      locale named through LC_CTYPE or LANG resolves to utf-8, and the port
#      over-refuses the latter.
#
#   7. The "could not load schema" diagnostic, on a host whose INSTALL
#      directory has a name that is not valid UTF-8. This one is the odd one
#      out twice over: it is neither a refusal nor a Go-only surface. It is a
#      measured DIVERGENCE, declared here rather than fixed, and the honest
#      one-line statement of it is that STDERR'S ERROR HANDLER IS UNMODELED.
#
#      CPython gives sys.stderr the `backslashreplace` handler by default —
#      independently of PYTHONIOENCODING, and DIFFERENTLY from sys.stdout, which
#      is the part that makes it easy to miss:
#
#        $ python3 -c "import sys; print(sys.stdout.errors, sys.stderr.errors)"
#        surrogateescape backslashreplace
#        $ PYTHONIOENCODING=utf-8 python3 -c "...same..."
#        strict backslashreplace
#
#      The port models the STDOUT encoder carefully — pyioerrors.go decides the
#      handler and pystdout.go applies it — and has no stderr counterpart at
#      all. That is harmless for every stderr write but one, because the rest
#      are ASCII by the time they are written: they are compile-time constants
#      that UTF-8 encodes identically on both sides (`usage`), or they go
#      through PyReprString, which escapes every non-ASCII character to
#      `\uXXXX`. The exception is schemaLoadError (cmd/validate-intent/main.go),
#      which interpolates the schema's ORIGIN with a bare `%s`. Measured, with a
#      malformed schema under an install directory named `d\xe9`:
#
#        PY   ... could not load schema /tmp/bt/d\udce9/schemas/...   6 ASCII chars
#        GO   ... could not load schema /tmp/bt/d<ED><B3><A9>/schemas/...   3 raw bytes
#
#      The `[Errno ...]` half of that same line is NOT affected and is not
#      excluded: it goes through pyOSError -> PyReprString and is byte-identical
#      to the reference, which is why the unreadable variant prints both
#      renderings side by side on one line. Section 16e ("stderr's error handler
#      — unmodeled, and pinned where it shows") asserts BOTH halves, so the
#      correct one cannot be lost while the group is open.
#
#      The blast radius is that diagnostic alone: a badly-named install tree
#      carrying a VALID schema is byte-identical in every mode, and 16e pins
#      that too — otherwise "excluded" would be doing more work than it earns.
#
#      Not a regression, and that is worth recording. Before this slice decoded
#      argv and the filesystem (pyfspath.go) the same line diverged differently
#      — one raw 0xE9 byte rather than three WTF-8 ones, because filepath.Dir
#      does not iterate runes. Neither answer matched the reference. What this
#      slice changed is that the divergence is now WRITTEN DOWN, which is the
#      whole difference between an exclusion and a hole: the next person who
#      adds a non-ASCII `%s` to a stderr write is adding to this group, not
#      using a channel someone modeled for them.
#
# NOT AN EXCLUSION, and it is written down here because for two review rounds it
# looked like one: non-UTF-8 bytes in a FILENAME or an ARGUMENT. Every `\x`-byte
# case in this file built a payload; none built a filename, so the axis was
# UNTESTED rather than excluded — and an untested axis reads exactly like a
# passing one. CPython decodes sys.argv and os.listdir with surrogateescape, the
# port now does the same (cmd/validate-intent/pyfspath.go), and the two are
# compared byte-for-byte in section 15g ("non-UTF-8 filenames and argv — the
# other end of the same channel").
#
# It does reach one ALREADY-RATIFIED non-parity by a new route, and that is
# asserted rather than newly excluded: under the `strict` handler CPython cannot
# encode a lone surrogate for stdout, so a badly-named file kills the TEXT
# renderers on both sides — identical exit code, identical (truncated) stdout,
# and a CPython traceback this port does not reproduce. Same pair as section 15e
# ("lone surrogates on the way OUT, under BOTH handlers"), reached through a
# filename instead of through stdin. `--json` is unaffected and stays a
# byte-for-byte comparison, because ensure_ascii escapes the surrogate into
# ASCII before it ever reaches the encoder.
#
# One remaining KNOWN LIMIT, not an exclusion because nothing here can reach it:
# CPython decodes a file through TextIOWrapper in 8 KiB chunks, so a decode
# failure past the first chunk reports a chunk-relative byte offset while the
# port reports a whole-input one. Every fixture in this repo is orders of
# magnitude below that, and stdin is read in one go by both. Stated so a future
# large-input case is recognised rather than debugged from scratch.
#
# RETIRED EXCLUSIONS — slice 2 (SPGD-102) closed two of slice 1's five, slice 4
# (SPGD-123) closed a third, and slice 3 (SPGD-107) closed two more:
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
#   * Non-UTF-8 input — the PROSE. Slice 1 could name the failing byte but not
#     CPython's error SPAN, which uses a different message template entirely
#     once it blames more than one byte ("bytes in position 10-11" vs "byte
#     0xff in position 0"). cmd/validate-intent/pyutf8.go now reproduces the
#     decoder's span algorithm, so the message is compared rather than waved
#     through. Compared for streams in section 15b ("stdin mode (`-`) — text
#     and --json") and for files in section 15d ("adopter mode --json — three
#     counting rules over one batch"), which carries the file-side twin of the
#     same bytes.
#
#   * stdin (`-`) and adopter-mode `--json`. Implemented in slice 3; the mode
#     matrix is complete and nothing is refused for being unported any more.
#     Compared in section 15b ("stdin mode (`-`) — text and --json") and
#     section 15d ("adopter mode --json — three counting rules over one batch"),
#     and section 16 ("Go-side refusals — the excluded surfaces, still
#     asserted") asserts that each of them has STOPPED refusing, so deleting a
#     dispatch cannot leave the suite green.
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

  # PARITY_ENV (documented on compare_stdin below) applies here too. It has to:
  # PYTHONIOENCODING governs sys.stdout as well as sys.stdin, so the handler
  # changes the answer in adopter and --source mode with no stdin involved, and
  # a primitive that could not carry the variable is a primitive that cannot
  # reach that half of the matrix.
  local py_rc go_rc
  (cd "$cwd" && env ${PARITY_ENV:+"$PARITY_ENV"} \
     "$PYTHON" "$reference" "$@" >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  (cd "$cwd" && env ${PARITY_ENV:+"$PARITY_ENV"} \
     "$gobin" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
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

# compare_stdin <label> <input-file> [args...] — the same comparison with a
# byte-identical stream on stdin.
#
# The input is a FILE rather than a here-string on purpose: stdin mode's whole
# point is arbitrary bytes, and `<<<` appends a newline and cannot carry a NUL.
# Redirecting a file feeds both implementations exactly the bytes on disk.
#
# PARITY_ENV, if set, is applied to BOTH runs — it is how the two CPython error
# handlers get exercised without a second primitive, in this helper and in
# compare_in alike. Set it, call, unset it; a stale value would silently
# re-scope every case after it, so every use below unsets it on the next line.
# compare_stdin_in <cwd> <reference> <go-binary> <label> <input-file> [args...]
# is the same comparison with the pair under test named explicitly, so a section
# can point it at a COPY of both implementations rather than the repo's. Section
# 16e ("stderr's error handler") is the caller: its subject is what each
# implementation does when its own install prefix cannot be named in ASCII,
# which is a property of where the binaries live and cannot be reached from
# here.
compare_stdin_in() {
  local cwd="$1" reference="$2" gobin="$3" label="$4" input="$5"
  shift 5

  local py_rc go_rc
  (cd "$cwd" && env ${PARITY_ENV:+"$PARITY_ENV"} \
     "$PYTHON" "$reference" "$@" <"$input" >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  (cd "$cwd" && env ${PARITY_ENV:+"$PARITY_ENV"} \
     "$gobin" "$@" <"$input" >"$WORK/go.out" 2>"$WORK/go.err")
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
  printf '        args: %s   stdin: %s\n' "$*" "$input"
  local problem
  for problem in "${problems[@]}"; do
    printf '        %s\n' "$problem"
  done
  if ! cmp -s "$WORK/py.out" "$WORK/go.out"; then
    printf '        --- stdout (-python +go), od -c ---\n'
    diff -u <(od -c "$WORK/py.out") <(od -c "$WORK/go.out") | tail -n +3 | sed 's/^/        /'
  fi
  if ! cmp -s "$WORK/py.err" "$WORK/go.err"; then
    printf '        --- stderr (-python +go) ---\n'
    diff -u "$WORK/py.err" "$WORK/go.err" | tail -n +3 | sed 's/^/        /'
  fi
  return 1
}

# compare_stdin <label> <input-file> [args...] — the common case: the repo's own
# reference and the binary this run just built, from the repo root.
compare_stdin() {
  local label="$1" input="$2"
  shift 2
  compare_stdin_in "$REPO_ROOT" "$REFERENCE" "$GO_BIN" "$label" "$input" "$@"
}
PARITY_ENV=""

# assert_trailing_newline <label> <input-file> [args...]
#
# Criteria that a JSON-parsing assertion cannot see. `json.dumps` emits no
# trailing newline and `print` adds exactly one, while Go's MarshalIndent adds
# none and Encoder.Encode adds one — so "ends in exactly one \n" is a real
# choice with three plausible answers, and every one of them parses to the same
# document. Asserted on BYTES, against the Go binary alone, so it stays pinned
# even where a case is otherwise compared as a whole.
assert_trailing_newline() {
  local label="$1" input="$2"
  shift 2
  (cd "$REPO_ROOT" && "$GO_BIN" "$@" <"$input" >"$WORK/nl.out" 2>/dev/null)
  local tail_bytes
  tail_bytes="$(tail -c 2 "$WORK/nl.out" | od -An -c | tr -s ' ')"
  if [ "$tail_bytes" = " } \\n" ]; then
    passed=$((passed + 1))
    printf '  ok    %s (ends "}\\n")\n' "$label"
    return 0
  fi
  failed=$((failed + 1))
  red "  FAIL  $label — expected the last two bytes to be '}' '\\n', got:$tail_bytes"
  return 1
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
echo "== 1. shipped fixtures, one argument at a time =="
for fixture in "$REPO_ROOT"/examples/*.json "$REPO_ROOT"/examples/invalid/*.json; do
  rel="${fixture#"$REPO_ROOT"/}"
  compare "$rel" "$rel"
done

# --------------------------------------------------------------------------- #
# 2. the parity fixtures — the hazards the shipped corpus does not reach
# --------------------------------------------------------------------------- #
echo
echo "== 2. parity fixtures =="
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
echo "== 3. multiple arguments =="
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
echo "== 4. glob expansion =="
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
echo "== 5. recursive ** globbing =="
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
# are the README's own quickstart lines (README.md:72, 78, 82), and the second
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
echo "== 6. no-match =="
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
# 4, cmd/validate-intent/version.go) and `--schema-source` (excluded group 5,
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
#
# It honours PARITY_ENV for the same reason `compare` does, and the reason is
# not hypothetical here: sections 15f ("the ENCODING half of PYTHONIOENCODING")
# and 17c ("the locale's default codec") both compare `--help` under a chosen
# environment, and both are asserting the ACCEPTED half of a gate — that an
# environment the port does not refuse still produces the reference's own bytes.
# A helper that dropped the variable would have run those cases in the harness's
# own environment, where they pass by construction and prove nothing about the
# environment named in their label.
# compare_help_in <reference> <go-binary> <label> [args...] — the same split
# with the pair under test named explicitly, for the same reason
# compare_stdin_in exists. Always run from the repo root: --help reads nothing
# from the working directory, and section 16e's subject is where the BINARIES
# live.
compare_help_in() {
  local reference="$1" gobin="$2" label="$3"
  shift 3

  local py_rc go_rc
  (cd "$REPO_ROOT" && env ${PARITY_ENV:+"$PARITY_ENV"} \
     "$PYTHON" "$reference" "$@" >"$WORK/py.out" 2>"$WORK/py.err")
  py_rc=$?
  (cd "$REPO_ROOT" && env ${PARITY_ENV:+"$PARITY_ENV"} \
     "$gobin" "$@" >"$WORK/go.out" 2>"$WORK/go.err")
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

# compare_help <label> [args...] — the common case.
compare_help() {
  local label="$1"
  shift
  compare_help_in "$REFERENCE" "$GO_BIN" "$label" "$@"
}

echo
echo "== 7. help =="
compare_help "--help"                   --help
compare_help "-h"                       -h
compare_help "--help wins over a file"  'examples/*.json' --help
compare_help "--help wins over a missing file" nope.json -h
# --help wins over --version too, in either order. This is a real COMPARISON and
# not a Go-side assertion, which is the whole reason it is worth having:
# `--version` is a Go-only flag (excluded group 4), so the tempting move is to
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
# The same crossing for the second Go-only surface (excluded group 5). It is
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
echo "== 8. read and schema failures =="

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
# are supposed to diverge — that is excluded group 2, and what the Go binary
# does there instead is pinned in section 16d ("excluded group 2 — a tree with
# no schemas/ directory beside the binary").
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

# WHICH ERROR WINS when the schema is unloadable AND --json asks for a mode that
# refuses it. The reference loads the schema before it ever looks at as_json
# (bin/validate-intent:895 vs :902), so the schema error wins and the self-test
# refusal is never reached. Both paths exit 2, so an exit-code assertion sees
# nothing — only the stderr comparison catches an implementation that ordered
# its own refusal first. The port had exactly that ordering; main.go now
# reproduces the reference's, and this is what holds it there.
#
# The probe has to be a schema that EXISTS and cannot be loaded. It used to be a
# tree with no schemas/ directory at all, and that stopped working as a probe
# when SPGD-131 gave the Go binary an embedded fallback: on a schema-LESS tree
# the port now loads its compiled-in copy and never reaches either branch, so
# the crossing would pass whichever side of the load the refusal sat on. A
# schema present but not JSON is a different exception class reaching the same
# handler, so the ordering is pinned by the message as well as by the path.
schema_root3="$WORK/malformedschema"
mkdir -p "$schema_root3/bin" "$schema_root3/schemas"
cp "$REFERENCE" "$schema_root3/bin/validate-intent"
cp "$GO_BIN" "$schema_root3/bin/validate-intent-go"
printf 'not json at all' > "$schema_root3/schemas/open-test-intent.v1.json"
compare_in "$schema_root3" "$schema_root3/bin/validate-intent" \
  "$schema_root3/bin/validate-intent-go" \
  "malformed schema beats the self-test --json refusal" --json
# ...and the refusal still fires when the schema DOES load, so the case above
# cannot pass by having lost the refusal altogether.
compare "self-test --json is refused when the schema loads" --json

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
echo "== 9. grown schema: pattern, bounds, items, nested paths =="

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
echo "== 10. --source over the shipped corpus =="
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
echo "== 11. self-test =="
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
echo "== 12. malformed payloads and documents =="

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
echo "== 13. --source hazards =="

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
echo "== 14. --source --json =="
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
echo "== 15. self-test empty-fixture-set guard =="

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

# An examples/ tree that is THERE and holds nothing: all four sets empty, all
# four diagnostics.
#
# The directory is created deliberately, and since SPGD-315 it is what makes
# this a COMPARISON at all. The Go binary now carries the corpus, and falls back
# to it when there is no examples/ tree beside the executable — so on a root
# with no such directory the two implementations diverge by design (Python
# reports four empty sets, the port runs its embedded twelve), and that state is
# pinned on the Go side in section 16d instead. A tree that EXISTS and is empty
# is not an absent tree: disk wins, nothing matches, and both implementations
# say so identically. That is also the shape the guard is actually FOR — fixtures
# deleted out of a checkout — so the case tests more here than it did as a bare
# root, not less.
bare_root="$(make_fixture_root selftest-bare)"
mkdir -p "$bare_root/examples"
compare_root "$bare_root" "self-test: an empty fixture tree, every set empty"

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
# 15b. stdin mode (`-`) — text and --json
# --------------------------------------------------------------------------- #
#
# The programmatic input path, and the mode where a divergence costs most: the
# caller has no filename to sanity-check the answer against, only the document
# that came back.
#
# Every input here is a FILE of exact bytes, fed to both implementations by
# redirection, because half of what is being pinned is what happens to bytes
# that are not text.
echo
echo "== 15b. stdin (-) =="

STDIN_DIR="$WORK/stdin"
mkdir -p "$STDIN_DIR"

cp "$REPO_ROOT/examples/unit-order-total.json" "$STDIN_DIR/valid.json"
printf '{"layer":"e2e"}'                > "$STDIN_DIR/schemafail.json"
printf '{"broken"'                      > "$STDIN_DIR/malformed.json"
printf ''                               > "$STDIN_DIR/empty.json"
printf '   \n\t '                       > "$STDIN_DIR/whitespace.json"
printf '[1,2,3]'                        > "$STDIN_DIR/not-an-object.json"
# A non-ASCII enum value: json.dumps escapes it to é while Go's own
# marshaller would emit it raw. The exact opposite of the `<root>` case below.
printf '{"layer":"caf\xc3\xa9"}'        > "$STDIN_DIR/nonascii.json"
# NUL and other control characters inside a string literal — the parser's
# problem, and repr's.
printf '{"layer":"a\\u0000b"}'          > "$STDIN_DIR/control.json"
# Bytes that are not UTF-8 at all, in three shapes that make CPython's decoder
# reach for three different message templates: a stray lead byte (singular
# "byte 0xff in position 0"), a truncated three-byte character followed by more
# input (plural "bytes in position 10-11"), and a truncated one at EOF
# ("unexpected end of data").
printf '\xff\xfe{"layer":"unit"}'       > "$STDIN_DIR/bad-lead.json"
printf '{"layer":"\xe2\x82"}'           > "$STDIN_DIR/bad-continuation.json"
printf '{"layer":"unit\xe2\x82'         > "$STDIN_DIR/truncated-at-eof.json"
# A KEY holding undecodable bytes. This is the one that reaches the renderers
# unescaped: "missing required property '%s'" and "additional property '%s' is
# not allowed" interpolate key names with %s, not %r (bin/validate-intent:169,
# 178), so a lone surrogate lands in the output rather than being repr'd on the
# way. Text mode writes it back out as the original bytes (sys.stdout is
# surrogateescape too); --json escapes it to \udcXX.
printf '{"la\xe2\x82yer":"unit"}'       > "$STDIN_DIR/bad-key.json"

for case in valid schemafail malformed empty whitespace not-an-object nonascii \
            control bad-lead bad-continuation truncated-at-eof bad-key; do
  compare_stdin "stdin text: $case" "$STDIN_DIR/$case.json" -
  compare_stdin "stdin json: $case" "$STDIN_DIR/$case.json" - --json
done

# --json is stripped before the positional dispatch, so it may sit anywhere.
compare_stdin "stdin json: --json leads"   "$STDIN_DIR/valid.json" --json -
compare_stdin "stdin json: --json trails"  "$STDIN_DIR/valid.json" - --json
# stdin mode ignores every argument after `-` (bin/validate-intent:911). A port
# that validated them instead would refuse patterns the reference never looks
# at — including `**`, which is otherwise a Go-side refusal.
compare_stdin "stdin: trailing arguments are ignored" "$STDIN_DIR/valid.json" - nope.json
compare_stdin "stdin: even a ** argument is ignored"  "$STDIN_DIR/valid.json" - 'examples/**/*.json'
compare_stdin "stdin json: trailing arguments ignored" "$STDIN_DIR/valid.json" - nope.json --json

# The trailing-newline contract, on bytes. Invisible to any assertion that
# parses the document first.
assert_trailing_newline "stdin json: exactly one trailing newline (pass)" \
  "$STDIN_DIR/valid.json" - --json
assert_trailing_newline "stdin json: exactly one trailing newline (fail)" \
  "$STDIN_DIR/schemafail.json" - --json

# --------------------------------------------------------------------------- #
# 15c. the OTHER error handler, and the assumption behind the default
# --------------------------------------------------------------------------- #
#
# `sys.stdin.errors` decides whether undecodable bytes are a READ failure or a
# string full of lone surrogates, and the two produce different `kind` values,
# different `summary.annotations` and sometimes different exit codes for the
# SAME bytes. The port reproduces CPython's choice
# (cmd/validate-intent/pyioerrors.go, pyIOErrors) rather than hard-coding one,
# so both branches are compared.
#
# ONE HANDLER, TWO STREAMS. PYTHONIOENCODING is not a stdin setting: it gives
# sys.stdout the same handler, and the port answers for the output side from the
# same function. Both values are asserted here, not just the one this section is
# named for — a port that read the variable for the input side and hard-coded
# the output side is exactly what round 4 of SPGD-107's review found, and it
# diverged with the SAME exit code, in every mode, with no stdin involved.
echo
echo "== 15c. stdin error handlers =="
read -r default_in default_out <<<"$("$PYTHON" -c \
  'import sys; print(sys.stdin.errors, sys.stdout.errors)' </dev/null)"
if [ "$default_in" = "surrogateescape" ] && [ "$default_out" = "surrogateescape" ]; then
  passed=$((passed + 1))
  printf '  ok    sys.stdin.errors and sys.stdout.errors both default to surrogateescape\n'
else
  failed=$((failed + 1))
  red "  FAIL  defaults are stdin='$default_in' stdout='$default_out', not both 'surrogateescape'"
  printf '        pyIOErrors() assumes the C/POSIX-locale default for BOTH streams;\n'
  printf '        this environment disagrees, so section 15b ("stdin mode (`-`) — text and --json")\n'
  printf '        is comparing the wrong branch.\n'
fi

# The premise pyIOErrors rests on, asserted rather than assumed: whatever the
# handler is, the two streams carry the SAME one. If CPython ever set them
# apart, answering for stdout from stdin's variable would be unsound — so this
# says so, rather than the port picking the wrong one in silence.
for spec in "" "utf-8" ":replace" "utf-8:ignore"; do
  read -r probe_in probe_out <<<"$(env ${spec:+PYTHONIOENCODING=$spec} "$PYTHON" -c \
    'import sys; print(sys.stdin.errors, sys.stdout.errors)' </dev/null)"
  if [ "$probe_in" = "$probe_out" ]; then
    passed=$((passed + 1))
    printf '  ok    PYTHONIOENCODING=%-12s -> both streams %s\n' "${spec:-(unset)}" "$probe_in"
  else
    failed=$((failed + 1))
    red "  FAIL  PYTHONIOENCODING=${spec:-(unset)} — stdin='$probe_in' but stdout='$probe_out'"
    printf '        cmd/validate-intent/pyioerrors.go answers for BOTH streams from one\n'
    printf '        function, on the premise that they always agree. Here they do not.\n'
  fi
done

# strict, on the way IN: the UnicodeDecodeError -> KIND_READ path, with
# annotations 0 — the only route to a `read` finding that stdin mode has, and
# dead code under the default handler.
PARITY_ENV="PYTHONIOENCODING=utf-8"
for case in valid bad-lead bad-continuation truncated-at-eof bad-key; do
  compare_stdin "stdin text (strict): $case" "$STDIN_DIR/$case.json" -
  compare_stdin "stdin json (strict): $case" "$STDIN_DIR/$case.json" - --json
done
PARITY_ENV=""

# An error handler the port does NOT reproduce must refuse, not substitute one
# it does. `replace` decodes successfully into different text, so answering with
# surrogateescape's verdict would be confidently about the wrong string.
unsupported_rc=0
(cd "$REPO_ROOT" && PYTHONIOENCODING=:replace "$GO_BIN" - \
   <"$STDIN_DIR/bad-lead.json" >"$WORK/go.out" 2>"$WORK/go.err") || unsupported_rc=$?
if [ "$unsupported_rc" -eq 2 ] && [ -s "$WORK/go.err" ] && [ ! -s "$WORK/go.out" ]; then
  passed=$((passed + 1))
  printf '  ok    an unreproducible stdin error handler refuses (exit 2: %s)\n' \
    "$(head -1 "$WORK/go.err")"
else
  failed=$((failed + 1))
  red "  FAIL  PYTHONIOENCODING=:replace — expected a clean exit-2 refusal, got rc=$unsupported_rc"
fi

# --------------------------------------------------------------------------- #
# 15d. adopter mode --json — three counting rules over one batch
# --------------------------------------------------------------------------- #
#
# `files`, `annotations` and `failed` are three DIFFERENT counts and a naive
# port collapses them into one. The mixed batch below is built so all three
# disagree — 4 / 3 / 4 over five arguments — which is the only arrangement that
# can tell a collapsed counter from a correct one:
#
#   files       every file matched, readable or not; the no-match PATTERN is
#               not a file and does not count.
#   annotations sites EXAMINED; the malformed file counts (the site existed,
#               its payload was bad), the unreadable one does not.
#   failed      every failing finding, INCLUDING the no-match that neither of
#               the other two counted.
echo
echo "== 15d. adopter --json =="

# The shipped corpus first — and note what it proves: these fixtures' messages
# contain `<root>`, which Go's encoding/json escapes to `<root>` by
# default. Parity breaks on the very first shipped invalid fixture unless the
# document is rendered by hand.
compare "adopter --json: one valid fixture"   --json examples/unit-order-total.json
compare "adopter --json: the invalid corpus"  --json 'examples/invalid/*.json'
compare "adopter --json: the whole corpus"    --json 'examples/*.json' 'examples/invalid/*.json'
compare "adopter --json: position-independent (trailing)" 'examples/invalid/*.json' --json
compare "adopter --json: several explicit files" --json \
  examples/unit-order-total.json examples/invalid/bad-layer.json

# The mixed batch.
MIXED="$WORK/mixed"
mkdir -p "$MIXED"
cp "$REPO_ROOT/examples/unit-order-total.json" "$MIXED/ok.json"
printf '{"broken"'          > "$MIXED/bad.json"
printf '{"layer":"e2e"}'    > "$MIXED/schemafail.json"
printf '{}'                 > "$MIXED/unread.json"

compare_in "$REPO_ROOT" "$REFERENCE" "$GO_BIN" "adopter --json: no-match is a finding" \
  --json 'nope*.json'
compare_in "$REPO_ROOT" "$REFERENCE" "$GO_BIN" "adopter --json: no-match mixed with matches" \
  --json 'examples/*.json' 'nope*.json'

chmod 000 "$MIXED/unread.json"
if [ -r "$MIXED/unread.json" ]; then
  skipped=$((skipped + 1))
  red "  SKIP  adopter --json mixed batch — chmod 000 did not make the file unreadable (running as root?)"
else
  compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
    "adopter --json: mixed batch (files 4 / annotations 3 / failed 4)" \
    --json ok.json bad.json schemafail.json unread.json 'nope*.json'
  # The summary is the point of that case, so it is also asserted directly
  # against the Go binary. A whole-document comparison would go green if BOTH
  # implementations were compared with the summary lines absent; this cannot.
  (cd "$MIXED" && "$GO_BIN" --json ok.json bad.json schemafail.json unread.json \
     'nope*.json' >"$WORK/mixed.out" 2>/dev/null)
  got="$(tr -d ' \n' < "$WORK/mixed.out" | sed 's/.*"summary":{\([^}]*\)}.*/\1/')"
  want='"files":4,"annotations":3,"failed":4'
  if [ "$got" = "$want" ]; then
    passed=$((passed + 1))
    printf '  ok    adopter --json: summary is %s\n' "$want"
  else
    failed=$((failed + 1))
    red "  FAIL  adopter --json summary — want $want, got $got"
  fi
fi
chmod 644 "$MIXED/unread.json"

# A non-UTF-8 FILE is a read finding with annotations 0 — the file-side twin of
# the strict-stdin case above, and now compared for its PROSE too (the exclusion
# that slice 3 retired).
printf '\xff\xfe{"layer":"unit"}' > "$MIXED/bad-lead.json"
printf '{"layer":"\xe2\x82"}'     > "$MIXED/bad-continuation.json"
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" "adopter: non-UTF-8 file, text" bad-lead.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" "adopter --json: non-UTF-8 file (read, annotations 0)" \
  --json bad-lead.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" "adopter: truncated multi-byte file, text" \
  bad-continuation.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" "adopter --json: truncated multi-byte file" \
  --json bad-continuation.json

# --------------------------------------------------------------------------- #
# 15e. lone surrogates on the way OUT, under BOTH handlers
# --------------------------------------------------------------------------- #
#
# The mirror of the stdin decoding above, and reachable with no stdin at all: a
# file whose KEY carries a "\udc82" escape puts a lone surrogate into a str, and
# "additional property '%s' is not allowed" interpolates key names with %s, not
# %r (bin/validate-intent:178) — so unlike every other interpolated value it is
# NOT repr'd into ASCII on the way out. See cmd/validate-intent/pystdout.go.
#
# THE HANDLER AXIS IS THE POINT OF THIS SECTION, and it used to be missing. What
# sys.stdout does with such a character depends on PYTHONIOENCODING's handler
# exactly as the input side does — under `surrogateescape` CPython writes the
# ORIGINAL BYTE back, under `strict` it raises UnicodeEncodeError mid-report and
# dies. Section 15c ("the OTHER error handler, and the assumption behind the
# default") ran strict-mode stdin over inputs that either decode cleanly or fail
# the READ, and this section ran its escape cases only under the default
# environment; the product of the two axes was the one cell nothing covered, and
# the port diverged in it. Every case below is therefore run under both.
#
# Three inputs, because the two surrogate ranges behave differently and the
# difference is a range boundary:
#
#   esc   U+DC82  in U+DC80-U+DCFF, the range the DECODER produces
#   high  U+DCFF  the upper bound of that range
#   unenc U+D800  outside it — only reachable from an explicit \uXXXX escape
#
# and two renderers, because they do NOT agree: json.dumps' ensure_ascii=True
# turns the surrogate into a literal \udc82 in the JSON text, so the --json path
# writes pure ASCII and never reaches the encoder at all. That makes --json a
# byte-for-byte comparison in every cell while text mode is a comparison in
# three of six and an assert-pair in the other three. Measured, not assumed.
echo
echo "== 15e. lone surrogates on stdout =="
printf '{"\\udc82key":"x"}'      > "$MIXED/escaped-surrogate.json"
printf '{"a\\udcffb":"x"}'       > "$MIXED/escaped-surrogate-high.json"
printf '{"\\ud800key":"x"}'      > "$MIXED/unencodable-surrogate.json"

# assert_encode_failure <label> <env-or-empty> <stdin-file-or-empty> [args...]
#
# The reference dies with a traceback naming CPython's own source lines, which
# is not a surface this port can be byte-identical to. So the pair is ASSERTED
# rather than compared: identical exit 1, identical stdout (both truncate at the
# line that failed, because TextIOWrapper encodes before it buffers), and a
# stated prose difference on stderr.
#
# stdout is compared with cmp, not merely required to be non-empty. That is the
# half a "both failed" assertion would throw away — the reference's partial
# report is the evidence that the port stops at the same character, and an
# implementation that wrote nothing at all would otherwise pass.
#
# The stderr check is not "the port said something". The offending character is
# lifted out of CPython's own message and required VERBATIM in the port's, so
# the two must agree on WHICH character failed and on how it is spelled. Go's
# string(rune) turns a surrogate into U+FFFD, which produced a diagnostic that
# read perfectly and named the replacement character; only an assertion tied to
# the reference's own text catches that.
assert_encode_failure() {
  local label="$1"
  shift
  assert_encode_failure_in "$MIXED" "$label" "$@"
}

# assert_encode_failure_in <cwd> <label> <env-or-empty> <stdin-file-or-empty> [args...]
#
# The same assertion from a directory of the caller's choosing. Section 15g
# ("non-UTF-8 filenames and argv — the other end of the same channel") needs it:
# a lone surrogate reaches the text renderers through a FILENAME as well as
# through stdin and a JSON escape, so the same ratified pair has to be pinned
# over a fixture tree that is not $MIXED.
assert_encode_failure_in() {
  local cwd="$1" label="$2" penv="$3" input="$4"
  shift 4
  local py_rc=0 go_rc=0
  if [ -n "$input" ]; then
    (cd "$cwd" && env ${penv:+"$penv"} "$PYTHON" "$REFERENCE" "$@" \
       <"$input" >"$WORK/py.out" 2>"$WORK/py.err") || py_rc=$?
    (cd "$cwd" && env ${penv:+"$penv"} "$GO_BIN" "$@" \
       <"$input" >"$WORK/go.out" 2>"$WORK/go.err") || go_rc=$?
  else
    (cd "$cwd" && env ${penv:+"$penv"} "$PYTHON" "$REFERENCE" "$@" \
       </dev/null >"$WORK/py.out" 2>"$WORK/py.err") || py_rc=$?
    (cd "$cwd" && env ${penv:+"$penv"} "$GO_BIN" "$@" \
       </dev/null >"$WORK/go.out" 2>"$WORK/go.err") || go_rc=$?
  fi
  # e.g. `can't encode character '\udc82'` — CPython's spelling, not ours.
  local py_char
  py_char="$(grep -o "can't encode character '[^']*'" "$WORK/py.err" | head -1)"
  if [ "$py_rc" -eq 1 ] && [ "$go_rc" -eq 1 ] &&
     grep -q 'UnicodeEncodeError' "$WORK/py.err" &&
     [ -n "$py_char" ] && grep -qF "$py_char" "$WORK/go.err" &&
     cmp -s "$WORK/py.out" "$WORK/go.out"; then
    passed=$((passed + 1))
    printf '  ok    %s: same exit 1, same stdout, same character named\n' "$label"
    return 0
  fi
  failed=$((failed + 1))
  red "  FAIL  $label — python rc=$py_rc go rc=$go_rc"
  printf '        python stderr: %s\n' "$(tail -1 "$WORK/py.err")"
  printf '        go stderr:     %s\n' "$(head -1 "$WORK/go.err")"
  if [ -n "$py_char" ] && ! grep -qF "$py_char" "$WORK/go.err"; then
    printf '        the port does not name the character CPython names: %s\n' "$py_char"
  fi
  if ! cmp -s "$WORK/py.out" "$WORK/go.out"; then
    printf '        --- stdout (-python +go), od -c ---\n'
    diff -u <(od -c "$WORK/py.out") <(od -c "$WORK/go.out") | tail -n +3 | sed 's/^/        /'
  fi
  return 1
}

# ---- default handler (surrogateescape) ----
#
# The escape range round-trips: CPython writes the original byte, and so must
# the port. Every other byte of these runs agrees, which is exactly what makes
# it the shape of divergence that ships.
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "surrogateescape range on stdout, text" escaped-surrogate.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "surrogateescape range on stdout, --json" --json escaped-surrogate.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "surrogateescape range, upper bound (U+DCFF)" escaped-surrogate-high.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "surrogateescape range, upper bound, --json" --json escaped-surrogate-high.json

# U+D800 is outside what surrogateescape can encode, so even the default
# handler raises here. A DEFECT IN THE REFERENCE, not a port gap.
assert_encode_failure "an unencodable surrogate (surrogateescape)" "" "" \
  unencodable-surrogate.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "an unencodable surrogate, --json (ensure_ascii escapes it)" \
  --json unencodable-surrogate.json

# ---- strict (PYTHONIOENCODING=utf-8) — the cell that was missing ----
#
# Now the ESCAPE range raises too, because nothing under `strict` ever produced
# it: the decoder raised instead, so a surrogate here can only have come from a
# \uXXXX escape, and CPython refuses to encode it. The port applied the
# surrogateescape encoder in every mode until round 4, which made it write byte
# 0x82 and finish the line where the reference truncated and died.
assert_encode_failure "escape range under strict, adopter text" \
  "PYTHONIOENCODING=utf-8" "" escaped-surrogate.json
assert_encode_failure "escape range upper bound under strict, adopter text" \
  "PYTHONIOENCODING=utf-8" "" escaped-surrogate-high.json
assert_encode_failure "unencodable surrogate under strict, adopter text" \
  "PYTHONIOENCODING=utf-8" "" unencodable-surrogate.json

# stdin mode, same environment, same bytes: the reviewer's reported case. The
# document is pure ASCII on the wire, so it decodes fine under `strict` — the
# surrogate arrives from the JSON escape, not from the bytes — which is why the
# input-side gate could never have caught this.
assert_encode_failure "escape range under strict, stdin text" \
  "PYTHONIOENCODING=utf-8" "$MIXED/escaped-surrogate.json" -

# --json under strict stays a byte-for-byte comparison in every one of them,
# because ensure_ascii=True leaves the encoder nothing to fail on. A section
# that only asserted the failures would have missed that the two renderers
# genuinely disagree here.
PARITY_ENV="PYTHONIOENCODING=utf-8"
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "escape range under strict, --json" --json escaped-surrogate.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "escape range upper bound under strict, --json" --json escaped-surrogate-high.json
compare_in "$MIXED" "$REFERENCE" "$GO_BIN" \
  "unencodable surrogate under strict, --json" --json unencodable-surrogate.json
compare_stdin "escape range under strict, stdin --json" \
  "$MIXED/escaped-surrogate.json" - --json
PARITY_ENV=""

# --json in SELF-TEST mode is a usage error in the reference too — exit 2, one
# error line, then the whole usage block on stderr. Compared, not asserted: it
# is reproduced behaviour, not a port limitation, and the difference matters if
# the reference's usage text ever changes.
compare "--json is refused in self-test mode" --json

# The trailing-newline contract again, on a document with findings.
assert_trailing_newline "adopter --json: exactly one trailing newline" \
  /dev/null --json 'examples/invalid/*.json'

# --------------------------------------------------------------------------- #
# 15f. the ENCODING half of PYTHONIOENCODING
# --------------------------------------------------------------------------- #
#
# `PYTHONIOENCODING` is `ENCODING:HANDLER`. Section 15c ("the OTHER error
# handler, and the assumption behind the default") covers the second field. This
# one covers the first, which round 5 of SPGD-107's review found parsed and then
# discarded: the port inferred `strict` from "an encoding is named" and never
# looked at WHICH, so `latin-1` — handler `strict`, which the port reproduces —
# went straight through a gate whose whole job was to catch it, while CPython
# encoded and decoded both streams as iso8859-1 and the port hard-coded UTF-8.
#
# THE MISSING CELL, precisely. Every PYTHONIOENCODING value this harness set
# before this section was unset, `utf-8`, `:replace` or `utf-8:ignore` — each of
# which either omits the encoding field or names the one the port assumes. The
# handler axis was varied across four spellings; the encoding axis was never
# varied at all, so it was UNTESTED rather than excluded, and an untested axis
# reads exactly like a passing one.
#
# The port's answer is to REFUSE any codec that is not UTF-8 (asserted per mode
# in section 16, "Go-side refusals — the excluded surfaces, still asserted").
# This section establishes the two things that refusal rests on and that a
# refusal alone cannot show:
#
#   1. the codecs it refuses genuinely make the REFERENCE answer differently, so
#      the gate is not refusing a run it could have reproduced; and
#   2. the codecs it accepts — including six aliases of utf-8 that are not
#      spelled "utf-8" — are still compared byte-for-byte, so closing the hole
#      did not close it too far.
#
# Without (1) the refusal could be tightened to nothing and every test here
# would stay green, which is this project's house defect: a gate reporting
# success having verified nothing.
echo
echo "== 15f. the PYTHONIOENCODING encoding =="

# The premise pyIOEncodingSupported rests on, asserted rather than assumed.
# Same shape as 15c's handler probe, on the other field: whatever the codec is,
# both streams carry the SAME one, and with no encoding named it is utf-8 —
# which is what the port hard-codes and what makes the unset/`:replace` cases
# reproducible at all. Asked of python3 at run time so an image whose locale
# default is not UTF-8 reports a broken assumption instead of quietly diverging.
#
# os.write to the raw fd, not print(): this probe runs UNDER the encodings it
# reports, and `print` would encode its own answer with them — the utf-16 row
# comes back as UTF-16 and unparseable otherwise.
for spec in "" "utf-8" ":replace" "latin-1" "ascii" "utf-16"; do
  read -r enc_in enc_out <<<"$(env ${spec:+PYTHONIOENCODING=$spec} "$PYTHON" -c \
    'import os, sys; os.write(1, ("%s %s\n" % (sys.stdin.encoding, sys.stdout.encoding)).encode("ascii"))' </dev/null)"
  # Does this spelling NAME an encoding, or leave CPython on the locale default?
  names_encoding=yes
  case "$spec" in "" | :*) names_encoding=no ;; esac

  if [ "$enc_in" != "$enc_out" ]; then
    failed=$((failed + 1))
    red "  FAIL  PYTHONIOENCODING=${spec:-(unset)} — stdin codec '$enc_in' but stdout codec '$enc_out'"
    printf '        cmd/validate-intent/pyioerrors.go answers for BOTH streams from one\n'
    printf '        function, on the premise that they always agree. Here they do not.\n'
  elif [ "$names_encoding" = no ] && [ "$enc_in" != "utf-8" ]; then
    # The rows that name no encoding resolve to the locale default, and the
    # port accepts them because that default is UTF-8 here. If it is not, the
    # port is answering in the wrong codec rather than refusing.
    failed=$((failed + 1))
    red "  FAIL  PYTHONIOENCODING=${spec:-(unset)} — locale default codec is '$enc_in', not utf-8"
    printf '        pyIOEncodingSupported() accepts an empty encoding field on the\n'
    printf '        premise that the default is UTF-8. On this image it is not.\n'
  else
    passed=$((passed + 1))
    printf '  ok    PYTHONIOENCODING=%-10s -> both streams %s\n' "${spec:-(unset)}" "$enc_in"
  fi
done

# (1) The refusal is not vacuous: the REFERENCE really does answer differently.
#
# iso8859-1 decodes every byte, so bytes that are not valid UTF-8 never reach
# the reference's read failure at all — the document parses and is rejected on
# SCHEMA instead. Measured on the same input, stdin --json:
#
#   PYTHONIOENCODING=utf-8     kind "read",   annotations 0, 1 error,  rc 1
#   PYTHONIOENCODING=latin-1   kind "schema", annotations 1, 5 errors, rc 1
#
# That is the read/parse split of section 15b ("stdin mode (`-`) — text and
# --json") inverted, with the SAME exit code on both sides. This asserts python3
# against python3 — no Go involved — so if a future CPython ever made the two
# agree, the port's refusal would have become over-refusal and this says so
# rather than the refusal quietly outliving its reason.
ENC_DIR="$WORK/encoding"
mkdir -p "$ENC_DIR"
# Valid JSON structurally, and NOT valid UTF-8: 0xe2 0x82 is a truncated
# three-byte sequence, which iso8859-1 happily reads as two characters.
printf '{"schema":"open-test-intent/v1","action":"a","entity":"e","layer":"\xe2\x82unit"}' \
  > "$ENC_DIR/not-utf8.json"

assert_reference_diverges_on_codec() {
  local label="$1" codec="$2"
  shift 2
  local utf8_rc=0 other_rc=0
  (cd "$REPO_ROOT" && PYTHONIOENCODING=utf-8 "$PYTHON" "$REFERENCE" "$@" \
     <"$ENC_DIR/not-utf8.json" >"$WORK/py.utf8" 2>/dev/null) || utf8_rc=$?
  (cd "$REPO_ROOT" && PYTHONIOENCODING="$codec" "$PYTHON" "$REFERENCE" "$@" \
     <"$ENC_DIR/not-utf8.json" >"$WORK/py.other" 2>/dev/null) || other_rc=$?
  if cmp -s "$WORK/py.utf8" "$WORK/py.other" && [ "$utf8_rc" = "$other_rc" ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — the reference answers IDENTICALLY under utf-8 and $codec"
    printf '        The port refuses %s because it changes the reference'"'"'s answer.\n' "$codec"
    printf '        It no longer does, so the refusal asserted in\n'
    printf '        section 16 ("Go-side refusals — the excluded surfaces, still asserted")\n'
    printf '        is now over-refusal: the port declines a run it could\n'
    printf '        reproduce byte-for-byte.\n'
    return 1
  fi
  passed=$((passed + 1))
  printf '  ok    %s (utf-8 rc=%s/%sB vs %s rc=%s/%sB)\n' "$label" \
    "$utf8_rc" "$(wc -c <"$WORK/py.utf8")" "$codec" "$other_rc" "$(wc -c <"$WORK/py.other")"
  return 0
}

assert_reference_diverges_on_codec "latin-1 changes the reference's stdin verdict" \
  latin-1 -
assert_reference_diverges_on_codec "latin-1 changes it under --json too" \
  latin-1 - --json
# `ascii` is the sharper one: it decodes nothing above 0x7f, and then cannot
# ENCODE the em dash in the port's own report prose, so the reference dies with
# UnicodeEncodeError and a truncated stdout. The port used to emit a full,
# clean, plausible report and exit 1 where the reference produced no report at
# all — answering confidently where the oracle declined to answer.
assert_reference_diverges_on_codec "ascii makes the reference die mid-report" \
  ascii -
assert_reference_diverges_on_codec "ascii, --json" \
  ascii - --json

# (2) The other direction: a whitelist that is too tight is also a defect, and
# it is the one this fix could plausibly have introduced. CPython resolves six
# aliases plus several punctuations to the utf-8 codec, and it ANSWERS every one
# of them — so the port must too, byte-for-byte, rather than refusing anything
# it does not recognise on sight.
#
# Compared, not asserted. `U8` and `cp65001` under the too-tight implementation
# (`encoding == "utf-8"`) would refuse with exit 2 while the reference printed a
# report, which is a parity failure these lines catch and a bare "did it refuse?"
# assertion would not.
for spec in "utf-8" "UTF-8" "utf8" "utf_8" "utf---8" "U8" "utf" "cp65001" \
            "utf8_ucs2" "utf8_ucs4" "utf-8:strict"; do
  PARITY_ENV="PYTHONIOENCODING=$spec"
  compare_stdin "utf-8 alias '$spec' is answered, not refused (stdin --json)" \
    "$ENC_DIR/not-utf8.json" - --json
  compare "utf-8 alias '$spec' is answered, not refused (adopter)" \
    'examples/invalid/*.json'
  PARITY_ENV=""
done

# --------------------------------------------------------------------------- #
# 15g. non-UTF-8 FILENAMES and argv — the other end of the same channel
# --------------------------------------------------------------------------- #
#
# Every `\x`-byte case above this line builds a PAYLOAD. Not one builds a
# filename or an argument, and that gap is what round 6 of SPGD-107's review
# found: `pySurrogateEscape` was wired to sys.stdin and to nothing else, so a
# byte that is not valid UTF-8 in a FILENAME decoded to U+FFFD where CPython
# produces U+DC00+byte. The axis was UNTESTED rather than excluded, and the
# exclusions header did not name it — which reads exactly like covered.
#
# CPython decodes sys.argv and every os.listdir result with the filesystem
# encoding under surrogateescape, so this is the same channel as stdin's, at the
# other end. See cmd/validate-intent/pyfspath.go.
#
# WHY IT IS WORTH A SECTION OF ITS OWN, rather than one more case above.
# The failure is LOSSY, and lossy failures do not stay local. U+FFFD is one
# value, so three files whose names differ only in that byte collapse onto ONE
# `file` key while `summary.files` still says three — in documents of identical
# byte length, with identical exit codes and identical structure. A consumer
# aggregating findings across invocations silently loses two of them. The
# distinct-key assertion below is the part a whole-document comparison would
# also catch but a structural one would not, and it is written out because it is
# the property the field exists for (see JSONFinding in report.go).
#
# It does not need exotic argv: an ordinary ASCII glob over a real badly-named
# file reaches it, which is why the fixtures here are matched with `*.json`.
#
# ORDERING IS PART OF THE CONTRACT. _expand_files sorts (bin/validate-intent:464)
# and Python sorts `str` by code point. Sorting the raw BYTES is a different
# order, and it agrees by luck on most name sets: every escaped byte becomes
# U+DC80-U+DCFF, whose WTF-8 lead byte is 0xED, so the fixtures below include a
# name carrying U+FF21 (lead 0xEF) — which sorts after all of them decoded and
# in the middle of them raw. Without that one character an implementation that
# sorted before decoding would pass this whole section.
echo
echo "== 15g. non-UTF-8 filenames and argv =="

BAD_DIR="$WORK/badnames"
mkdir -p "$BAD_DIR/sub"
# A directory whose own NAME is undecodable, so the escape has to survive being
# a path PREFIX and not only a final component.
BAD_SUBDIR="$BAD_DIR/$(printf 'd\xe2\x82')"
mkdir -p "$BAD_SUBDIR"

cp examples/unit-order-total.json "$BAD_DIR/$(printf 'x\xe9.json')"
cp examples/unit-order-total.json "$BAD_DIR/$(printf 'x\xee.json')"
cp examples/unit-order-total.json "$BAD_DIR/$(printf 'x\xf0.json')"
printf '{"broken"' > "$BAD_DIR/$(printf 'x\xff.json')"
cp examples/unit-order-total.json "$BAD_DIR/xé.json"
cp examples/unit-order-total.json "$BAD_DIR/xＡ.json"
cp examples/invalid/missing-required.json "$BAD_DIR/$(printf 'bad\x80.json')"
cp examples/unit-order-total.json "$BAD_SUBDIR/inside.json"
cp examples/sources/checkout_service_test.py "$BAD_DIR/sub/$(printf 's\xe9.py')"

# THE FIXTURE INVENTORY, checked before anything is compared.
#
# Not defensive noise — this caught a real vacuous pass. An earlier draft copied
# `examples/sources/test_order_total.py`, which does not exist: `cp` failed, the
# `sub/` directory stayed empty, and BOTH implementations then answered
# "no file(s) match ..." byte-identically. Two green `--source` cases that
# compared a missing file to a missing file, in a section whose entire subject
# is what happens to a badly-named one.
#
# A glob-based comparison cannot tell "agreed about the files" from "agreed
# there were none", so the inventory is asserted separately and the run stops
# here if it is short.
bad_fixture_missing=0
for expected in "$BAD_DIR/$(printf 'x\xe9.json')" "$BAD_DIR/$(printf 'x\xee.json')" \
                "$BAD_DIR/$(printf 'x\xf0.json')" "$BAD_DIR/$(printf 'x\xff.json')" \
                "$BAD_DIR/xé.json" "$BAD_DIR/xＡ.json" \
                "$BAD_DIR/$(printf 'bad\x80.json')" "$BAD_SUBDIR/inside.json" \
                "$BAD_DIR/sub/$(printf 's\xe9.py')"; do
  if [ ! -f "$expected" ]; then
    bad_fixture_missing=$((bad_fixture_missing + 1))
    red "  FAIL  fixture not created: $(printf '%q' "$expected")"
  fi
done
if [ "$bad_fixture_missing" -gt 0 ]; then
  failed=$((failed + bad_fixture_missing))
  printf '        Every case below globs this directory, so a missing fixture makes\n'
  printf '        both implementations answer "no file(s) match" and agree about it.\n'
else
  passed=$((passed + 1))
  printf '  ok    the badly-named fixture set was created (9 files)\n'
fi

compare "badly-named files, adopter text"     "$BAD_DIR/*.json"
compare "badly-named files, adopter --json"   --json "$BAD_DIR/*.json"
compare "badly-named files, --source text"    --source "$BAD_DIR/sub/*.py"
compare "badly-named files, --source --json"  --source --json "$BAD_DIR/sub/*.py"
compare "an undecodable DIRECTORY component"  --json "$BAD_DIR/"'*'"/*.json"
compare "recursive ** over badly-named files" --json "$BAD_DIR/**/*.json"

# Patterns that make the MATCHER look at the escaped byte, which `*.json` never
# does — it matches whatever is there and would go green over names the port had
# collapsed to U+FFFD. A `?` has to count one escaped byte as ONE character, and
# a character class has to tell two of them apart. Reverting fnmatch to Go's
# `[]rune` (which turns every WTF-8 surrogate into U+FFFD) leaves every `*` case
# above green and fails exactly these.
compare "one escaped byte is one ? "          --json "$BAD_DIR/x?.json"
compare "a class over escaped bytes"          --json "$BAD_DIR/x[$(printf '\xe9\xff')].json"
compare "a class excluding escaped bytes"     --json "$BAD_DIR/x[!$(printf '\xe9')].json"
# The literal path through the glob (`_glob0`): no magic character at all, so the
# name is resolved by lexists() rather than by the matcher — which is the other
# place a mis-encoded path stops finding its own file.
compare "a literal escaped name, no wildcard" --json "$BAD_DIR/$(printf 'x\xe9.json')"

# The same names typed directly, so the argv leg is exercised without a glob in
# the way. A `--json` position is varied too, because the strip happens before
# the positional dispatch and both operate on the decoded argv.
for badname in 'x\xe9.json' 'x\xff.json' 'bad\x80.json'; do
  literal="$BAD_DIR/$(printf "$badname")"
  compare "literal argv $badname, text"          "$literal"
  compare "literal argv $badname, --json"        --json "$literal"
  compare "literal argv $badname, trailing flag" "$literal" --json
done

# A pattern that matches NOTHING and carries bad bytes. It reaches BOTH renderers
# and lands in two fields at once — `file` and `errors[0]` — and the two spell it
# differently on purpose (repr in text, bare in JSON, no_match at
# bin/validate-intent:551-559), so a decode applied to one and not the other
# shows up here.
for pattern in 'nope\xe2\x82*.json' '\xff*.json' '\xe9literal.json'; do
  missing="$(printf "$pattern")"
  compare "no-match $pattern, text"     "$missing"
  compare "no-match $pattern, --json"   --json "$missing"
  compare "no-match $pattern, --source" --source "$missing"
done

# The path that reaches str(OSError). `pyOSError` renders the filename with
# repr, and the bytes it gets back from the syscall are the RE-ENCODED ones — so
# this is the one message whose whole job is to name the file that failed, and
# the one place a missing decode on the way back out is invisible to every case
# above (they all name files that read fine).
unread_bad="$BAD_DIR/$(printf 'unread\xe9.json')"
cp examples/unit-order-total.json "$unread_bad"
chmod 000 "$unread_bad"
if [ -r "$unread_bad" ]; then
  # A case that could not be SET UP must not be counted as one that passed.
  skipped=$((skipped + 1))
  red "  SKIP  unreadable badly-named file — chmod 000 did nothing (running as root?)"
else
  compare "an unreadable badly-named file, text"   "$unread_bad"
  compare "an unreadable badly-named file, --json" --json "$unread_bad"
  compare "an unreadable badly-named file, glob"   --json "$BAD_DIR/unread*.json"
fi
chmod 644 "$unread_bad"
rm -f "$unread_bad"

# THE `strict` HANDLER REACHES THIS CHANNEL TOO, and the asymmetry between the
# renderers is measured rather than assumed.
#
# A surrogateescaped byte is a lone surrogate, and under `strict` CPython cannot
# encode one for stdout. That was already ratified for stdin and for a `"\udc82"`
# JSON escape — section 15e ("lone surrogates on the way OUT, under BOTH
# handlers") — and a FILENAME is simply a third way to get one there. Measured
# with PYTHONIOENCODING=utf-8 (whose handler is `strict`) over these fixtures:
#
#   text / --source   BOTH sides die, rc 1, identical (truncated) stdout. Only
#                     the stderr prose differs: CPython raises a traceback
#                     naming its own source lines, which is not a surface this
#                     port can reproduce. The ratified pair, asserted below.
#   --json            BYTE-IDENTICAL, and that is not luck: json.dumps'
#                     ensure_ascii=True turns the surrogate into the seven ASCII
#                     characters `\udce2`, which encode fine under any codec. The
#                     compares below pin it, so a renderer that stopped escaping
#                     would fail here rather than quietly joining the ratified
#                     group.
PARITY_ENV="PYTHONIOENCODING=utf-8"
compare "--json survives strict (ensure_ascii escapes the surrogate)" \
  --json "$BAD_DIR/*.json"
compare "--source --json survives strict" \
  --source --json "$BAD_DIR/sub/*.py"
PARITY_ENV=""
assert_encode_failure_in "$REPO_ROOT" "a surrogate from a FILENAME, adopter text" \
  PYTHONIOENCODING=utf-8 "" "$BAD_DIR/*.json"
assert_encode_failure_in "$REPO_ROOT" "a surrogate from a FILENAME, --source text" \
  PYTHONIOENCODING=utf-8 "" --source "$BAD_DIR/sub/*.py"

# assert_distinct_file_keys <label> <want> [args...] — the lossy-collapse guard.
#
# Asserted on the PORT, and cross-checked against the reference, because it is a
# property rather than a byte comparison: N files must produce N DISTINCT `file`
# values. Before the fix this read 1 where python read 6, in two documents of
# the same length. `summary.files` is checked against the same number, so a
# document that loses keys while still counting them cannot pass.
assert_distinct_file_keys() {
  local label="$1" want="$2"
  shift 2
  local go_keys py_keys go_files
  go_keys="$(cd "$REPO_ROOT" && "$GO_BIN" "$@" 2>/dev/null |
    grep -c '^      "file": ' || true)"
  go_files="$(cd "$REPO_ROOT" && "$GO_BIN" "$@" 2>/dev/null |
    grep '^      "file": ' | sort -u | wc -l)"
  py_keys="$(cd "$REPO_ROOT" && "$PYTHON" "$REFERENCE" "$@" 2>/dev/null |
    grep '^      "file": ' | sort -u | wc -l)"

  if [ "$go_files" != "$want" ] || [ "$py_keys" != "$want" ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — wanted $want distinct \"file\" keys, got go=$go_files python=$py_keys"
    printf '        (%s findings emitted; U+FFFD collapses distinct names onto one key)\n' "$go_keys"
    return 1
  fi
  passed=$((passed + 1))
  printf '  ok    %s (%s distinct "file" keys, both sides)\n' "$label" "$want"
  return 0
}

assert_distinct_file_keys "seven badly-named files stay seven keys" 7 \
  --json "$BAD_DIR/*.json"

# And the reverse guard: the fixture set must actually CONTAIN names that a
# naive decode would collapse, or the assertion above is vacuous. Four of the
# seven carry a byte that is not valid UTF-8; a set of clean names would pass
# the distinct-key check under the very implementation it exists to catch.
lossy_names=0
for name in "$BAD_DIR"/*.json; do
  printf '%s' "$(basename "$name")" | iconv -f UTF-8 -t UTF-8 >/dev/null 2>&1 ||
    lossy_names=$((lossy_names + 1))
done
if [ "$lossy_names" -lt 2 ]; then
  failed=$((failed + 1))
  red "  FAIL  the fixture set carries $lossy_names undecodable name(s) — fewer than two"
  printf '        cannot collide, so the distinct-key assertion above proves nothing.\n'
else
  passed=$((passed + 1))
  printf '  ok    %s of the fixture names are undecodable (the collision is reachable)\n' "$lossy_names"
fi

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
# oversight in the prose. `--version` (excluded group 4, slice 6 / SPGD-141) is
# a Go-only surface that SUCCEEDS: exit 0, a line on stdout, nothing on stderr.
# assert_refusal asserts the exact opposite of all three, so it could not be
# stretched to cover it — assert_version_line below is its counterpart, and it
# checks the same three streams with the expectations inverted, plus the one
# thing a refusal never has to prove: that the payload on stdout actually says
# something.
echo
echo "== 16. excluded surfaces refuse loudly (Go only) =="

# How many refusals this section actually asserted. Checked below, because the
# list SHRANK as slices implemented their modes, and a list that shrinks to zero
# would leave assert_refusal defined, never called, and this section reporting a
# clean pass having probed nothing.
refusals_asserted=0

# _refusal_verdict <label> <rc> — grade the run whose output is already in $WORK.
_refusal_verdict() {
  local label="$1" rc="$2"
  refusals_asserted=$((refusals_asserted + 1))
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

assert_refusal() {
  local label="$1"
  shift
  local rc=0
  (cd "$REPO_ROOT" && "$GO_BIN" "$@" </dev/null >"$WORK/go.out" 2>"$WORK/go.err") || rc=$?
  _refusal_verdict "$label" "$rc"
}

# assert_refusal_env <label> <VAR=VALUE> [args...] — the same, for a refusal that
# only fires under a particular environment. The one surviving refusal is of that
# shape: with PYTHONIOENCODING unset the encoding is utf-8 and the handler is
# `surrogateescape`, both of which the port reproduces and therefore does NOT
# refuse.
assert_refusal_env() {
  local label="$1" assignment="$2"
  shift 2
  local rc=0
  (cd "$REPO_ROOT" && env "$assignment" "$GO_BIN" "$@" </dev/null \
     >"$WORK/go.out" 2>"$WORK/go.err") || rc=$?
  _refusal_verdict "$label ($assignment)" "$rc"
}

# Nothing in this list is an unported MODE any more. Slice 3 implemented the last
# two — stdin and adopter `--json` — and slice 4 implemented recursive `**`;
# every entry that used to stand here moved to the assert_not_refused list below,
# which is the point of that list existing.
#
# What remains is the refusal that was never about missing work: a
# PYTHONIOENCODING this port cannot reproduce — EITHER HALF of it. The variable
# is `ENCODING:HANDLER`, and both fields get their own block below:
#
#   HANDLER   `replace` and `ignore` SUCCEED in both directions and each
#             produces different bytes, so answering with an implemented
#             handler's reading would be a confident verdict about a string the
#             reference never saw.
#   ENCODING  `latin-1`, `ascii`, `cp1252`, `utf-16` all carry handler `strict`,
#             which the port DOES reproduce — so the handler block waves every
#             one of them through, and only the encoding block catches them.
#             The port hard-codes UTF-8 on both sides; iso8859-1 decodes bytes
#             UTF-8 rejects, so the reference reaches a different verdict on the
#             same input with the same exit code.
#
# EVERY MODE, not just stdin. The variable governs sys.stdout as well as
# sys.stdin, so an adopter run over a file whose key carries a surrogate escape
# diverges under `replace` with no stdin involved — measured, before the fix:
#
#   python3   ... additional property '?key' is not allowed
#   port      ... additional property '\202key' is not allowed
#
# The refusal used to be scoped to `-`, which left the other three modes
# answering confidently. Each mode is asserted separately here, because a gate
# that reads the mode is a gate that can be re-scoped to one of them again.
assert_refusal_env "unreproducible handler (replace), stdin" \
  PYTHONIOENCODING=:replace -
assert_refusal_env "unreproducible handler (ignore), stdin" \
  PYTHONIOENCODING=utf-8:ignore -
assert_refusal_env "unreproducible handler (replace), adopter" \
  PYTHONIOENCODING=:replace 'examples/*.json'
assert_refusal_env "unreproducible handler (replace), adopter --json" \
  PYTHONIOENCODING=:replace --json 'examples/*.json'
assert_refusal_env "unreproducible handler (replace), --source" \
  PYTHONIOENCODING=:replace --source 'examples/sources/*'
assert_refusal_env "unreproducible handler (replace), self-test" \
  PYTHONIOENCODING=:replace

# The ENCODING half, which is the round-5 fix and the cell this section was
# missing. Every spelling above names `utf-8` or no encoding at all, so the
# refusals above prove only that the port reads the variable's SECOND field.
#
# These codecs all carry handler `strict`, which the port DOES reproduce — so
# the handler gate waves every one of them through, and only the encoding gate
# can catch them. Before the fix the port answered all of them in UTF-8, with
# the same exit code as the reference and a different verdict: section 15f ("the
# ENCODING half of PYTHONIOENCODING") measures the reference actually changing
# its answer, which is what makes these refusals honest rather than merely safe.
#
# EVERY MODE and BOTH RENDERERS, for the same reason as the handler above: a
# gate that reads the mode is a gate that can be re-scoped to one of them again.
assert_refusal_env "unreproducible encoding (latin-1), stdin" \
  PYTHONIOENCODING=latin-1 -
assert_refusal_env "unreproducible encoding (latin-1), stdin --json" \
  PYTHONIOENCODING=latin-1 - --json
assert_refusal_env "unreproducible encoding (latin-1), adopter" \
  PYTHONIOENCODING=latin-1 'examples/*.json'
assert_refusal_env "unreproducible encoding (latin-1), adopter --json" \
  PYTHONIOENCODING=latin-1 --json 'examples/*.json'
assert_refusal_env "unreproducible encoding (latin-1), --source" \
  PYTHONIOENCODING=latin-1 --source 'examples/sources/*'
assert_refusal_env "unreproducible encoding (latin-1), self-test" \
  PYTHONIOENCODING=latin-1
assert_refusal_env "unreproducible encoding (ascii), stdin" \
  PYTHONIOENCODING=ascii -
assert_refusal_env "unreproducible encoding (ascii), stdin --json" \
  PYTHONIOENCODING=ascii - --json
assert_refusal_env "unreproducible encoding (ascii), adopter" \
  PYTHONIOENCODING=ascii 'examples/*.json'
assert_refusal_env "unreproducible encoding (ascii), adopter --json" \
  PYTHONIOENCODING=ascii --json 'examples/*.json'
assert_refusal_env "unreproducible encoding (cp1252), adopter" \
  PYTHONIOENCODING=cp1252 'examples/*.json'
assert_refusal_env "unreproducible encoding (utf-16), stdin --json" \
  PYTHONIOENCODING=utf-16 - --json
# Both fields unreproducible at once. The encoding is named, because it is the
# field that decides which STRING the reference validated where the handler only
# decides how an unrepresentable character in that string is rendered.
assert_refusal_env "both fields unreproducible names the encoding" \
  PYTHONIOENCODING=latin-1:replace -
if grep -q "encoding 'latin-1'" "$WORK/go.err"; then
  passed=$((passed + 1))
  printf '  ok    ...and it names the codec, not the handler: %s\n' "$(head -1 "$WORK/go.err")"
else
  failed=$((failed + 1))
  red "  FAIL  latin-1:replace refused without naming the encoding"
  printf '        got: %s\n' "$(head -1 "$WORK/go.err")"
fi
# An encoding CPython does not know at all. ACKNOWLEDGED NON-PARITY, asserted
# rather than compared: the reference dies in init_stdio_encoding with a fatal
# error and rc 1 before bin/validate-intent's first line runs, and the port exits
# 2 naming the encoding. Neither produces a report, so no consumer is misled.
# What is pinned here is the half that WOULD mislead — before the fix the port
# read `bogus` as "an encoding is named, therefore handler strict, therefore
# reproducible" and printed a clean report where the reference refused to start.
assert_refusal_env "an encoding CPython cannot even start with" \
  PYTHONIOENCODING=bogus -

if [ "$refusals_asserted" -eq 0 ]; then
  failed=$((failed + 1))
  red "  FAIL  this section asserted NO refusals at all"
  printf '        assert_refusal is defined and never called, so the section\n'
  printf '        reported a clean pass having probed nothing. If the last\n'
  printf '        refusal was genuinely retired, delete the section and its\n'
  printf '        entry in the header rather than leaving an empty one.\n'
fi

# --help under a refusing environment, and the two fields answer DIFFERENTLY —
# which is worth pinning rather than assuming, because assuming is how this got
# written wrong the first time.
#
# Under an unreproducible HANDLER, --help is still answered and still compared
# byte-for-byte. A handler only alters characters the codec cannot represent,
# and UTF-8 represents every character in the usage block, so no handler can
# change a byte of it.
#
# Under an unreproducible ENCODING it is REFUSED, because the usage block is not
# ASCII — it carries an em dash (U+2014), copied from the reference's USAGE,
# which the two texts are compared byte-for-byte to keep. Measured on the
# reference itself:
#
#   PYTHONIOENCODING=latin-1   dies, UnicodeEncodeError, rc 1, 0 bytes of stdout
#   PYTHONIOENCODING=utf-16    1644 bytes of UTF-16, against UTF-8's 824
#
# The first draft of the encoding gate sat BELOW the --help check, on the
# inherited premise that "--help is a compile-time ASCII constant no environment
# can alter". Half of that was true and the ASCII half was simply wrong; this
# comparison is what found it.
PARITY_ENV="PYTHONIOENCODING=:replace"
compare_help "--help is unaffected by an unreproducible handler" --help
PARITY_ENV=""
assert_refusal_env "--help IS refused under an unreproducible encoding" \
  PYTHONIOENCODING=latin-1 --help
assert_refusal_env "--help under a codec that merely re-encodes it" \
  PYTHONIOENCODING=utf-16 --help

# The mirror of the list above: the surfaces the port IMPLEMENTED must NOT be
# refused any more. Without this, deleting a mode's dispatch would leave every
# comparison of that mode above unrun and the refusal assertions still green — a
# suite that got smaller without going red. The stdin and adopter-`--json`
# entries are the ones slice 3 moved across, and they are the reason this list
# exists.
assert_not_refused() {
  local label="$1"
  shift
  local rc
  # </dev/null so the stdin-mode entries below cannot block on a terminal.
  (cd "$REPO_ROOT" && "$GO_BIN" "$@" </dev/null >"$WORK/go.out" 2>"$WORK/go.err")
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

# Slice 3's entries. `- --json` and `--json -` are BOTH here because the
# position-independent strip is what routes them, and a dispatch that only
# handled one order would leave the other falling through to adopter mode with
# `-` read as a filename.
assert_not_refused "stdin mode is implemented" -
assert_not_refused "stdin --json is implemented" - --json
assert_not_refused "adopter --json is implemented" --json 'examples/*.json'
assert_not_refused "adopter --json, anywhere on the line" 'examples/*.json' --json

# --------------------------------------------------------------------------- #
# 16b. --version — the excluded surface that SUCCEEDS
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
echo "== 16b. --version reports an identity (Go only) =="

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
#
# PARITY_ENV, if set, is applied — the same convention compare_in and
# compare_stdin follow, and section 17c ("the locale's default codec") needs it:
# the ACCEPTED half of that gate has to be proven for `--version` too, and no
# `compare` can do it because the reference has no such flag. Without the
# variable being honoured here, that assertion would run in the harness's own
# environment and pass whatever the gate did.
assert_version_line_in() {
  local cwd="$1" gobin="$2" label="$3"
  shift 3

  local rc
  (cd "$cwd" && env ${PARITY_ENV:+"$PARITY_ENV"} "$gobin" "$@" \
     >"$WORK/go.out" 2>"$WORK/go.err")
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
# distinction excluded group 2 draws, for the same reason. On this tree
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
# 16c. --schema-source — the second Go-only surface that SUCCEEDS (excluded group 5)
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
echo "== 16c. --schema-source reports the ENFORCED schema (Go only) =="

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
# 16d. excluded group 2 — a tree with no schemas/ directory beside the binary
# --------------------------------------------------------------------------- #
#
# These five argument sets USED to be compared, in section 8 ("OS-level
# failures"), against a tree with no schemas/ directory. Since SPGD-131 the Go
# binary embeds the schema and falls back to it when the file is absent, so it
# no longer fails there and Python still does. That is a real divergence, and it
# is the divergence the change exists to create: a released binary has no repo
# around it. SPGD-315 widened it to the fixture corpus, on the same rule, which
# is why the bare self-test below now succeeds as well.
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
# either: three of the five succeed and two still refuse, and each is asserted
# as what it is. When one of them changes from a refusal to a success the
# assertion is INVERTED here rather than deleted — that is what happened to the
# bare self-test, and the case is the record of it.
echo
echo "== 16d. excluded group 2: no schemas/ beside the binary (Go only) =="

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
  red "  FAIL  excluded group 2 — this tree must have NO schemas/ directory, and it has one"
fi

# And the same for the corpus, since SPGD-315. The bare self-test below passes
# on this tree only because the binary carries examples/ itself; an examples/
# directory appearing here would make it pass by reading one off disk, and the
# case would go on reporting green having stopped testing the fallback. Same
# argument as the line above, for the second embedded asset.
if [ -e "$noschema_root/examples" ]; then
  failed=$((failed + 1))
  red "  FAIL  excluded group 2 — this tree must have NO examples/ directory, and it has one"
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

# The three that now WORK — the release-asset case, and the reason for the change.
# Before SPGD-131 the first two exited 2 with "could not load schema".
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

# The third, and the question this block used to leave open.
#
# It used to assert the opposite, and said why: fixing the schema had not made
# the binary self-contained, because RepoRoot() resolved the fixture CORPUS the
# same executable-relative way and examples/ was not embedded. A bare self-test
# here found no fixtures, said so four times, and exited 1. The comment ended
# "whether a released binary should carry the corpus is a real question and a
# separate slice."
#
# That slice landed (SPGD-315). The corpus is embedded now, on the same
# ABSENT/PRESENT rule as the schema — a real examples/ tree beside the
# executable still wins, and the fallback fires only when there is none — so
# this tree, which has neither schemas/ nor examples/, runs the whole corpus out
# of the binary and exits 0. That is what an adopter's `validate-intent` with no
# arguments now does, and scripts/install.sh runs exactly it from the prefix
# before it will complete an install.
#
# The stdout expectation is the load-bearing half. Exit 0 alone would be
# satisfied by a self-test that had quietly stopped checking anything — the
# empty-fixture guard is what stands between those two, and "12/12" is what says
# the guard did not have to. It is the same summary line a run inside a checkout
# prints, byte for byte, which is the claim the whole change rests on;
# cmd/validate-intent/selftest_embed_test.go compares the two in full.
#
# Parity-safe by construction, as the whole of 16d is: assert_no_schema_tree
# asserts FIRST that python3 still diverges here (exit 2, "could not load
# schema"), then checks the Go binary against explicit expectations. Nothing in
# this case is a byte-for-byte comparison against the reference, so inverting it
# breaks no comparison.
assert_no_schema_tree "bare self-test now WORKS: the corpus is embedded too (was exit 1)" \
  0 "12/12 fixtures matched expectation." EMPTY
assert_no_schema_tree "--source with no argument still refuses" \
  2 EMPTY "--source requires at least one FILE/glob argument" --source

# --------------------------------------------------------------------------- #
# 16e. stderr's error handler — unmodeled, and pinned where it shows
# --------------------------------------------------------------------------- #
#
# Excluded group 7, and the only group in this file that is a measured
# DIVERGENCE rather than a refusal or a Go-only surface. The header states the
# rule; this section states what the port actually does, so "excluded" cannot
# drift into "whatever it does now".
#
# CPython's sys.stderr carries the `backslashreplace` handler by default,
# independently of PYTHONIOENCODING and differently from sys.stdout. This port
# models the stdout encoder (pyioerrors.go, pystdout.go) and has no stderr
# counterpart, which shows in exactly one place: schemaLoadError interpolates
# the schema's origin with a bare `%s`, so an install directory whose name is
# not valid UTF-8 comes out as raw WTF-8 bytes where the reference writes the
# six ASCII characters `\udce9`.
#
# Three claims are pinned here, and the second and third are the ones that stop
# this from being a licence:
#
#   1. THE DIVERGENCE ITSELF, in both directions. The reference must emit the
#      escape and the port must emit the bytes. A port that started agreeing
#      would fail here — which is the point: if someone models stderr later,
#      this section is what tells them the exclusion can be retired, instead of
#      leaving a stale paragraph in the header claiming a divergence that no
#      longer exists.
#
#   2. THE HALF THAT IS ALREADY CORRECT. The `[Errno ...]` clause on that same
#      line goes through pyOSError -> PyReprString and IS byte-identical. The
#      unreadable case prints both renderings on ONE line of the port's output,
#      which is the clearest available statement of where the boundary runs, and
#      asserting it keeps a future "simplification" of pyOSError from quietly
#      widening the group.
#
#   3. THE BLAST RADIUS. Everything else on a badly-named install tree is
#      COMPARED, not excluded: adopter, adopter --json, --source, --source
#      --json, stdin, self-test and --help all run from a directory the port
#      cannot name in ASCII, and all must be byte-identical. Without these the
#      exclusion would read as "the port is untested under a badly-named install
#      prefix", which is a much larger claim than the one being made.
echo
echo "== 16e. stderr's error handler (excluded group 7) =="

BADINSTALL="$WORK/badinstall/$(printf 'd\xe9')"
mkdir -p "$BADINSTALL/bin" "$BADINSTALL/schemas"
cp "$REFERENCE" "$BADINSTALL/bin/validate-intent"
cp "$GO_BIN" "$BADINSTALL/bin/validate-intent-go"
BADINSTALL_PY="$BADINSTALL/bin/validate-intent"
BADINSTALL_GO="$BADINSTALL/bin/validate-intent-go"

# NON-VACUITY, first. Every claim below is about a path CPython has to
# surrogate-escape. If the directory name were decodable — a tmpdir that
# sanitised it, a filesystem that rejected the byte — the comparisons would
# still pass and would prove nothing at all, which is the failure mode this
# repo keeps re-finding. So the premise is checked before it is used.
if "$PYTHON" - "$BADINSTALL" <<'PROBE'
import os, sys
raw = os.fsencode(sys.argv[1])
sys.exit(0 if raw.decode("utf-8", "surrogateescape") != raw.decode("utf-8", "replace") else 1)
PROBE
then
  passed=$((passed + 1))
  printf '  ok    the install directory name is genuinely undecodable\n'
else
  failed=$((failed + 1))
  red "  FAIL  the install directory name decodes cleanly — this section proves nothing"
fi

# assert_stderr_bytes <label> <file> <pattern>... — every pattern must appear in
# <file>, matched as raw BYTES (grep -F under LC_ALL=C, because two of these
# patterns are not valid UTF-8 and a locale-aware grep may refuse them).
assert_stderr_bytes() {
  local label="$1" file="$2"
  shift 2
  local missing=() pat
  for pat in "$@"; do
    LC_ALL=C grep -qF -- "$pat" "$file" || missing+=("$pat")
  done
  if [ ${#missing[@]} -eq 0 ]; then
    passed=$((passed + 1))
    printf '  ok    %s\n' "$label"
    return 0
  fi
  failed=$((failed + 1))
  red "  FAIL  $label"
  for pat in "${missing[@]}"; do
    printf '        missing from stderr: %s\n' "$(printf '%s' "$pat" | cat -v)"
  done
  printf '        stderr was: %s\n' "$(cat -v "$file")"
  return 1
}

# The two renderings of the same directory name, built here rather than written
# as literals so the relationship between them is visible: one is what
# backslashreplace produces from U+DCE9, the other is U+DCE9 in WTF-8.
BADINSTALL_ESCAPED='d\udce9'
BADINSTALL_RAW="$(printf 'd\xed\xb3\xa9')"

# (1) THE DIVERGENCE. A schema that is PRESENT and malformed, so both sides
# reach the diagnostic rather than the embedded fallback (which fires on ENOENT
# only — see LoadSchema).
printf '{ this is not json' > "$BADINSTALL/schemas/open-test-intent.v1.json"
badinstall_py_rc=0
(cd "$REPO_ROOT" && "$PYTHON" "$BADINSTALL_PY" 'examples/*.json' \
   </dev/null >"$WORK/bi.py.out" 2>"$WORK/bi.py.err") || badinstall_py_rc=$?
badinstall_go_rc=0
(cd "$REPO_ROOT" && "$BADINSTALL_GO" 'examples/*.json' \
   </dev/null >"$WORK/bi.go.out" 2>"$WORK/bi.go.err") || badinstall_go_rc=$?

if [ "$badinstall_py_rc" = 2 ] && [ "$badinstall_go_rc" = 2 ]; then
  passed=$((passed + 1))
  printf '  ok    both implementations still exit 2 on the unloadable schema\n'
else
  failed=$((failed + 1))
  red "  FAIL  exit codes diverge: python=$badinstall_py_rc go=$badinstall_go_rc"
fi
assert_stderr_bytes "the reference backslashreplaces the path (\\udce9)" \
  "$WORK/bi.py.err" "$BADINSTALL_ESCAPED"
assert_stderr_bytes "the port writes the raw WTF-8 bytes — group 7, declared" \
  "$WORK/bi.go.err" "$BADINSTALL_RAW"
if cmp -s "$WORK/bi.py.err" "$WORK/bi.go.err"; then
  failed=$((failed + 1))
  red "  FAIL  the two stderr streams now AGREE — group 7 is stale, retire it"
else
  passed=$((passed + 1))
  printf '  ok    the two stderr streams differ, and only here\n'
fi

# (2) THE HALF THAT IS ALREADY CORRECT. A schema that EXISTS and cannot be read
# — a directory in the file's place, which is EISDIR for root and non-root alike
# (chmod 000 does nothing when this suite runs as root, and a case that SKIPs is
# not a case). The port's one line then carries both renderings: the `%s` half
# raw, the pyOSError half escaped exactly as the reference writes it.
rm -f "$BADINSTALL/schemas/open-test-intent.v1.json"
mkdir -p "$BADINSTALL/schemas/open-test-intent.v1.json"
(cd "$REPO_ROOT" && "$PYTHON" "$BADINSTALL_PY" 'examples/*.json' \
   </dev/null >"$WORK/bi2.py.out" 2>"$WORK/bi2.py.err") || true
(cd "$REPO_ROOT" && "$BADINSTALL_GO" 'examples/*.json' \
   </dev/null >"$WORK/bi2.go.out" 2>"$WORK/bi2.go.err") || true
assert_stderr_bytes "the reference's [Errno] clause reprs the path" \
  "$WORK/bi2.py.err" "[Errno 21] Is a directory: '" "$BADINSTALL_ESCAPED"
assert_stderr_bytes "the port's [Errno] clause reprs it IDENTICALLY, on a line whose %s half does not" \
  "$WORK/bi2.go.err" "[Errno 21] Is a directory: '" "$BADINSTALL_ESCAPED" "$BADINSTALL_RAW"
rmdir "$BADINSTALL/schemas/open-test-intent.v1.json"

# (3) THE BLAST RADIUS. With a real schema in place, every mode is compared
# byte-for-byte from the same badly-named prefix. These are COMPARISONS: the
# exclusion covers one diagnostic, and this is what says so in a form that can
# go red.
cp "$REPO_ROOT/schemas/open-test-intent.v1.json" "$BADINSTALL/schemas/"
compare_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: adopter" 'examples/*.json'
compare_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: adopter --json" --json 'examples/invalid/*.json'
compare_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: --source" --source 'examples/sources/*.rb'
compare_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: --source --json" --source --json 'examples/sources/*.rb'
compare_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: a no-match diagnostic" 'nope*.json'
# The two modes that resolve something ELSE from the executable's own path: the
# self-test finds its corpus there, and --help does not touch the path at all.
# Both are here because every self-test line names a path RELATIVE to that root,
# and a port that leaked the undecoded root into one would fail here and nowhere
# above.
#
# The corpus is staged into the badly-named prefix rather than left absent. It
# used to be absent, and the case compared four "no fixtures match" diagnostics
# — but since SPGD-315 an absent tree makes the port fall back to its embedded
# copy while Python still reports nothing, so an absent tree here would compare
# a divergence. Staging it keeps the comparison AND strengthens it: with twelve
# fixtures present both implementations print twenty-five lines whose paths were
# each derived by relativising against an undecodable root, which is far more of
# that round trip than four pattern names ever exercised.
mkdir -p "$BADINSTALL/examples/invalid" "$BADINSTALL/examples/sources/invalid"
cp "$REPO_ROOT"/examples/*.json "$BADINSTALL/examples/"
cp "$REPO_ROOT"/examples/invalid/*.json "$BADINSTALL/examples/invalid/"
cp "$REPO_ROOT"/examples/sources/*.* "$BADINSTALL/examples/sources/" 2>/dev/null
cp "$REPO_ROOT"/examples/sources/invalid/* "$BADINSTALL/examples/sources/invalid/"
compare_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: self-test names its corpus, identically" 
compare_help_in "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: --help" --help
compare_stdin_in "$REPO_ROOT" "$BADINSTALL_PY" "$BADINSTALL_GO" \
  "badly-named install prefix: stdin --json" "examples/unit-order-total.json" - --json

# --------------------------------------------------------------------------- #
# 17. Go-side refusals — schemas carrying a pattern RE2 cannot reproduce
# --------------------------------------------------------------------------- #
#
# The other half of excluded group 1. Each case builds a tree whose schema
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
echo "== 17. schemas the port refuses (Go only) =="
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
# 17b. truncation sweep — every prefix of a \uXXXX-bearing document
# --------------------------------------------------------------------------- #
#
# WHY A SWEEP AND NOT A HANDFUL OF POINT CASES
#
# A real parity bug lived in this class and the 228 hand-picked cases above did
# not see it. CPython's C scanner refuses to decode a \uXXXX escape unless a
# character FOLLOWS the four hex digits, so an escape ending exactly at
# end-of-document raises `Invalid \uXXXX escape` at the 'u' — it never reaches
# the "unterminated" path. The port's bound was one character short, so it
# decoded the escape, ran off the end, and reported `Unterminated string` at the
# opening quote instead: wrong message AND wrong offset, which is a different
# errors[0] in the --json document machine consumers read.
#
# The fix was one character in decodeUXXXX (cmd/validate-intent/pyjson.go). A
# point case for the exact reported input would go green while leaving the rest
# of the class unguarded — the escape nested inside an object, the low half of a
# surrogate pair, the escape whose hex digits are not hex. So what is pinned
# here is the CLASS: every prefix of each document below, through all three
# input paths. Truncating one character at a time walks the scanner into every
# partial-token state it has, which is exactly how the original was found.
#
# Scope note: these documents are deliberately pure ASCII, so a byte prefix and
# a character prefix are the same thing and `${doc:0:i}` is unambiguous.
# Truncated multi-byte UTF-8 at EOF is a different failure — a DECODE error,
# raised before the parser ever runs — and is the job of section 15b
# ("stdin mode (`-`) — text and --json"). Mixing the two
# would test neither cleanly.
echo
echo "== 17b. truncation sweep: every prefix of a \\uXXXX-bearing document =="

SWEEP_DIR="$WORK/sweep"
mkdir -p "$SWEEP_DIR"

# sweep_prefixes <label> <document> <leg>
#
# Compares every prefix (length 1..N) and books ONE harness case per
# document/leg pair. A per-prefix "ok" line would bury the rest of the run in
# several hundred lines of noise, and would also let a single document inflate
# the headline count until "N/N passed" stopped meaning anything.
#
# On failure it names how many prefixes diverged and the first one, then replays
# that prefix through the ordinary comparison helper so the real diff is
# printed. The replay books its own pass/fail, so the counters are saved and
# restored around it — otherwise one logical failure would be counted twice.
#
# A sweep that compared nothing is a failure, not a pass: the prefix count is
# asserted non-zero. That is this project's own vacuous-green rule (SPGD-78)
# applied to the harness — "nothing to check" must not read as "all clear".
sweep_prefixes() {
  local label="$1" doc="$2" leg="$3"
  local n=${#doc}
  local i swept=0 bad=0 first_bad=""
  local py_rc go_rc

  for (( i = 1; i <= n; i++ )); do
    printf '%s' "${doc:0:i}" > "$SWEEP_DIR/prefix.json"
    case "$leg" in
      stdin-text)
        (cd "$REPO_ROOT" && "$PYTHON" "$REFERENCE" - \
           <"$SWEEP_DIR/prefix.json" >"$WORK/py.out" 2>"$WORK/py.err"); py_rc=$?
        (cd "$REPO_ROOT" && "$GO_BIN" - \
           <"$SWEEP_DIR/prefix.json" >"$WORK/go.out" 2>"$WORK/go.err"); go_rc=$?
        ;;
      stdin-json)
        (cd "$REPO_ROOT" && "$PYTHON" "$REFERENCE" - --json \
           <"$SWEEP_DIR/prefix.json" >"$WORK/py.out" 2>"$WORK/py.err"); py_rc=$?
        (cd "$REPO_ROOT" && "$GO_BIN" - --json \
           <"$SWEEP_DIR/prefix.json" >"$WORK/go.out" 2>"$WORK/go.err"); go_rc=$?
        ;;
      file)
        # Adopter mode over the same bytes on disk. A parse failure travels a
        # different path here (check_file, not run_stdin) and is rendered per
        # file with its own prefix, so the escape bound has to be right in both.
        (cd "$SWEEP_DIR" && "$PYTHON" "$REFERENCE" prefix.json \
           >"$WORK/py.out" 2>"$WORK/py.err"); py_rc=$?
        (cd "$SWEEP_DIR" && "$GO_BIN" prefix.json \
           >"$WORK/go.out" 2>"$WORK/go.err"); go_rc=$?
        ;;
      *)
        failed=$((failed + 1))
        red "  FAIL  $label — unknown sweep leg '$leg'"
        return 1
        ;;
    esac

    swept=$((swept + 1))
    if [ "$py_rc" != "$go_rc" ] \
       || ! cmp -s "$WORK/py.out" "$WORK/go.out" \
       || ! cmp -s "$WORK/py.err" "$WORK/go.err"; then
      bad=$((bad + 1))
      [ -z "$first_bad" ] && first_bad="$i"
    fi
  done

  if [ "$swept" -eq 0 ]; then
    failed=$((failed + 1))
    red "  FAIL  $label — swept 0 prefixes, so this case verified nothing"
    return 1
  fi

  if [ "$bad" -eq 0 ]; then
    passed=$((passed + 1))
    printf '  ok    %s (%d prefixes)\n' "$label" "$swept"
    return 0
  fi

  failed=$((failed + 1))
  red "  FAIL  $label — $bad/$swept prefixes diverged, first at length $first_bad"
  printf '        prefix: %s\n' "${doc:0:first_bad}"
  printf '%s' "${doc:0:first_bad}" > "$SWEEP_DIR/prefix.json"

  local saved_passed=$passed saved_failed=$failed
  case "$leg" in
    stdin-text) compare_stdin "(replay) $label" "$SWEEP_DIR/prefix.json" - ;;
    stdin-json) compare_stdin "(replay) $label" "$SWEEP_DIR/prefix.json" - --json ;;
    file)       compare_in "$SWEEP_DIR" "$REFERENCE" "$GO_BIN" \
                  "(replay) $label" prefix.json ;;
  esac
  passed=$saved_passed
  failed=$saved_failed
  return 1
}

# Each document targets a distinct corner of the class. They are single-quoted
# so the backslashes stay literal, and they are written with `printf '%s'`
# rather than `printf "$doc"` — bash's own printf decodes \uXXXX, which would
# quietly feed the sweep the DECODED character and test nothing.
sweep_docs=(
  # the reported bug: a plain BMP escape inside a value, truncated mid-document.
  # `layer` is an enum, so once it parses the decoded value is echoed back in the
  # error prose — a mis-decode changes the message, not just the offset.
  'plain-bmp|{"layer":"a\u0041b","entity":"ab"}'
  # a surrogate PAIR. The bound governs the low half independently of the high
  # one, and the pair is combined only when the second escape decodes.
  'surrogate-pair|{"layer":"\ud800\udc00"}'
  # a lone high surrogate followed by an ordinary character: it decodes, does
  # not combine, and must still reach the unterminated path at the right offset.
  'lone-surrogate|{"layer":"\ud800x"}'
  # an escape that decodes INTO a valid enum member, so a wrong decode flips the
  # schema verdict rather than only the error text.
  'escape-to-enum|{"layer":"\u0075nit","entity":"ab"}'
  # nested, and with hex digits that are not hex — the other route to the same
  # message, reported at the same offset.
  'nested-bad-hex|["\u0041",{"k":"\uZZZZ"}]'
  # a document that is fully VALID once complete, so the sweep also walks the
  # success path out the far end instead of only comparing error prose.
  'valid-tail|{"entity":"\u004Fx","action":"go","behavior":"abcdefghijklmno","layer":"unit"}'
)

for entry in "${sweep_docs[@]}"; do
  sweep_prefixes "sweep stdin text: ${entry%%|*}" "${entry#*|}" stdin-text
  sweep_prefixes "sweep stdin json: ${entry%%|*}" "${entry#*|}" stdin-json
  sweep_prefixes "sweep file:       ${entry%%|*}" "${entry#*|}" file
done

# --------------------------------------------------------------------------- #
# 17c. the locale's default codec
# --------------------------------------------------------------------------- #
#
# The THIRD variable in the PYTHONIOENCODING family, and the one nothing here
# was looking at. CPython derives sys.getfilesystemencoding() — which decodes
# argv and every directory listing — AND sys.std*.encoding, whenever
# PYTHONIOENCODING names no codec, from a single default codec that the LOCALE
# picks. Two claims the port relied on turn out to be properties of this
# container rather than of CPython:
#
#   pyioerrors.go   "an empty PYTHONIOENCODING encoding field means the locale
#                    default, and the locale default is utf-8"
#   pyfspath.go     "the filesystem encoding is utf-8/surrogateescape"
#
# Both are false under `PYTHONUTF8=0 LC_ALL=C`, where all three become `ascii`
# with nothing in PYTHONIOENCODING set at all — so the gate in section 15f
# ("the ENCODING half of PYTHONIOENCODING") cannot see it. Measured:
# `python3 bin/validate-intent --help` DIES there (UnicodeEncodeError on the
# usage block's em dash, 0 bytes, rc 1) where the port printed 824 clean bytes
# and exited 0.
#
# The port refuses those environments (cmd/validate-intent/pylocale.go). This
# section pins the same two things section 15f ("the ENCODING half of
# PYTHONIOENCODING") pins for its own variable:
#
#   1. THE PREMISE, python3 against python3 — the environments this refuses must
#      genuinely change the reference's own answer, or the refusal has become
#      over-refusal and nothing else here would notice; and
#   2. THE OVER-REFUSAL DIRECTION — the environments it accepts are still
#      compared byte-for-byte, including the C locale and several spellings of a
#      UTF-8 one, so closing the hole did not close it too far.
#
# The gate is a WHITELIST and is knowingly wider than CPython's rule, because
# CPython's rule is libc's `nl_langinfo(CODESET)` for a locale that may or may
# not be installed — unanswerable from a cgo-free Go binary. It therefore
# refuses some environments CPython would have answered (`PYTHONUTF8=0` with a
# real UTF-8 locale is the main one). Those are visible exit 2s, which is the
# direction this gate is allowed to be wrong in;
# cmd/validate-intent/pylocale_test.go runs the same matrix against python3 and
# fails on an UNDER-refusal only.
echo
echo "== 17c. the locale's default codec =="

# assert_refusal_envs <label> <"VAR=V VAR=V ..."> [args...] — a refusal that
# needs SEVERAL variables. PYTHONUTF8 alone does not select a non-UTF-8 codec; it
# hands the choice to the locale, so the halves have to be set together to reach
# the row that matters. The assignments are deliberately word-split (no locale
# name or codec name here contains a space).
#
# `env -i` — a PRISTINE environment, not the harness's own. This is the one place
# in the file where inheriting the caller's LANG changes the answer rather than
# merely being untidy: PEP 538's C-locale coercion is skipped when LC_ALL is set
# and applied otherwise, so `LC_CTYPE=C` on top of an ambient `LANG=C.UTF-8`
# resolves to utf-8 while the same variable in an empty environment does not.
# PATH is restored because python3 and the port both need it; it carries no
# codec.
assert_refusal_envs() {
  local label="$1" assignments="$2"
  shift 2
  local rc=0
  # shellcheck disable=SC2086
  (cd "$REPO_ROOT" && env -i PATH="$PATH" $assignments "$GO_BIN" "$@" </dev/null \
     >"$WORK/go.out" 2>"$WORK/go.err") || rc=$?
  _refusal_verdict "$label ($assignments)" "$rc"
}

# (1) THE PREMISE. Ask python3 what its default codec actually is in each
# environment this refuses, and require that it is NOT utf-8 — i.e. that the
# refusal is declining a real divergence rather than a hypothetical one.
#
# os.write to the raw fd rather than print(), for the same reason as section 15f
# ("the ENCODING half of PYTHONIOENCODING"): the probe runs under the very codec
# it is reporting.
# The three rows below are the environments this section then asserts a refusal
# for. A fourth shape — `PYTHONUTF8=0 LC_CTYPE=C` with no PYTHONCOERCECLOCALE —
# is deliberately NOT here, and finding out why is what these rows are for: PEP
# 538's C-locale coercion rewrites LC_CTYPE to C.UTF-8, and it is SKIPPED when
# LC_ALL is set and applied otherwise. So `LC_ALL=C` gives ascii while the same
# locale named through LC_CTYPE or LANG gives utf-8. The port refuses both (it
# refuses PYTHONUTF8=0 outright), which makes the second an OVER-refusal — the
# direction this gate is allowed to be wrong in, and one this premise loop must
# not claim is a real divergence.
locale_premise_checked=0
for assignments in "PYTHONUTF8=0 LC_ALL=C" \
                   "PYTHONUTF8=0 LC_ALL=POSIX" \
                   "PYTHONUTF8=0 PYTHONCOERCECLOCALE=0 LC_CTYPE=C"; do
  # shellcheck disable=SC2086
  read -r fs_enc fs_err out_enc <<<"$(env -i PATH="$PATH" $assignments "$PYTHON" -c \
    'import os, sys; os.write(1, ("%s %s %s\n" % (sys.getfilesystemencoding(), sys.getfilesystemencodeerrors(), sys.stdout.encoding)).encode("ascii"))' </dev/null)"
  locale_premise_checked=$((locale_premise_checked + 1))
  if [ "$fs_enc" = "utf-8" ] && [ "$out_enc" = "utf-8" ] && [ "$fs_err" = "surrogateescape" ]; then
    failed=$((failed + 1))
    red "  FAIL  $assignments — CPython's default codec IS utf-8 here"
    printf '        cmd/validate-intent/pylocale.go refuses this environment. If the\n'
    printf '        reference no longer diverges in it, the refusal has outlived its\n'
    printf '        reason and has become pure over-refusal.\n'
  else
    passed=$((passed + 1))
    printf '  ok    %s makes CPython use %s/%s (stdout %s) — the refusal is real\n' \
      "$assignments" "$fs_enc" "$fs_err" "$out_enc"
  fi
done
if [ "$locale_premise_checked" -eq 0 ]; then
  failed=$((failed + 1))
  red "  FAIL  the premise loop probed no environments — this section proved nothing"
fi

# The reference genuinely dies on --help there, which is the sharpest statement
# of "these are not the same run". Asserted rather than described, because it is
# the measurement that decided the gate sits ABOVE the --help check in main.go.
help_rc=0
(env -i PATH="$PATH" PYTHONUTF8=0 LC_ALL=C \
   "$PYTHON" "$REFERENCE" --help >"$WORK/py.out" 2>"$WORK/py.err") || help_rc=$?
if [ "$help_rc" -eq 0 ] || [ -s "$WORK/py.out" ]; then
  failed=$((failed + 1))
  red "  FAIL  PYTHONUTF8=0 LC_ALL=C: the reference answered --help (rc $help_rc, $(wc -c <"$WORK/py.out") bytes)"
  printf '        main.go puts the locale gate ABOVE its --help check on the measurement\n'
  printf '        that it does NOT. If that changed, the ordering should be revisited.\n'
else
  passed=$((passed + 1))
  printf '  ok    PYTHONUTF8=0 LC_ALL=C kills the reference on --help (rc %s, 0 bytes)\n' "$help_rc"
fi

# (2) THE REFUSAL, in every mode and both renderers. A gate that reads the mode
# is a gate that can be re-scoped to one of them again — which is exactly what
# happened to the handler gate in round 4.
assert_refusal_envs "non-UTF-8 locale, stdin" \
  "PYTHONUTF8=0 LC_ALL=C" -
assert_refusal_envs "non-UTF-8 locale, stdin --json" \
  "PYTHONUTF8=0 LC_ALL=C" - --json
assert_refusal_envs "non-UTF-8 locale, adopter" \
  "PYTHONUTF8=0 LC_ALL=C" 'examples/*.json'
assert_refusal_envs "non-UTF-8 locale, adopter --json" \
  "PYTHONUTF8=0 LC_ALL=POSIX" --json 'examples/*.json'
assert_refusal_envs "non-UTF-8 locale, --source" \
  "PYTHONUTF8=0 PYTHONCOERCECLOCALE=0 LC_CTYPE=C" --source 'examples/sources/*'
assert_refusal_envs "non-UTF-8 locale, self-test" \
  "PYTHONUTF8=0 PYTHONCOERCECLOCALE=0 LANG=C"
assert_refusal_envs "non-UTF-8 locale, --help" \
  "PYTHONUTF8=0 LC_ALL=C" --help
# --version too, and this one is the case that would go red if someone hoisted
# it above the gates. It is the ONE surface with no reference behaviour at all
# (excluded group 4), so the argument for answering it here is real and is
# written out at its call site in main.go; the decision is that these gates
# refuse the PROCESS rather than a mode, so nothing answers from inside an
# environment the port has just declared unreproducible. Pinned so that changing
# it has to be a deliberate edit with its own reason.
assert_refusal_envs "non-UTF-8 locale, --version" \
  "PYTHONUTF8=0 LC_ALL=C" --version
assert_refusal_env "unreproducible encoding (latin-1), --version" \
  PYTHONIOENCODING=latin-1 --version
assert_refusal_env "a locale naming a non-UTF-8 codeset" \
  LC_ALL=en_US.ISO-8859-1 'examples/*.json'
assert_refusal_env "a locale naming no codeset at all" \
  LC_ALL=en_US 'examples/*.json'
# CPython refuses to START on this one (rc 1, before the reference's first
# line). An acknowledged NON-parity, the same shape as PYTHONIOENCODING=bogus:
# neither side produces a report, so no consumer is handed a wrong answer.
assert_refusal_env "a PYTHONUTF8 value CPython will not start on" \
  PYTHONUTF8=2 'examples/*.json'

# (3) THE OVER-REFUSAL DIRECTION. Everything the gate accepts is still compared
# byte-for-byte. Without these, tightening the gate to "refuse everything" would
# leave every assertion above green.
for locale_spec in "LC_ALL=C" "LC_ALL=POSIX" "LC_ALL=C.UTF-8" "LC_ALL=C.utf8" \
                   "LANG=C.UTF-8" "LC_CTYPE=C.UTF-8" "PYTHONUTF8=1"; do
  PARITY_ENV="$locale_spec"
  compare "$locale_spec is answered, not refused (adopter --json)" \
    --json 'examples/invalid/*.json'
  compare_help "$locale_spec is answered, not refused (--help)" --help
  # ...and the Go-only flag, which no comparison can cover: the reference reads
  # `--version` as a filename. Without this the accepted half of the gate would
  # be proven only for surfaces the reference also has, and a gate that had
  # tightened to refuse `--version` everywhere would stay green. PARITY_ENV is
  # still set here, deliberately — that is the whole assertion.
  assert_version_line_in "$REPO_ROOT" "$GO_BIN" \
    "$locale_spec answers --version" --version
  PARITY_ENV=""
done
# PYTHONUTF8=1 with a locale that would otherwise be refused: UTF-8 mode wins,
# and the port must follow it rather than reading the locale it overrode.
PARITY_ENV="PYTHONUTF8=1"
compare "UTF-8 mode forced on is answered (badly-named files, --json)" \
  --json "$BAD_DIR/*.json"
PARITY_ENV=""

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
echo "== 18. the oracle is unmodified =="
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
echo "== 19. the Ruby leg (specguard-lint vs. the port) =="
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
