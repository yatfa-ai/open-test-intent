// Package opentestintent carries the canonical Open Test Intent v1 schema as an
// asset compiled into any binary that imports it.
//
// WHY THIS PACKAGE EXISTS AT THE MODULE ROOT
// ==========================================
//
// It exists here, rather than in cmd/validate-intent where it is used, because
// `go:embed` cannot reach a parent directory: `//go:embed ../../schemas/...`
// is rejected at compile time with "invalid pattern syntax". The directive has
// to live in a package at or above schemas/, and the module root is the only
// such place.
//
// The alternative — copying schemas/open-test-intent.v1.json down into
// cmd/validate-intent/ so a local embed could see it — was rejected
// deliberately. That would create a SECOND copy of the contract, free to drift
// from the canonical one, and a linter enforcing a schema nobody edited is the
// exact failure this repo exists to prevent. One file, embedded from above.
//
// WHY EMBEDDING AT ALL
// ====================
//
// cmd/validate-intent derives its schema path from its own location
// (fileio.go's SchemaPath), a faithful port of the Python reference's
// REPO_ROOT computation. That is correct for a script that always lives at
// <repo>/bin/, and wrong for a distributable binary, whose entire purpose is
// to live somewhere else: installed at /usr/local/bin/validate-intent it
// resolved its schema to /usr/local/schemas/open-test-intent.v1.json, found
// nothing, and exited 2 in every working mode.
//
// The embedded copy is a FALLBACK, not a replacement — see LoadSchema in
// cmd/validate-intent/fileio.go for the absent-versus-unreadable rule and why
// the distinction is load-bearing.
package opentestintent

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// schemaJSON is the canonical schema, verbatim. Held as a string rather than a
// []byte so it cannot be mutated through the package variable: an embedded
// []byte is writable by any importer, and a schema that changes under the
// validator at run time is not a failure mode worth leaving open for the sake
// of one avoided copy of 638 bytes.
//
//go:embed schemas/open-test-intent.v1.json
var schemaJSON string

// SchemaJSON returns the bytes of the canonical schemas/open-test-intent.v1.json
// as of the build. Each call returns a fresh copy, so a caller that decodes it
// in place cannot corrupt the copy the next caller gets.
//
// schema_test.go pins these bytes to the canonical file by SHA256. That guard
// is the reason this can be trusted as "the schema" rather than "a schema
// somebody embedded once".
func SchemaJSON() []byte {
	return []byte(schemaJSON)
}

// SchemaSHA256 returns the hex-encoded SHA-256 of the bytes SchemaJSON returns:
// the identity of the contract THIS BINARY CARRIES, computed from the compiled-in
// copy with no filesystem access at all.
//
// WHY A BINARY NEEDS TO BE ABLE TO SAY THIS
// =========================================
//
// An embedded asset is invisible. Every drift guard in this ecosystem compares a
// MATCHED PAIR inside ONE checkout — schema_test.go digests the embed against
// schemas/open-test-intent.v1.json in the Go tree, specguard-rspec's
// schema_packaging_spec.rb digests the gem's vendored copy against the gem's own
// pin, tests/parity/run_ruby_parity.sh compares two files in two checkouts. None
// of them is looking at an INSTALLED artifact, and none of them can: an installed
// binary has no schemas/ beside it (tests/cross/run_cross_build.sh asserts the
// absence). So a gem that vendors schema A can be pointed at a binary built when
// canonical was B, and all three guards stay green while the two halves enforce
// different contracts.
//
// Answering that needs no new pin and no new file — the bytes are already here.
// It only needs the binary to be ASKED, which is what `--version` now does
// (cmd/validate-intent/version.go's VersionLine).
//
// WHAT IT DOES NOT CLAIM
// ======================
//
// This is the contract the artifact CARRIES, not necessarily the one a given run
// ENFORCED. LoadSchema (cmd/validate-intent/fileio.go) gives a schema file found
// beside the executable priority over the embedded copy, and this function never
// consults that decision — it cannot, it does not touch the disk. Anything
// reporting this digest owes its reader that distinction; see helpTrailer.
//
// The distinction is now ANSWERABLE rather than only disclosed: `--schema-source`
// (cmd/validate-intent/schemasource.go) runs the same LoadSchema a verdict run
// does and reports the resolved origin plus the digest of the bytes it actually
// loaded, computed through SHA256Hex below — the same fold this function uses, so
// the two digests are comparable and a difference between them means the schemas
// differ rather than the arithmetic does.
//
// The digest is recomputed per call rather than cached in a package-level var:
// over 638 bytes it is far below the cost of the process start that precedes it,
// and a function that is a pure fold of a compile-time constant has no
// initialisation order to reason about.
//
// schema_test.go pins this against CanonicalV1SHA256, the same constant
// specguard-rspec pins, so the value this reports is guarded by the check that
// already existed rather than by a second, independently-drifting claim.
func SchemaSHA256() string {
	return SHA256Hex([]byte(schemaJSON))
}

// SHA256Hex is the ONE way this module turns schema bytes into a digest:
// SHA-256, hex-encoded, lowercase. SchemaSHA256 is its application to the
// compiled-in copy; cmd/validate-intent applies it to whatever bytes a run
// actually loaded (LoadSchema in fileio.go, reported by `--schema-source`).
//
// It is shared rather than reimplemented at the second call site because those
// two digests are compared — by an operator, and by any consumer asking whether
// the schema a run ENFORCED is the schema the artifact CARRIES. Equal bytes must
// produce equal strings, and two independent folds are two chances to encode
// them differently (uppercase hex, a truncation, a different digest entirely)
// and turn "these agree" into "these were printed by different code". A
// difference must mean the SCHEMAS differ; nothing else.
//
// It takes the bytes rather than reading a path on purpose: the caller has
// already read them, and a function that re-read the file could digest
// something other than what was loaded — which is the exact failure the
// enforced-schema report exists to rule out.
func SHA256Hex(schema []byte) string {
	sum := sha256.Sum256(schema)
	return hex.EncodeToString(sum[:])
}
