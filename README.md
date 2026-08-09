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

Two implementations of the same validator, held to each other byte for byte. Pick by what the
machine you are running on already has:

- **`bin/validate-intent`** — the reference implementation, Python 3 standard library only. On a
  host that already has `python3` there is nothing to install. That qualifier is the whole of it:
  a `node:alpine`, `golang` or `scratch` image has no `python3` at all, so on those this script is
  not a zero-install path but an impossible one.
- **`validate-intent`** — the Go port, a single static binary with no runtime to install anywhere.
  One line acquires and verifies it, needing neither a clone of this repository nor a Go toolchain:

  ```sh
  scripts/install.sh --from <release-asset-base-url> --prefix /usr/local/bin
  ```

  See [Installing a built artifact, and verifying it](#installing-a-built-artifact-and-verifying-it)
  for what that checks before anything lands, and [The Go port](#the-go-port) for the mode matrix.

Either one checks an annotation against `schemas/open-test-intent.v1.json`, either as parsed JSON
or straight out of your test source. The commands below invoke the Python script; the binary takes
the same arguments and prints the same bytes.

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

## The Go port

`cmd/validate-intent` is a Go port of the same validator: a single static binary adopters can drop
in without a Python 3 runtime. Python remains the reference implementation; the Go build is held to
it byte for byte.

**Implemented so far:** adopter (`FILE...`) mode, `-h`/`--help`, self-test mode,
`--source`, `--source --json`, recursive `**` globs, stdin (`-`), and `--json` for
adopter (`FILE...`) mode.

**Not yet:** nothing.

The mode matrix is complete — every surface the reference exposes is implemented
rather than refused. Packaging shipped with it: `scripts/build-release.sh` cross-compiles the four
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

What still *refuses* with exit `2` and a diagnostic naming itself, rather than answering,
is the schema's `pattern` keyword, and two environment settings this port cannot
reproduce (both below). Python's
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

One environment note, because it changes the answer rather than the wording.
`PYTHONIOENCODING` is `ENCODING:HANDLER`, and it configures **both** `sys.stdin` and
`sys.stdout`. Both fields matter, and both change verdicts rather than phrasing.

The **handler** is `surrogateescape` under the C/POSIX locale and `strict` when the
variable names an encoding. The two give *different verdicts* for the same input, in both
directions: on the way in, undecodable bytes are a `read` finding with no annotation site
under `strict` and a parsed-then-rejected one under `surrogateescape`; on the way out, a
lone surrogate (from those bytes, or from a literal `"\udc82"` escape in a file) is written
back as its original byte under `surrogateescape` and raises `UnicodeEncodeError` mid-report
under `strict`.

The **encoding** decides which bytes decode at all. Under `latin-1` every byte decodes, so
input that UTF-8 rejects never reaches the read failure: the document parses and is rejected
on *schema* instead — a different `kind`, a different `summary.annotations`, and the same
exit code. Under `ascii` the reference cannot even encode the em dash in its own report and
dies mid-write with a truncated stdout.

The port reproduces UTF-8 and both handlers in both directions
(`cmd/validate-intent/pyioerrors.go`), and **refuses anything else in every mode** — not
only for stdin, and not only the handler half. `replace` and `ignore` succeed in both
directions and produce different bytes; `latin-1`, `cp1252`, `ascii` and `utf-16` carry the
reproducible `strict` handler but a codec the port does not implement. In each case
answering would be a confident verdict about a string the reference never saw. The six
CPython aliases of UTF-8 (`utf8`, `utf_8`, `U8`, `utf`, `cp65001`, …) are *not* refused —
they resolve to the same codec, and the port compares byte-for-byte under each.

`--help` is still answered under an unreproducible **handler** — the usage block is
UTF-8-representable, so no handler can change a byte of it — and is **refused** under an
unreproducible **encoding**, because that block is not ASCII: the em dash in it kills the
reference outright under `latin-1` and comes back as different bytes under `utf-16`.

The same question is asked once more, one level down, by the **locale**. When
`PYTHONIOENCODING` names no codec, CPython takes one from the locale — and uses that same
codec for `sys.getfilesystemencoding()`, which is what decodes `sys.argv` and every
directory listing. `PYTHONUTF8=0 LC_ALL=C` makes all of them `ascii`, with nothing in
`PYTHONIOENCODING` set at all, and the reference then dies on its own `--help`. The port
refuses any environment it cannot *prove* resolves to UTF-8
(`cmd/validate-intent/pylocale.go`). That whitelist is knowingly wider than CPython's
rule, which is libc's `nl_langinfo(CODESET)` for a locale that may or may not be
installed — unanswerable from a static Go binary — so `PYTHONUTF8=0` is refused even
alongside a genuinely UTF-8 locale. An over-refusal is a visible exit `2`; the other
direction is a clean report in a codec the reference never used.

Filenames go through that codec too, which is why the port carries CPython's
`surrogateescape` on the argv and filesystem channel as well as on stdin
(`cmd/validate-intent/pyfspath.go`): a byte that is not valid UTF-8 in a *filename* has to
reach the report as `U+DC00+byte`, the way Python spells it, and not as `U+FFFD` — which
is lossy, and would collapse several distinct files onto one `file` key in `--json` while
`summary.files` still counted them all.

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

The *fixture corpus* is embedded on the same terms, so a bare `validate-intent` outside
the repo self-tests the compiled-in `examples/` and prints `12/12 fixtures matched
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
tests/parity/run_parity.sh   # the acceptance test for the port
```

The parity harness runs both implementations over the same arguments and requires
identical **stdout, stderr and exit code** — any single byte of difference fails the run.
It also asserts that the three refused surfaces still refuse, that every implemented mode
has *stopped* refusing, and that the Python reference has no local modifications (a port
"made to pass" by editing its own oracle would otherwise look green). Cases excluded from
the comparison are listed at the top of the script with the reason for each.

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
compared, rather than passing. Four differences between the gem and the port are ratified
rather than fixed — with the reasons and the assertions in the script's header.

Three of them are wording: two read failures and one parse diagnostic, each a case of the
gem declining to re-port a CPython error string. The fourth is not a wording difference at
all, and it is the one that explains the third: the two tools parse with two different JSON
parsers, and **those parsers do not accept the same language**. CPython's is strictly the
more permissive — it takes `NaN`/`Infinity`/`-Infinity`, a lone high surrogate escape, and
nesting past Ruby's `max_nesting: 100`. That list is exhaustive and was derived by sweeping
89,108 documents (this repo's own `pyjson_fuzz_test.go` corpus, plus every one of the 65,536
single `\uXXXX` escapes) through both parsers, not by collecting fixtures one at a time.

Its section 8 compares a third pair: the gem against **itself**, once on its Ruby path and
once with `SPECGUARD_VALIDATE_INTENT` pointing at this binary — the gem's opt-in Go backend.
For every payload both parsers accept, everything structural is required to be identical byte
for byte: the selection line, the summary line, which annotation failed and where, stderr and
the exit code. **Six differences are enumerated**, and each is asserted to *still* differ, so
closing one retires the entry instead of leaving it to rot.

Three of the six are read failures — the gem cannot name an errno the binary never gave it.
The fourth is a payload that survives normalisation and still is not JSON: it gets **CPython's**
parser diagnostic through the backend and **Ruby's** through the Ruby path. That is an ordinary
typo in an `@intent`, not an exotic input, so it is the difference a user flipping the variable
on will actually see.

The fifth and sixth are the acceptance-set difference from inside the gem, and neither is about
prose. On a payload only CPython parses, the backend does not word the failure differently — it
does not have the same failure. It reports a **schema** violation where the Ruby path reports a
**parse** failure; and where such a payload is otherwise schema-valid, the backend finds nothing
wrong and **exits 0 where the Ruby path exits 1**. That last case is the only known input on
which flipping the variable changes whether the run passes.

Both of those went unenumerated for a review cycle each, for the same reason twice: the corpus
was grown from guesses about inputs rather than from the generator of divergence. Section 4b
built the first payload the normalizer cannot rescue; section 4c sweeps the acceptance set and
pins its boundary, including the members that do **not** diverge (a lone *low* surrogate is fine
on both).

A gem checkout predating that backend ignores the variable rather than failing on it, which
would compare the Ruby path against itself and report perfect agreement — so the preflight
probes for the refusal first and exits 2 when it does not come.

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
