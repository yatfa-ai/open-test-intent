package opentestintent

// The fixture corpus, compiled in beside the schema.
//
// WHY THIS IS HERE AND NOT IN cmd/validate-intent
// ===============================================
//
// The same two rules schema.go records, for the same two reasons.
//
// It is at the module root because `//go:embed ../../examples/...` is rejected
// at compile time with "invalid pattern syntax": the directive has to live in a
// package at or above the tree it names, and the module root is the only such
// place.
//
// And examples/ is NOT copied down into cmd/validate-intent/ so a local embed
// could see it. That would create a second copy of the corpus, free to drift
// from the one the parity harness, the Ruby oracle and every reviewer actually
// read — the same objection schema.go raised against a second copy of the
// contract, and worse here, because a drifted fixture is how a validator starts
// passing a test nobody wrote.
//
// WHY EMBEDDING AT ALL
// ====================
//
// `validate-intent` with no arguments self-tests the shipped corpus, and
// --help's first line is what tells an adopter to run it. On the host an
// adopter installs to there is no corpus: RunSelfTest resolves its four globs
// executable-relative (cmd/validate-intent/selftest.go's RepoRoot), so a binary
// at /usr/local/bin/validate-intent looked for /usr/local/examples/*.json,
// found nothing, and printed four errors and exit 1 — while its schema, since
// SPGD-131, loaded perfectly well. The tool's entire on-host documentation set
// opened by advertising a command that could not work.
//
// That is also why nothing an adopter could run proved an installed binary
// validates anything. The two checks that do exercise an installed artifact
// (tests/cross/run_cross_build.sh, scripts/build-release.sh) both feed it
// fixtures by absolute path out of a checkout, so the proof existed only where
// a clone existed. scripts/install.sh now runs the bare self-test from the
// prefix, which it could not do before this file.
//
// LIKE THE SCHEMA, THIS IS A FALLBACK
// ===================================
//
// A real examples/ tree beside the executable WINS. The embedded copy is used
// only when that tree is absent, so in a checkout the self-test reads exactly
// the bytes on disk and its output stays byte-for-byte comparable against the
// Python reference. See newFixtureSource in cmd/validate-intent/selftest.go for
// the absent/present rule, and for why the decision is taken once for the whole
// TREE rather than per glob.

import "embed"

// examplesCorpus is examples/ as of the build.
//
// `all:` is deliberate. Without it `go:embed` silently drops every name
// beginning with "." or "_", which would make this FS a filtered view of the
// directory rather than a mirror of it — and corpus_test.go's guard, which
// compares the two file-for-file, would have to carry an exception list to stay
// green. It would then be green about a corpus it had stopped checking.
//
// Skipping dotfiles is Python's GLOB rule (`*` never matches one), not a
// property of the corpus, so it is applied where the reference applies it: in
// the expansion, by cmd/validate-intent's fixtureSource. Embedding faithfully
// and filtering at the glob keeps each rule in the one place it is true.
//
//go:embed all:examples
var examplesCorpus embed.FS

// ExamplesFS returns the compiled-in fixture corpus as a read-only filesystem
// whose paths are rooted at the MODULE root: "examples/unit-order-total.json",
// "examples/sources/invalid/broken_intent_spec.rb". They are therefore already
// the paths the self-test prints, relative to the repo root, which is what lets
// the embedded run and an in-checkout run produce identical stdout rather than
// merely equivalent stdout.
//
// The concrete embed.FS is returned rather than an fs.FS, which is the reverse
// of the usual advice and is here for a measured reason: fs.FS makes the
// consumer reach for fs.Glob/fs.Stat/fs.ReadFile, and those three drag io/fs's
// generic walkers and the whole `path` package into a binary that otherwise
// needs none of them — some 12KB of code to read 7KB of fixtures, before debug
// information. embed.FS's own ReadDir and ReadFile are all any consumer here
// wants, and it is immutable by construction (every field is unexported), so
// handing back the concrete type gives up no safety. cmd/validate-intent
// narrows it to the two methods it uses at its own end.
//
// corpus_test.go pins this against the files on disk, file for file and byte
// for byte. That guard is the reason this can be trusted as "the corpus"
// rather than "a corpus somebody embedded once".
func ExamplesFS() embed.FS {
	return examplesCorpus
}
