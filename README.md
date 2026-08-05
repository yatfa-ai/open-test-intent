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
