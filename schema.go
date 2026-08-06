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
	_ "embed"
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
