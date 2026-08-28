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

**[PROTOCOL.md](PROTOCOL.md) is normative.** Together with `schemas/open-test-intent.v1.json` it
defines what a valid annotation is, down to the JSON: §1.1 states that a payload is an RFC 8259
JSON text and resolves the three points that RFC leaves to the implementation. No implementation is
the arbiter — where `validate-intent` and the document disagree, the document is right.

## How to validate

**`validate-intent`** is the canonical validator: a single static binary, no runtime to install on
any host. One line acquires and verifies it, needing neither a clone of this repository nor a Go
toolchain — the installer itself is fetched over the network and piped, so there is nothing to
check out first:

```sh
curl -fsSL https://host/owner/repo/raw/v1.4.0/scripts/install.sh | bash -s -- --from https://host/owner/repo/releases/download/v1.4.0
```

Pipe it into **`bash`**, not `sh`: arriving on stdin is the one case the script cannot re-exec
itself out of. On a host that *does* have this repository, the same script runs from the
checkout — `scripts/install.sh --from dist/release --prefix /usr/local/bin`. Both URLs above
name a **published release, which this repository deliberately does not produce**: it builds
and verifies release assets, but tagging them and uploading them is out of its scope.

See [Installing a built artifact, and verifying it](#installing-a-built-artifact-and-verifying-it)
for what is checked before anything lands, and [The binary](#the-binary) for the mode matrix and
what is genuinely still open.

It checks an annotation against `PROTOCOL.md` and `schemas/open-test-intent.v1.json`, either as
parsed JSON or straight out of your test source.

```sh
# Self-test the in-repo fixtures (no arguments): every examples/*.json must pass
# and every examples/invalid/*.json must fail, and every @intent in
# examples/sources/* must pass while every one in examples/sources/invalid/*
# must fail. Exits 0 only if all match.
validate-intent

# Validate a single annotation from stdin (the `-` sentinel) — the programmatic
# path for scripts and AI agents. Prints PASS/FAIL and exits 0/1 accordingly.
echo '{ "entity": "Order", "action": "checkout", "behavior": "returns 402 on expired card", "layer": "request" }' \
  | validate-intent -

# Validate your own annotation file(s) or glob — exits 0 if every file conforms,
# non-zero with the specific violated rule otherwise.
validate-intent path/to/intent.json
validate-intent 'specs/**/*.json'

# Validate @intent annotations *in place* inside test source files (--source,
# or -s). Each finding is reported at its file:line — the location the other
# modes can't give you. Exits 0 if every annotation found conforms.
validate-intent --source spec/models/order_spec.rb
validate-intent --source 'spec/**/*_spec.rb' 'tests/**/*.py'

# Any of the three above, as one JSON document on stdout instead of prose
# (--json goes anywhere on the line). Same checks, same exit code.
validate-intent --json --source 'spec/**/*_spec.rb'
```

The first three paths take **strict JSON**. `--source` is the one that reads the
annotation as you actually write it — it finds each `@intent:` token, captures the
object literal that follows it, and normalizes the protocol's permissive syntax
(unquoted keys, single-quoted strings, arbitrary whitespace — see
[Equivalent forms](PROTOCOL.md#equivalent-forms)) into strict JSON before validating.
Capture is string-aware, so a brace or apostrophe inside a `behavior` sentence is safe.

```
$ validate-intent --source spec/models/order_spec.rb
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
$ validate-intent --json --source spec/models/order_spec.rb
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

Two instruments, and they check different things — run both.

```sh
validate-intent      # the fixture corpus: do the shipped examples still get the expected verdict?
go test ./...        # the code: the pieces the corpus cannot reach, plus the SHA256 pin on the
                     # embedded schema (note the `./...`, not `./cmd/...`)
```

The **self-test** is the conformance instrument. Every `examples/*.json` must validate, every
`examples/invalid/*.json` must be rejected, and the same both ways for the annotations in
`examples/sources/`. A fixture set that matches *nothing* is a failure rather than a vacuous pass,
so a corpus someone deleted half of goes red instead of reporting a smaller, greener tally.

The **Go suite** covers what a verdict cannot show: PROTOCOL.md §1.1 conformance clause by clause
(`cmd/validate-intent/conformance_test.go`), the draft-07 keywords the shipped schema does not
declare, the glob's directory walk, and the encoders.

## The binary

`cmd/validate-intent` is the validator: a single static binary adopters can drop in with no runtime
at all. It is graded against `PROTOCOL.md`, `schemas/open-test-intent.v1.json` and the fixture
corpus under `examples/` — those three are the source of truth, and the binary is held to them.

**The mode matrix is complete:** adopter (`FILE...`), `-h`/`--help`, self-test, `--source`,
recursive `**` globs, stdin (`-`), `--json` for all three input modes, plus `--version` and
`--schema-source`. Packaging shipped with it: `scripts/build-release.sh` cross-compiles the four
stamped artifacts and `scripts/install.sh` puts one on a host and checks it against the manifest
before it lands, both described below and both calibrated under `tests/cross/`.

Two things are open, and neither is a behaviour of this binary. **Publishing** — tagging a version
and uploading the assets — is deliberately not in this repository: `scripts/install.sh` ("What this
does NOT do: publish, tag, or upload anything") and `tests/cross/run_cross_build.sh` ("What this
deliberately does not do") each record it in their own headers, `.agents/README.md` makes
`.github/workflows/` human-owned here, and the release chapter below says the same. That is why the
one-line install above takes a release-asset base URL somebody else published. **The wrapper gem**
exists and is **opt-in** — not absent, and not finished: `specguard-rspec`'s
`lib/specguard/rspec/validator_backend.rb` shells out to this binary when
`SPECGUARD_VALIDATE_INTENT` names one, and stays off by default because, in its own words, "there
is no release to depend on" — so the hand-rolled Ruby validator it is meant to replace is still in
place beside it. Both are downstream of the same published release rather than of a missing
capability here.

### The accepted JSON language

`PROTOCOL.md` §1.1 is normative for what the binary parses, and it resolves the three points
RFC 8259 leaves open. Each is refused with a diagnostic naming the clause it enforces, so a reader
can look the rule up rather than guess whether the tool or the payload is wrong:

| refused | why | clause |
| ------- | --- | ------ |
| an unpaired surrogate escape (`"\ud800"`) | RFC 8259 §8.2 makes it non-conformant, and it has no UTF-8 encoding, so no consumer can carry it | §1.1(a) |
| `NaN` / `Infinity` / `-Infinity` | RFC 8259 §6's number grammar has no non-finite literals; a parser taking them accepts a superset of JSON | §1.1(b) |
| nesting deeper than **100** levels | RFC 8259 §9 makes depth limits implementation-defined, so the specification fixes one | §1.1(c) |

Input that is not well-formed UTF-8 is a `read` failure and is never repaired: substituting U+FFFD
for an undecodable byte would validate text nobody wrote.

None of the three is a narrowing. `additionalProperties: false` and the schema's string-only value
types mean a non-finite number and a deep nest have no legal slot to occupy, so refusing them
changes which diagnostic is printed and never whether a payload passes. An unpaired surrogate is
the one that could move a verdict — and it moves it away from a payload that could not survive
transport in the first place.

`examples/invalid/` carries a fixture per clause. Two of the three would still be rejected by the
*schema* if the rule were deleted, so `conformance_test.go` grades them on the failure's `kind` and
on the clause its diagnostic cites, not merely on rejection.

### The schema's `pattern` keyword

`pattern` is compiled with Go's `regexp` (RE2: draft-07's ECMA-262 syntax, minus backreferences and
lookaround), and it is compiled **at schema-load time**. A pattern that will not compile fails the
whole schema with exit `2` and the engine's own message, rather than being skipped mid-verdict and
leaving a rule silently unenforced. The shipped schema declares no patterns, so this governs a
schema an adopter supplies.

```sh
go build -o bin/validate-intent-go ./cmd/validate-intent
./bin/validate-intent-go 'examples/*.json'
```

Build it into `bin/`. The binary looks for `schemas/open-test-intent.v1.json` relative to
its own directory's parent, and that copy wins whenever it is there — which is what lets a
test plant a schema of its choosing beside the binary and have it enforced.

A binary that finds **no such file** falls back to a copy of the
canonical schema compiled into it (`schema.go`), so a released binary works from
`/usr/local/bin/` or anywhere else. The fallback is narrow on purpose: it fires only when
the file is *absent*. A schema that is present but unreadable or malformed still fails
exactly as before, because a file that exists is a deliberate override and silently
validating against a different schema than the one you wrote is worse than stopping.

The embedded copy is pinned to `schemas/open-test-intent.v1.json` by SHA256 in
`schema_test.go`, so editing the canonical schema fails loudly rather than leaving two
copies of the contract to drift. That guard runs under `go test ./...` — note the `./...`,
not `./cmd/...`.

The *fixture corpus* is embedded on the same terms, so a bare `validate-intent` outside
the repo self-tests the compiled-in `examples/` and prints `15/15 fixtures matched
expectation.` — byte for byte what the same command prints inside a checkout. An
`examples/` tree beside the executable still wins when there is one.

That fallback is decided once, for the whole tree, and deliberately not per glob: a
checkout whose `examples/invalid/` has been deleted still reports `no fixtures match
'examples/invalid/*.json'` and exits 1, rather than being quietly healed from the binary.
Healing it would let someone delete the rejection fixtures without a single check going
red, which is the empty-fixture guard working in reverse.

`corpus_test.go` pins the embedded corpus against the files on disk, file for file and
byte for byte, the way `schema_test.go` pins the schema.

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

### Installing a built artifact, and verifying it

`scripts/build-release.sh <semver>` produces the four stamped release binaries in
`dist/release/` alongside a **`SHA256SUMS`** manifest listing each one by basename. All of the
release script's other checks run on the build host and leave nothing behind; the manifest is
what carries an artifact's identity off it.

`scripts/install.sh` is the other half of that seam — the step that puts a binary on a host and
checks it against the manifest before it lands. It needs no Go toolchain and no clone of this
repository, which is the entire point: building from source is the wrong ask of someone who only
wants to run the linter.

```bash
scripts/install.sh --from dist/release --prefix /usr/local/bin
scripts/install.sh --from https://host/owner/repo/releases/download/v1.4.0
curl -fsSL https://host/owner/repo/raw/v1.4.0/scripts/install.sh | bash -s -- --from https://host/owner/repo/releases/download/v1.4.0
```

**It is a bash script, and bash is the one dependency it cannot work around.** It needs no Go
toolchain and no clone, but it does need its own interpreter — so it probes for it the way it
probes for `sha256sum` and `curl`, rather than assuming it. Started by a shell that is not bash
(`sh install.sh`, or a `/bin/sh` that is dash), it re-runs itself under a bash on `PATH`; a host
with no bash **exits 2 and installs nothing**, naming the miss. The one case it cannot recover
from is `curl … | sh`, where the script arrives on stdin and there is no file to hand to bash:
pipe it into **`bash`**, as above.

It maps `uname -s`/`uname -m` onto exactly one of the four artifact names, fetches that one and
`SHA256SUMS`, verifies the **one manifest row that describes it**, and only then installs — into
the prefix under a temporary name, renamed onto `validate-intent` as the final act, after running
`--version` from the prefix to confirm the installed binary actually works there. A host with no
matching artifact, a source with no manifest, a manifest with no row for this host, or a host with
neither `sha256sum` nor `shasum` all **exit 2 and install nothing**; a digest that does not match
exits **1** and leaves the prefix untouched. "Could not check" is never a pass. It acquires and
verifies only — the human-owned half above is unchanged by it.

Note what `install.sh` deliberately does *not* run: `sha256sum -c SHA256SUMS`. The manifest
describes four artifacts and a download brings one, so `-c` would report the three you did not
download as missing files and fail every honest install.

Verifying by hand is the same manifest read the same way, and needs nothing from this repository —
it is the standard coreutils/shasum format:

```sh
sha256sum -c SHA256SUMS        # GNU coreutils, e.g. Linux
shasum -a 256 -c SHA256SUMS    # BSD/macOS
```

(Run in a directory holding the manifest and **every** artifact it lists — a full `dist/release`,
not a single downloaded binary beside it.)

The digests themselves are computed with Go's `crypto/sha256`, not with a host tool: `sha256sum`
is GNU coreutils and is absent from a stock macOS build host, which is one of only two host
families the release script can run on. The manifest is written into the staging directory and
**re-read and verified there before promotion**, so a release exists only if its own manifest
matched — a manifest generated and never checked would be the vacuous green this project keeps
having to name. It is an integrity and identity record, not a signature: the same run produced
both, so it does not defend against an attacker who could replace both. Signing and provenance
are a separate mechanism and are not claimed — and `install.sh` verifying against a manifest it
fetched from the same place as the artifact inherits exactly that limit.

```sh
go test ./tests/cross/install/   # the installer's own calibration
```

The installer's failure modes are asserted rather than assumed: a corrupted artifact, a manifest
row that belongs to another target, an artifact that does not run, an unsupported host, a host with
no digest tool, and a 404 for each of the two files it fetches. Run without `-short` it also builds
a real release and installs it end to end.

## Versioning

Current: **v1**. Breaking changes (renamed/removed fields, narrowed enums) bump to `v2` under a new
schema `$id`. Additive optional fields do not bump the major version, but
`additionalProperties: false` means unknown keys are rejected — additions are an explicit, versioned
choice, never silent forward-compatibility.

**Status:** v1, specification stage. Built by [yatfa](https://yatfa.com) agents — the normative specification is
[PROTOCOL.md](PROTOCOL.md).

---

<p align="center">
  <a href="https://yatfa.com">
    <img src="assets/built-with-yatfa.png" alt="Built with yatfa — a team of AI agents that plans, builds &amp; ships software." width="100%">
  </a>
</p>
