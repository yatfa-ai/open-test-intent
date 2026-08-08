package opentestintent

// The guard that lets the embedded schema be trusted.
//
// An embedded asset is invisible: it is a build-time snapshot of a file, and
// nothing about reading the resulting binary says which bytes went in. Two
// distinct things can go wrong, and this file separates them on purpose so a
// failure names its own cause:
//
//   1. The canonical file was EDITED. Someone changed what the linter enforces
//      — an enum member, a minLength digit — and every consumer of this schema
//      (this binary, the specguard-rspec gem's vendored copy) now enforces a
//      different contract than the one that was reviewed. The pinned digest
//      fails, loudly, and updating it is a deliberate act with a diff to
//      justify it.
//
//   2. The embed DIRECTIVE was re-pointed, at a copy, a fixture, or a
//      hand-edited variant. The bytes-equal check fails.
//
// Why a digest and not a byte count: any same-length edit — swapping an enum
// member, moving a digit of minLength — passes a size check while changing
// what the validator accepts. specguard-rspec's
// spec/specguard/rspec/schema_packaging_spec.rb made exactly this ruling for
// the vendored copy of this same file, pinning the same digest; this is the
// Go half of that precedent, deliberately kept in step with it.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// CanonicalV1SHA256 is the digest of schemas/open-test-intent.v1.json.
//
// It is the SAME constant specguard-rspec pins as CANONICAL_V1_SHA256 for its
// vendored copy. If you change one you are changing the contract for both, and
// the two pins disagreeing is the signal that a copy has drifted.
const CanonicalV1SHA256 = "6535d9ba11b0936374d43e32a8bbc859f0adcf63d343a31df35f467113992924"

const canonicalPath = "schemas/open-test-intent.v1.json"

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestEmbeddedSchemaIsTheCanonicalFile(t *testing.T) {
	embedded := SchemaJSON()
	if len(embedded) == 0 {
		// Without this the two comparisons below could both be satisfied by an
		// empty embed against an empty file — a check that passes having
		// verified nothing.
		t.Fatal("the embedded schema is empty; //go:embed matched nothing usable")
	}

	onDisk, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("cannot read the canonical schema at %s: %v", canonicalPath, err)
	}

	embeddedSum := digest(embedded)
	onDiskSum := digest(onDisk)

	// (2) The directive points at the canonical file and nothing else.
	if embeddedSum != onDiskSum {
		t.Errorf("the embedded schema is not %s\n  embedded: %s (%d bytes)\n  on disk:  %s (%d bytes)\n"+
			"The //go:embed directive in schema.go has been re-pointed at a different file. "+
			"There must be exactly one copy of this contract.",
			canonicalPath, embeddedSum, len(embedded), onDiskSum, len(onDisk))
	}

	// (1) The canonical file is the reviewed one.
	if onDiskSum != CanonicalV1SHA256 {
		t.Errorf("%s has been edited\n  want: %s\n  got:  %s\n"+
			"Editing the schema changes what every consumer enforces — this binary AND "+
			"specguard-rspec's vendored copy, which pins the same digest. If the edit is "+
			"intended, update both pins in the same change.",
			canonicalPath, CanonicalV1SHA256, onDiskSum)
	}

	if embeddedSum != CanonicalV1SHA256 {
		t.Errorf("the schema compiled into this binary is not the reviewed one\n  want: %s\n  got:  %s",
			CanonicalV1SHA256, embeddedSum)
	}
}

// What `validate-intent --version` reports, and the reason it can be believed.
//
// SchemaSHA256 exists so an INSTALLED artifact can name the contract it carries
// (see the function's own comment for why no existing guard can). That answer is
// only worth having if it is genuinely a fold of the embedded bytes, so this
// asserts it against a digest computed here, in the test, from SchemaJSON() —
// and then against the same pin every other copy of this contract is held to.
//
// The first comparison is the load-bearing one. A SchemaSHA256 that had drifted
// into reporting a stored constant, a digest of a different asset, or a digest
// of the on-disk file would satisfy the pin check alone on a clean tree and
// start lying the moment the two diverged, which is precisely the moment anyone
// asks.
func TestSchemaSHA256DigestsTheEmbeddedBytes(t *testing.T) {
	reported := SchemaSHA256()

	if got := digest(SchemaJSON()); reported != got {
		t.Errorf("SchemaSHA256 is not the digest of SchemaJSON()\n  reported: %s\n  actual:   %s\n"+
			"--version reports this value as the contract the binary carries; it must be "+
			"computed from the embedded bytes and nothing else.",
			reported, got)
	}

	if reported != CanonicalV1SHA256 {
		t.Errorf("the schema compiled into this binary is not the reviewed one\n  want: %s\n  got:  %s",
			CanonicalV1SHA256, reported)
	}

	// Lowercase hex of exactly 32 bytes. The version line is parsed as text by
	// tests/parity/run_parity.sh and by scripts/install.sh's caller, and an
	// uppercase or truncated rendering would compare unequal against every other
	// pin of this same contract while naming the same bytes.
	if len(reported) != 64 {
		t.Errorf("SchemaSHA256 returned %d characters, want 64: %q", len(reported), reported)
	}
	for _, r := range reported {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			t.Errorf("SchemaSHA256 returned a non-lowercase-hex character %q in %q", r, reported)
			break
		}
	}
}

// A fresh copy per call, so a caller decoding in place cannot corrupt the copy
// the next caller receives. The embedded asset is process-wide and every mode
// of the validator loads it through this one accessor.
func TestSchemaJSONReturnsAnIndependentCopy(t *testing.T) {
	first := SchemaJSON()
	if len(first) == 0 {
		t.Fatal("the embedded schema is empty")
	}
	original := first[0]

	first[0] = 'X'
	second := SchemaJSON()

	if second[0] != original {
		t.Errorf("mutating one result changed the next: got %q, want %q", second[0], original)
	}
}
