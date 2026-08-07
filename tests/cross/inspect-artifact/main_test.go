package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Calibration for the inspector.
//
// WHY THIS EXISTS
//
// Every artifact tests/cross/run_cross_build.sh builds is expected to pass, so
// on a healthy repository the inspector returns "no problems" forever. That is
// indistinguishable from an inspector that CANNOT return anything else — a
// mapping typo, an assertion accidentally scoped to a branch that never runs,
// a rename that quietly turned a check into a no-op. The harness would stay
// green through all of it while verifying nothing, which is this project's
// house defect.
//
// So the failures are asserted here, by feeding the inspector deliberately
// wrong artifacts and requiring the SPECIFIC diagnostic each should produce.
// Requiring merely "some problem" would be a weaker check than it looks: a
// wrong-arch artifact and a wrong-OS artifact both trip the build-info
// comparison, so a test that only counted problems would pass even if the
// header assertions — the whole point of the file — had been deleted.
//
// This mirrors what tests/parity/check_readme_surfaces.py does for the same
// reason, and is cheap: the fixtures are a three-line program, not the
// validator.

// tinyProgram is the smallest thing that still produces a realistic Go binary.
// It imports os so that the darwin fixtures carry the same libresolv load
// command real binaries do — building the baseline allowlist's subject matter
// into the fixture rather than assuming it.
const tinyProgram = `package main

import "os"

func main() { os.Exit(0) }
`

// buildFixture compiles tinyProgram for the given target and returns its path.
func buildFixture(t *testing.T, goos, goarch string, cgo bool) string {
	t.Helper()

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(tinyProgram), 0o644); err != nil {
		t.Fatalf("writing fixture source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("writing fixture go.mod: %v", err)
	}

	out := filepath.Join(src, "fixture-bin")
	cgoSetting := "0"
	if cgo {
		cgoSetting = "1"
	}

	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED="+cgoSetting,
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not build a %s/%s fixture (cgo=%v), so this leg was not checked: %v\n%s",
			goos, goarch, cgo, err, combined)
	}
	return out
}

// requireProblem fails unless some problem contains want.
func requireProblem(t *testing.T, problems []string, want string) {
	t.Helper()
	for _, p := range problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no problem mentioned %q; got %#v", want, problems)
}

func TestCorrectArtifactsProduceNoProblems(t *testing.T) {
	for _, tc := range []struct{ goos, goarch string }{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	} {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			bin := buildFixture(t, tc.goos, tc.goarch, false)
			problems, _, err := inspect(bin, tc.goos, tc.goarch)
			if err != nil {
				t.Fatalf("could not inspect a correct %s/%s artifact: %v", tc.goos, tc.goarch, err)
			}
			if len(problems) != 0 {
				t.Errorf("correct %s/%s artifact reported problems: %#v", tc.goos, tc.goarch, problems)
			}
		})
	}
}

func TestWrongArchIsCaughtByTheFileHeaderAndNotOnlyTheBuildStamp(t *testing.T) {
	bin := buildFixture(t, "linux", "arm64", false)

	problems, _, err := inspect(bin, "linux", "amd64")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	// The header assertion is the load-bearing one: it reads what the linker
	// wrote. The build stamp trips too, but it would trip even if the header
	// check were gone, so it cannot stand in for this.
	requireProblem(t, problems, "ELF machine is EM_AARCH64, want EM_X86_64")
}

func TestWrongOSIsCaughtInBothDirections(t *testing.T) {
	t.Run("mach-o offered as linux", func(t *testing.T) {
		bin := buildFixture(t, "darwin", "arm64", false)
		problems, _, err := inspect(bin, "linux", "arm64")
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		requireProblem(t, problems, "this is a Mach-O object")
	})

	t.Run("elf offered as darwin", func(t *testing.T) {
		bin := buildFixture(t, "linux", "arm64", false)
		problems, _, err := inspect(bin, "darwin", "arm64")
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		requireProblem(t, problems, "this is an ELF object")
	})
}

// TestCgoIsCaughtEvenWhenItLinksStatically pins the finding that motivates
// checkBuildInfo's existence.
//
// A cgo-enabled build of a program that never calls into C still links
// statically — no PT_INTERP, no DT_NEEDED — so every header assertion in this
// file passes on it. If someone later decides the build-info check is
// redundant with the linkage check and removes it, this test is what says
// otherwise. It deliberately asserts BOTH halves: that the headers really do
// look clean, and that the artifact is rejected anyway.
func TestCgoIsCaughtEvenWhenItLinksStatically(t *testing.T) {
	hostOS, hostArch := goEnv(t, "GOHOSTOS"), goEnv(t, "GOHOSTARCH")
	if hostOS != "linux" {
		t.Skipf("this leg needs a native cgo toolchain; host is %s, so it was not checked", hostOS)
	}
	bin := buildFixture(t, hostOS, hostArch, true)

	problems, _, err := inspect(bin, hostOS, hostArch)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	requireProblem(t, problems, `recorded CGO_ENABLED="1"`)

	// The other half: confirm the linkage assertions did NOT fire, which is
	// what makes the build-stamp check the only thing standing between this
	// artifact and a pass.
	for _, p := range problems {
		if strings.Contains(p, "dynamically linked") {
			t.Fatalf("premise changed: this cgo build IS dynamically linked (%q), so it no "+
				"longer demonstrates why the build-info check is needed. Re-derive the "+
				"reasoning in checkBuildInfo's doc comment before editing this test.", p)
		}
	}
}

func TestUnexaminableFileIsAnErrorAndNotAProblem(t *testing.T) {
	// "Could not check" must stay distinguishable from "checked and clean".
	// If this ever came back as (nil, nil, nil) the caller would exit 0 on a
	// file it never read.
	path := filepath.Join(t.TempDir(), "not-a-binary")
	if err := os.WriteFile(path, []byte("module fixture\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	problems, _, err := inspect(path, "linux", "amd64")
	if err == nil {
		t.Fatalf("expected an error for an unexaminable file; got problems=%#v", problems)
	}
}

func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", key, err)
	}
	return strings.TrimSpace(string(out))
}
