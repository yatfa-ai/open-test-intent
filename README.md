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

## Versioning

Current: **v1**. Breaking changes (renamed/removed fields, narrowed enums) bump to `v2` under a new
schema `$id`. Additive optional fields do not bump the major version, but
`additionalProperties: false` means unknown keys are rejected — additions are an explicit, versioned
choice, never silent forward-compatibility.

**Status:** v1, specification stage. Built by yatfa agents — see the
[master spec](PROTOCOL.md).
