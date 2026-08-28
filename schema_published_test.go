package opentestintent

// Does the identifier the schema names actually serve the schema?
//
// Every other guard in this ecosystem compares copies to EACH OTHER, and all of
// them are offline: CanonicalV1SHA256 here, CANONICAL_V1_SHA256 in
// specguard-rspec's schema_packaging_spec.rb, SCHEMA_BLOB_SHA in SpecGuard's
// OpenTestIntent service. They are good guards and they cover a real failure —
// a copy drifting from its publisher. What not one of them touches is the
// PUBLISHED ADDRESS. If the schema-v1.0 tag were force-moved or deleted
// tomorrow, every suite across all three repositories would stay entirely green
// while the `$id` in the document returned different bytes, or 404'd. The one
// property this file's schema promises the outside world would be the only one
// nothing checked — a guard reporting success having verified nothing about the
// thing in question.
//
// So this test is the network half, and it deliberately reads the URL OUT OF
// THE SCHEMA rather than pinning a literal. A hardcoded URL here would prove
// that some address serves these bytes; reading `$id` proves that the address
// THIS DOCUMENT NAMES serves them, which is the actual claim PROTOCOL.md §3
// makes normatively.
//
// It is opt-in, because a test that reaches the network cannot be in the path
// of an offline `go test ./...` without making a working tree's verdict depend
// on a DNS lookup. Set OTI_CHECK_PUBLISHED_SCHEMA=1 to run it. When it is unset
// the test SKIPS rather than passes — a skip is visible in the output and says
// "not checked", where a silent pass would say "checked and fine".
//
//	OTI_CHECK_PUBLISHED_SCHEMA=1 go test -run TestPublishedSchemaMatchesTheCanonicalFile -count=1 .
//
// `-count=1` matters: a cached PASS would report success without the fetch
// happening, which is the failure mode this test exists to rule out.
//
// NOT YET WIRED TO ANYTHING SCHEDULED. This repository has no test workflow —
// .github/workflows holds only release.yml — so nothing runs this on a cadence
// today, and until something does it checks the published address only when a
// person or an agent asks it to. That is a real limitation and it is written
// here rather than in a commit message because the gap belongs next to the
// guard: a moved tag would still go unnoticed between runs. Wiring it to a
// weekly schedule is the intended follow-up.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The env var that opts in. Named for what it does rather than for CI, since
// anyone can run it from a checkout to answer "is the published schema still
// what I have here?" — which is exactly the question worth being able to ask.
const publishedSchemaEnvVar = "OTI_CHECK_PUBLISHED_SCHEMA"

func TestPublishedSchemaMatchesTheCanonicalFile(t *testing.T) {
	if os.Getenv(publishedSchemaEnvVar) == "" {
		t.Skipf("network check not requested; set %s=1 to fetch the published schema", publishedSchemaEnvVar)
	}

	onDisk, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("cannot read the canonical schema at %s: %v", canonicalPath, err)
	}

	var doc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(onDisk, &doc); err != nil {
		t.Fatalf("cannot parse %s: %v", canonicalPath, err)
	}
	if doc.ID == "" {
		// Without this the fetch below would be of the empty string and would
		// fail with an unreadable URL error rather than naming the real cause.
		t.Fatalf("%s has no $id, so there is no published address to check", canonicalPath)
	}
	if !strings.HasPrefix(doc.ID, "https://") {
		t.Fatalf("the $id %q is not an https address, so it cannot be fetched", doc.ID)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(doc.ID)
	if err != nil {
		t.Fatalf("the $id %s could not be fetched: %v\n"+
			"PROTOCOL.md §3 states this is a real, fetchable address. Either the tag it names "+
			"was deleted, or the network is unavailable to this run.", doc.ID, err)
	}
	defer resp.Body.Close()

	// Checked before digesting so a 404 reports itself as a 404. Comparing the
	// digest first would report GitHub's "404: Not Found" body as a byte
	// mismatch, which names the symptom and hides the cause.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the $id %s answered %s, not 200 OK\n"+
			"The identifier this schema publishes no longer resolves to a document.",
			doc.ID, resp.Status)
	}

	fetched, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response from %s failed: %v", doc.ID, err)
	}

	// Against the pinned constant, not against onDisk. Comparing the response
	// to the working tree would go green on a branch that edited the schema and
	// re-pointed the tag at itself — the drift this is here to catch. The
	// constant is the reviewed value, and the file is separately held to it by
	// TestEmbeddedSchemaIsTheCanonicalFile.
	if got := digest(fetched); got != CanonicalV1SHA256 {
		t.Errorf("the schema served at the published $id is not the canonical one\n"+
			"  $id:    %s\n  want:   %s\n  served: %s (%d bytes)\n"+
			"A published identifier whose content drifts is worse than one that never resolved: "+
			"every consumer who pinned it is now being handed a document they did not pin. "+
			"The tag this $id names must never move — see PROTOCOL.md §3.",
			doc.ID, CanonicalV1SHA256, got, len(fetched))
	}
}
