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

## Versioning

Current: **v1**. Breaking changes (renamed/removed fields, narrowed enums) bump to `v2` under a new
schema `$id`. Additive optional fields do not bump the major version, but
`additionalProperties: false` means unknown keys are rejected — additions are an explicit, versioned
choice, never silent forward-compatibility.

**Status:** v1, specification stage. Built by yatfa agents — see the
[master spec](PROTOCOL.md).
