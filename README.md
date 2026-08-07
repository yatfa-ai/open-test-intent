# OpenTestIntent

> A tiny, language-agnostic annotation that declares *what a test verifies*. Open and
> vendor-neutral — any tool can read it, not just [SpecGuard](https://github.com/yatfa-ai/specguard).

A test that carries an `@intent` declares its subject (`entity`), operation (`action`), expected
outcome (`behavior`), and test `layer`. The annotation is a single comment line:

```ruby
# @intent: { entity: "Order", action: "checkout", "behavior": "returns 402 payment required on expired card", layer: "request" }
```

## What's here

The canonical home of the protocol:

- `schemas/open-test-intent.v1.json` — the JSON Schema (draft-07) every annotation validates against.
- The human-readable protocol specification.
- Worked examples by layer (`unit` / `integration` / `request` / `system`).

SpecGuard is the **reference consumer**; this repo is intentionally SpecGuard-independent so other
test frameworks — pytest, jest, Go, … — can adopt the same annotation without coupling to the
SpecGuard platform.

## How to validate

The repo ships a zero-dependency validator (`bin/validate-intent` — Python 3 standard library only,
nothing to install) that checks an annotation against `schemas/open-test-intent.v1.json`, either as
parsed JSON or straight out of your test source.

```sh
# Self-test the in-repo fixtures (no arguments): every examples/*.json must pass
# and every examples/invalid/*.json must fail, and every @intent in
# examples/sources/* must pass while every one in examples/sources/invalid/*
# must fail. Exits 0 only if all match.
./bin/validate-intent

# Validate a single annotation from stdin (the `-` sentinel) — the programmatic
# path for scripts and AI agents. Prints PASS/FAIL and exits 0/1 accordingly.
echo '{ "entity": "Order", "action": "checkout", "behavior": "returns 402 on expired card", "layer": "request" }' \
  | ./bin/validate-intent -

# Validate your own annotation file(s) or glob — exits 0 if every file conforms,
# non-zero with the specific violated rule otherwise.
./bin/validate-intent path/to/intent.json
./bin/validate-intent 'specs/**/*.json'

# Validate @intent annotations *in place* inside test source files (--source,
# or -s). Each finding is reported at its file:line — the location the other
# modes can't give you. Exits 0 if every annotation found conforms.
./bin/validate-intent --source spec/models/order_spec.rb
./bin/validate-intent --source 'spec/**/*_spec.rb' 'tests/**/*.py'

# Any of the three above, as one JSON document on stdout instead of prose
# (--json goes anywhere on the line). Same checks, same exit code.
./bin/validate-intent --json --source 'spec/**/*_spec.rb'
```

The first three paths take **strict JSON**. `--source` is the one that reads the
annotation as you actually write it — it finds each `@intent:` token, captures the
object literal that follows it, and normalizes the protocol's permissive syntax
(unquoted keys, single-quoted strings, arbitrary whitespace — see
[Equivalent forms](PROTOCOL.md#equivalent-forms)) into strict JSON before validating.
Capture is string-aware, so a brace or apostrophe inside a `behavior` sentence is safe.

```
$ ./bin/validate-intent --source spec/models/order_spec.rb
PASS  spec/models/order_spec.rb:10
FAIL  spec/models/order_spec.rb:24
        -> <root>: additional property 'entiity' is not allowed
```

A source file with no annotations is reported and skipped, not failed — the protocol
treats unannotated tests as legitimate. But an `@intent:` token whose payload can't be
captured (unbalanced or missing braces, or spread across lines — annotations are
single-line) **is** reported, so a typo'd annotation fails loudly instead of silently
counting as "unannotated".

Worked source fixtures exercising all three equivalent forms live in
[`examples/sources/`](examples/sources).

## Machine-readable output (`--json`)

The human report is for humans. `--json` emits **one JSON document on stdout** instead,
for the three adopter-facing modes — stdin (`-`), `FILE...`, and `--source` — so a
script, a CI job, or an AI agent gets *which file, which line, which rule* as data
rather than having to parse prose. The flag can go anywhere on the command line.

```sh
$ ./bin/validate-intent --json --source spec/models/order_spec.rb
```
```json
{
  "schema": "open-test-intent.v1.json",
  "mode": "source",
  "ok": false,
  "summary": { "files": 1, "annotations": 2, "failed": 2 },
  "findings": [
    { "file": "spec/models/order_spec.rb", "line": 24, "ok": false, "kind": "schema",
      "errors": ["<root>: additional property 'entiity' is not allowed"] },
    { "file": "spec/models/order_spec.rb", "line": 31, "ok": false, "kind": "extraction",
      "errors": ["unterminated object literal (an annotation must fit on one line)"] }
  ]
}
```

Every finding has the same five keys in every mode, so a consumer never branches on
which mode produced it (the envelope's `mode` is `"stdin"`, `"adopter"` or `"source"`;
`ok` mirrors the exit code, and `summary` counts what was checked):

`summary.annotations` counts **annotation sites examined**, the same way in every mode: a
site whose payload could not be captured or parsed still counts (it was there, it was
bad), while input that could not be read contributes none — an unreadable file, or a
stdin stream whose bytes never decoded. In `FILE...` mode each file is one site.

| field    | meaning |
| -------- | ------- |
| `file`   | the path checked — or `"-"` for stdin, or the pattern itself for a `no-match` |
| `line`   | the annotation's line number; `null` where the finding is not line-scoped (stdin, `FILE...`, `no-match`) |
| `ok`     | whether this finding passed |
| `kind`   | *how* it failed; `null` when it passed |
| `errors` | every violated rule (or the single problem), always a list of strings |

`kind` is the field that makes the two visually different failure shapes of the text
report distinguishable as data:

| `kind` | means |
| ------ | ----- |
| `schema` | parsed fine, violated `schemas/open-test-intent.v1.json` |
| `extraction` | an `@intent:` token whose object literal could not be captured (missing/unbalanced braces, spread across lines) |
| `parse` | anything that reached the JSON parser and was rejected by it — a captured payload, a stdin document, or a `FILE...` argument |
| `read` | the input could not be read at all (missing, unreadable, or not UTF-8 — including undecodable bytes on stdin, which never reach the parser) |
| `no-match` | a path/glob argument that matched no file |

A given failure gets the **same `kind` in every mode**: malformed JSON is `parse` whether
it arrived via stdin, a `--source` annotation, or a named file, and mis-encoded bytes are
`read` whether they arrived as a file or on stdin — so a consumer branching on `kind`
never has to know which mode produced the finding. `parse` and `read` are worth
telling apart because they route differently — `parse` is the author's file to fix, `read`
is the checkout or the encoding.

Two things worth knowing:

- **A pattern matching nothing is a `no-match` finding on stdout**, not only the stderr
  line the text mode prints. Without it, a stdout-only consumer would see a clean pass
  list next to an unexplained non-zero exit.
- **Exit codes are identical with and without the flag**, and the default (non-`--json`)
  output is unchanged — `--json` is a second renderer over the same checks, not a second
  code path. Self-test mode has no `--json` form: it is the in-repo fixture harness
  rather than an adopter surface, so asking for one is a usage error (exit `2`) rather
  than a silent fallback to prose.

## Running the tests

The validator's own regression suite is zero-dependency too — stdlib `unittest`, no
pytest, no `requirements.txt`. It covers the pure validation core (`validate` and its
type helpers), including the draft-07 keywords the shipped schema does not yet use.

```sh
python3 tests/test_validate_intent.py     # or: python3 -m unittest discover -s tests
```

Note that `./bin/validate-intent` (self-test mode) and this suite check different things:
the self-test verifies the **fixtures** still match their expected outcome, while the
suite verifies the **validator logic** — run both.

## The Go port (in progress)

`cmd/validate-intent` is a Go port of the same validator, on its way to a single static
binary adopters can drop in without a Python 3 runtime. Python remains the reference
implementation; the Go build is held to it byte for byte.

**Implemented so far:** adopter (`FILE...`) mode, `-h`/`--help`, self-test mode,
`--source`, `--source --json`, and recursive `**` globs.

**Not yet:** stdin (`-`), and `--json` for adopter (`FILE...`) mode.

Both of those *refuse* with exit `2` and a diagnostic naming themselves, rather than
falling through to the nearest surface that would accept the arguments — `-` read as a
filename glob that matches nothing, or a `--json` request answered with the human
report — and delivering a confident, correctly formatted, wrong result.

These two lists are not maintained by hand alone. `tests/parity/check_readme_surfaces.py`
parses them and runs the built binary once for every surface it knows how to probe, so a
surface that ships without this block being updated turns the parity harness red.

That same refuse-rather-than-guess rule covers the schema's `pattern` keyword. Python's
`re` and Go's RE2 are not the same regex language even where both accept the same source
text — Python's `$` also matches before a trailing newline, `\d`/`\w`/`\s`/`\b` are
Unicode-aware in Python and ASCII-only in RE2, `[[:alpha:]]` and `\p{L}` are RE2-only,
and `{,n}` means `{0,n}` to Python and four literal characters to RE2. Some constructs
are worse than merely different: `\p{L}`, or a quantifier on a bare `^`/`\A` (`^*`),
compile under RE2 and are outright parse errors in Python, so accepting one would have
the port answering a question the reference raises an exception on — and `^*` answers it
vacuously, matching every input. The port accepts only the constructs that provably
agree (rewriting a trailing `$` to `(?:\n?\z)`, its exact equivalent) and refuses the
whole schema with exit `2` otherwise, naming the construct. The shipped schema declares
no patterns, so this affects schema growth rather than current behaviour — see
`cmd/validate-intent/pypattern.go`.

```sh
go build -o bin/validate-intent-go ./cmd/validate-intent
./bin/validate-intent-go 'examples/*.json'
```

Build it into `bin/`. Like the Python script, the binary looks for
`schemas/open-test-intent.v1.json` relative to its own directory's parent, and that copy
wins whenever it is there — which is what lets the parity harness point both
implementations at a schema of its choosing.

Unlike the Python script, a binary that finds **no such file** falls back to a copy of the
canonical schema compiled into it (`schema.go`), so a released binary works from
`/usr/local/bin/` or anywhere else. The fallback is narrow on purpose: it fires only when
the file is *absent*. A schema that is present but unreadable or malformed still fails
exactly as before, because a file that exists is a deliberate override and silently
validating against a different schema than the one you wrote is worse than stopping.

The embedded copy is pinned to `schemas/open-test-intent.v1.json` by SHA256 in
`schema_test.go`, so editing the canonical schema fails loudly rather than leaving two
copies of the contract to drift. That guard runs under `go test ./...` — note the `./...`,
not `./cmd/...`.

Note that the *fixture corpus* is not embedded, only the schema: a bare self-test outside
the repo still reports `no fixtures match ...` and exits 1, which is the empty-fixture
guard working as intended rather than a regression.

```sh
tests/parity/run_parity.sh   # the acceptance test for the port
```

The parity harness runs both implementations over the same arguments and requires
identical **stdout, stderr and exit code** — any single byte of difference fails the run.
It also asserts that the unimplemented surfaces refuse, and that the Python reference has
no local modifications (a port "made to pass" by editing its own oracle would otherwise
look green). Cases excluded from the comparison are listed at the top of the script with
the reason for each.

```sh
tests/parity/run_ruby_parity.sh   # the second oracle: the specguard-rspec gem
```

A third participant, run as the last section of `run_parity.sh` and also standalone. The
[specguard-rspec](https://github.com/yatfa-ai/specguard-rspec) gem's `specguard-lint` is
an independent implementation of the same protocol — written against `PROTOCOL.md`, not
against this port — so its agreement is evidence the Python↔Go comparison cannot give on
its own. It compares the surface the two tools share, normalising out the four report
differences a CI linter is ratified to have against a fixture self-test, and cross-checks
the annotation counts carried by the normalised-away lines so a clean corpus cannot pass
by comparing nothing to nothing.

It is a separate script because it must stay runnable **without a Go toolchain**, where
`run_parity.sh` (which rebuilds the port first) cannot. Point it at a gem checkout with
`SPECGUARD_RSPEC=/path/to/specguard-rspec`; without one it exits 2 and says nothing was
compared, rather than passing. Two read-failure differences between the gem and the port
are ratified rather than fixed, with the reasons and the assertions in the script's header.

Its section 8 compares a third pair: the gem against **itself**, once on its Ruby path and
once with `SPECGUARD_VALIDATE_INTENT` pointing at this binary — the gem's opt-in Go backend.
That pair has no ratified report differences to normalise away, so the requirement is exact
bytes on stdout, stderr and the exit code. Three read-failure messages are enumerated as
differing (the gem cannot name an errno the binary never gave it) and each is asserted to
*still* differ, so closing one retires the entry instead of leaving it to rot. A gem checkout
predating that backend ignores the variable rather than failing on it, which would compare the
Ruby path against itself and report perfect agreement — so the preflight probes for the
refusal first and exits 2 when it does not come.

```sh
go test ./...                # unit coverage for the Python-emulation layer,
                             # plus the SHA256 pin on the embedded schema
```

```sh
tests/cross/run_cross_build.sh   # the four release targets, built and verified
```

Cross-compiles `./cmd/validate-intent` for `linux/amd64`, `linux/arm64`, `darwin/amd64` and
`darwin/arm64` with `CGO_ENABLED=0` into `dist/`, and then checks that each artifact is
what it claims to be. The verification reads the **ELF and Mach-O headers** rather than the
build settings the toolchain recorded, because those settings are a playback of the
environment the script itself just exported — agreement with them proves nothing about the
file. The recorded `CGO_ENABLED` is still read as corroboration, and it is not redundant:
a cgo-enabled build of this binary links statically anyway (nothing in its import graph
calls into C), so the header assertions pass on it and the recorded setting is the only
thing that catches it.

"Statically linked" is asserted differently per platform, because on darwin it cannot mean
what it means on linux. Linux artifacts must have no `PT_INTERP` and no `DT_NEEDED` at all.
macOS has no stable raw syscall ABI — the supported interface is libSystem — so every
darwin Go binary links `/usr/lib/libSystem.B.dylib`, and any binary importing `os` also
links `/usr/lib/libresolv.9.dylib`. Those are OS-provided and present on every install, so
the darwin assertion is an allowlist bounded by that baseline: still runtime-free for an
adopter, and still failing loudly on a cgo build or a third-party dylib.

The script then runs the artifact for **this** host from an installed layout outside
`bin/` — a prefix with no `schemas/` directory — and requires the good fixtures to exit `0`
and the bad ones to exit `1`, which is the embedded fallback described above working
end to end through a real binary for the first time. `cmd/validate-intent/fileio_schema_test.go`
skips precisely this case when `os.Executable()` is unavailable, so it could not establish it.
A second prefix that *does* carry an on-disk schema (deliberately malformed) must still exit `2`
naming the path it derived; without that control, the first result would pass just as happily
if the executable-relative lookup were deleted and the embedded copy became the only path.

No toolchain, or no artifact this host can execute, exits `2` and says what went unchecked —
never a pass. The script builds and verifies only: publishing, tagging and release-asset
upload are deliberately not here, for the reason `.agents/README.md` gives.

## Versioning

Current: **v1**. Breaking changes (renamed/removed fields, narrowed enums) bump to `v2` under a new
schema `$id`. Additive optional fields do not bump the major version, but
`additionalProperties: false` means unknown keys are rejected — additions are an explicit, versioned
choice, never silent forward-compatibility.

**Status:** v1, specification stage. Built by yatfa agents — the normative specification is
[PROTOCOL.md](PROTOCOL.md).
