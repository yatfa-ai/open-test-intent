package main

// `--schema-source`: the schema a run on this host ENFORCES, as opposed to the
// one this binary CARRIES.
//
// WHY THIS EXISTS
// ===============
//
// `--version` (version.go) reports opentestintent.SchemaSHA256 — a pure fold of
// the compiled-in schema, with no filesystem access by design. That is the
// contract the ARTIFACT carries, and the trailer it is documented in says so and
// says what beats it: LoadSchema (fileio.go) gives a schemas/open-test-intent.v1.json
// found beside the executable priority over the embedded copy, so a run can
// enforce bytes `--version` has never seen.
//
// That was not a hypothetical hedge. tests/parity/run_parity.sh's compare_root
// and assert_pattern_refusal cases work precisely by planting a synthetic schema
// beside the binary — enforcing a non-embedded schema is the harness's normal
// operating mode. And nothing downstream could tell: specguard-rspec's
// Runner#verify! (SPGD-295) compares its vendored schema against `--version`'s
// digest, a guard a binary whose runs enforce a shadowing on-disk schema
// satisfies while enforcing something else entirely.
//
// `--version` structurally cannot answer this. It returns from run() some thirty
// lines above the LoadSchema call, and moving it below would make the identity of
// the artifact contingent on a schema being loadable — a binary that could not say
// what it is on exactly the trees where you most need to ask. So this is a SECOND
// flag rather than a widened first one, and `--version`'s bytes are unchanged:
// they are pinned by version_test.go, run_parity.sh section 16,
// scripts/build-release.sh check 3, and scripts/install.sh's post-install run.
//
// WHY IT RUNS THE REAL LOADER
// ===========================
//
// It calls LoadSchema — the same function, with the same absent-versus-unreadable
// rule, from the same executable-relative path — rather than re-deriving the path
// and hashing whatever is at it. A reporter that resolved the schema its own way
// would answer a question about ITSELF, and the one thing this flag is for is
// reporting what a verdict run would do. That also means it inherits the refusal:
// a schema that exists and cannot be read, decoded or compiled exits 2 with the
// identical diagnostic the verdict path emits (schemaLoadError in main.go),
// because a digest for a schema no run can load would be an answer to a question
// nobody asked — and printing one WITH exit 0 would be this project's house defect
// in a new place: a report that looks like a successful check of nothing.
//
// WHAT IT IS NOT
// ==============
//
// It is not a verdict surface and does not read, validate or even look at a
// FILE... argument. Like `--version` it is answered from any argv position, and
// like `--version` it loses to `--help` — and, when both are given, to
// `--version`: the older surface is the one three external consumers already
// script against, so a crossing must not change what it prints.

import (
	"fmt"
	"os"
)

// schemaSourceFlag is the exact argument this surface answers. A constant
// because run() matches it, the trailer documents it and the tests assert the
// pair agree — three places that must name the same string.
const schemaSourceFlag = "--schema-source"

// SchemaSourceLine is the single line `--schema-source` writes to stdout,
// newline excluded:
//
//	schema /usr/local/schemas/open-test-intent.v1.json sha256:6535d9ba…
//	schema <embedded schema> sha256:6535d9ba…
//
// SHAPE, and what depends on it. The digest is LAST and is the only token after
// the origin, so `${line##* }` yields `sha256:<hex>` whatever the origin contains
// — and the origin is a path, which may hold spaces. Extracting it the other way
// round works too: everything between `schema ` and the final space. Both halves
// stay extractable by a shell one-liner with no parser, which is the whole
// audience for a report a CI preflight runs.
//
// The `sha256:` prefix and the trailing position deliberately match the tail of
// VersionLine (`… schema sha256:<hex>`), so the two digests this ecosystem is
// meant to COMPARE — carried versus enforced — are grepped out of both surfaces
// the same way. They are also computed the same way, by opentestintent.SHA256Hex,
// so a difference between them means the schemas differ and never that the
// arithmetic does.
//
// The origin is printed unquoted. EmbeddedSchemaLabel already carries angle
// brackets and so cannot be mistaken for a path, which is the one ambiguity worth
// spending characters on.
func SchemaSourceLine(source SchemaSource) string {
	return fmt.Sprintf("schema %s sha256:%s", source.Origin, source.SHA256)
}

// runSchemaSource answers the flag: resolve the schema exactly as a verdict run
// would, then report where it came from and what it digests to.
//
// The compiled schema is discarded on purpose — nothing is validated here. What
// is NOT discarded is the failure: an unloadable schema exits 2 through the same
// renderer main.go's verdict path uses, so the two cannot drift into disagreeing
// about how this host's schema is broken.
func runSchemaSource() int {
	_, source, err := LoadSchema()
	if err != nil {
		os.Stderr.WriteString(schemaLoadError(source, err))
		return 2
	}
	fmt.Println(SchemaSourceLine(source))
	return 0
}
