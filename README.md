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
nothing to install) that checks a parsed annotation against `schemas/open-test-intent.v1.json`.

```sh
# Self-test the in-repo fixtures (no arguments): every examples/*.json must pass
# and every examples/invalid/*.json must fail. Exits 0 only if all match.
./bin/validate-intent

# Validate a single annotation from stdin (the `-` sentinel) — the programmatic
# path for scripts and AI agents. Prints PASS/FAIL and exits 0/1 accordingly.
echo '{ "entity": "Order", "action": "checkout", "behavior": "returns 402 on expired card", "layer": "request" }' \
  | ./bin/validate-intent -

# Validate your own annotation file(s) or glob — exits 0 if every file conforms,
# non-zero with the specific violated rule otherwise.
./bin/validate-intent path/to/intent.json
./bin/validate-intent 'specs/**/*.json'
```

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
