// Command inspect-artifact answers one question about one cross-compiled
// binary: is this file really the OS/arch it was supposed to be, and is it
// really free of a runtime the target machine would have to supply?
//
// It exists because the obvious way to answer that question is circular.
//
// # Why not just trust `go version -m`
//
// `go version -m <binary>` prints the build settings the toolchain recorded,
// including GOOS, GOARCH and CGO_ENABLED. It is tempting to stop there. But
// those three lines are an echo of the environment the harness itself just
// set: run_cross_build.sh exports GOOS=darwin, and the binary duly reports
// GOOS=darwin. A check whose only evidence is a playback of its own input
// cannot fail for the reason we care about, which makes it the "vacuous green"
// shape this project keeps having to name — a gate that reports success while
// verifying nothing.
//
// So the primary evidence here is the FILE FORMAT ITSELF, read with
// debug/elf and debug/macho: the ELF machine field, the Mach-O CPU type, the
// program headers, the dynamic-linkage tables. Those are written by the
// linker as a consequence of the target, not copied from an env var, and they
// are what an actual kernel on the actual target machine will read. If the
// harness asked for arm64 and got an amd64 object, this notices; `go version
// -m` structurally cannot.
//
// The recorded build settings are still checked, as a SECOND and subordinate
// line of evidence (see checkBuildInfo). They are worth reading for exactly
// one thing the headers cannot show on their own — whether cgo was enabled —
// and they are reported as corroboration, never as the sole basis for a pass.
//
// # What "statically linked" is allowed to mean, per platform
//
// The roadmap promises a static, runtime-free binary. On linux that is
// literally achievable and is asserted literally: no PT_INTERP program header
// (nothing asks for an ELF interpreter) and no DT_NEEDED entries (nothing asks
// for a shared object). A CGO_ENABLED=0 Go binary meets this exactly.
//
// On darwin it is NOT literally achievable, and pretending otherwise would
// have produced either a false failure or — much more likely — a check
// quietly written to skip darwin. macOS has no stable raw syscall ABI; the
// supported interface is libSystem, so the Go linker emits a load command for
// it on every darwin binary regardless of cgo. Probed while writing this, on
// go1.22.12: a `func main(){}` that imports nothing links
// /usr/lib/libSystem.B.dylib, and merely importing `os` also links
// /usr/lib/libresolv.9.dylib. Both are OS-provided libraries present on every
// macOS install, so the binary is still runtime-free in the sense that
// matters: there is nothing for an adopter to install.
//
// The darwin assertion is therefore an ALLOWLIST rather than an emptiness
// check — the imported dylibs must be a subset of that OS-provided baseline.
// That still fails loudly on the regression worth catching: a cgo-enabled
// build, or a dependency that drags in a third-party dylib, appears as a name
// outside the list. An emptiness check would have been wrong; a skip would
// have been vacuous; this is the assertion that is both true and load-bearing.
//
// # Usage
//
//	inspect-artifact -os linux -arch arm64 path/to/binary
//
// Exit 0 when every assertion holds, 1 when any fails (each failure is printed
// on its own line, all of them, so one run reports the whole story rather than
// only the first problem), 2 when the file could not be examined at all.
package main

import (
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/yatfa-ai/open-test-intent/tests/cross/internal/report"
)

// darwinBaselineDylibs is the set of dynamic libraries a darwin Go binary is
// permitted to import. See the package comment for why this is an allowlist
// and not an emptiness check, and for the probe that established it.
var darwinBaselineDylibs = map[string]bool{
	"/usr/lib/libSystem.B.dylib": true,
	"/usr/lib/libresolv.9.dylib": true,
}

// elfMachines maps GOARCH to the ELF machine the linker must have written.
var elfMachines = map[string]elf.Machine{
	"amd64": elf.EM_X86_64,
	"arm64": elf.EM_AARCH64,
}

// machoCPUs maps GOARCH to the Mach-O CPU type the linker must have written.
var machoCPUs = map[string]macho.Cpu{
	"amd64": macho.CpuAmd64,
	"arm64": macho.CpuArm64,
}

func main() {
	goos := flag.String("os", "", "GOOS the artifact must be built for")
	goarch := flag.String("arch", "", "GOARCH the artifact must be built for")
	flag.Parse()

	if *goos == "" || *goarch == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: inspect-artifact -os GOOS -arch GOARCH <binary>")
		os.Exit(2)
	}
	path := flag.Arg(0)

	// The 0/1/2 translation lives in report because sha256sums makes the same
	// one, and the scripts that read these codes discriminate all three.
	problems, notes, err := inspect(path, *goos, *goarch)
	report.Exit("inspect-artifact", problems, notes, err)
}

// inspect returns the list of failed assertions and the list of observations
// worth printing on a pass. A non-nil error means the file could not be
// examined, which is neither of those things.
func inspect(path, goos, goarch string) (problems, notes []string, err error) {
	switch goos {
	case "linux":
		problems, notes, err = inspectELF(path, goarch)
	case "darwin":
		problems, notes, err = inspectMachO(path, goarch)
	default:
		return nil, nil, fmt.Errorf("no inspector for GOOS=%s", goos)
	}
	if err != nil {
		return nil, nil, err
	}

	// Subordinate evidence only — see the package comment.
	biProblems, biNotes := checkBuildInfo(path, goos, goarch)
	return append(problems, biProblems...), append(notes, biNotes...), nil
}

func inspectELF(path, goarch string) (problems, notes []string, err error) {
	f, e := elf.Open(path)
	if e != nil {
		// A file that will not parse as ELF when linux was requested is a real
		// failure, not an inability to check — but say what it IS if we can,
		// because "built for the wrong OS" is by far the likeliest cause and
		// the diagnostic should not make the reader guess.
		if m, me := macho.Open(path); me == nil {
			m.Close()
			return []string{fmt.Sprintf("expected an ELF object (GOOS=linux) but this is a Mach-O object: %s", path)}, nil, nil
		}
		return nil, nil, fmt.Errorf("not a readable ELF object: %w", e)
	}
	defer f.Close()

	want, ok := elfMachines[goarch]
	if !ok {
		return nil, nil, fmt.Errorf("no ELF machine mapping for GOARCH=%s", goarch)
	}
	if f.Machine != want {
		problems = append(problems, fmt.Sprintf("ELF machine is %v, want %v (GOARCH=%s)", f.Machine, want, goarch))
	}
	if f.Class != elf.ELFCLASS64 {
		problems = append(problems, fmt.Sprintf("ELF class is %v, want ELFCLASS64", f.Class))
	}

	// Static linkage, asserted literally on linux: nothing may request an
	// interpreter and nothing may request a shared object.
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			interp := "<unreadable>"
			if b, re := io.ReadAll(prog.Open()); re == nil {
				interp = strings.TrimRight(string(b), "\x00")
			}
			problems = append(problems, fmt.Sprintf("dynamically linked: PT_INTERP requests the loader %q", interp))
		}
	}
	libs, le := f.ImportedLibraries()
	if le != nil {
		return nil, nil, fmt.Errorf("could not read the dynamic section of %s: %w", path, le)
	}
	if len(libs) > 0 {
		sort.Strings(libs)
		problems = append(problems, fmt.Sprintf("dynamically linked: DT_NEEDED requests %s", strings.Join(libs, ", ")))
	}

	notes = append(notes, fmt.Sprintf("        ELF %v %v, no PT_INTERP, %d shared-object dependencies", f.Class, f.Machine, len(libs)))
	return problems, notes, nil
}

func inspectMachO(path, goarch string) (problems, notes []string, err error) {
	f, e := macho.Open(path)
	if e != nil {
		if l, le := elf.Open(path); le == nil {
			l.Close()
			return []string{fmt.Sprintf("expected a Mach-O object (GOOS=darwin) but this is an ELF object: %s", path)}, nil, nil
		}
		return nil, nil, fmt.Errorf("not a readable Mach-O object: %w", e)
	}
	defer f.Close()

	want, ok := machoCPUs[goarch]
	if !ok {
		return nil, nil, fmt.Errorf("no Mach-O CPU mapping for GOARCH=%s", goarch)
	}
	if f.Cpu != want {
		problems = append(problems, fmt.Sprintf("Mach-O CPU is %v, want %v (GOARCH=%s)", f.Cpu, want, goarch))
	}

	// Allowlist rather than emptiness — see the package comment for why this
	// is the strongest assertion that is also true on darwin.
	libs, le := f.ImportedLibraries()
	if le != nil {
		return nil, nil, fmt.Errorf("could not read the load commands of %s: %w", path, le)
	}
	var extra []string
	for _, l := range libs {
		if !darwinBaselineDylibs[l] {
			extra = append(extra, l)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		problems = append(problems, fmt.Sprintf(
			"links %d dylib(s) outside the OS-provided baseline: %s (a cgo-enabled build or a third-party dependency looks exactly like this)",
			len(extra), strings.Join(extra, ", ")))
	}

	notes = append(notes, fmt.Sprintf("        Mach-O %v, %d dylib(s), all within the OS-provided baseline", f.Cpu, len(libs)))
	return problems, notes, nil
}

// checkBuildInfo reads the build settings the toolchain stamped into the
// binary. This is deliberately the WEAKER half of the check: GOOS and GOARCH
// here are a playback of what the harness exported, so agreement proves little
// on its own and the file headers above are what actually establish the
// target.
//
// It earns its place for CGO_ENABLED, which is the one property the headers
// cannot settle by themselves — and the margin is much wider than it looks.
// The intuition is that a cgo build betrays itself as dynamic linkage, so the
// header check already covers it. Probed against this binary on go1.22.12, it
// does not: `CGO_ENABLED=1 go build ./cmd/validate-intent` produces an ELF
// object with no PT_INTERP and zero DT_NEEDED entries, indistinguishable from
// the CGO_ENABLED=0 build by every header assertion above. It links
// statically because nothing in this program's import graph actually calls
// into C — which is true today and is not a property anyone is guarding.
//
// So the recorded setting is not belt-and-braces here; on this repository it
// is the ONLY thing that catches a cgo-enabled build, on linux and darwin
// alike. The calibration test asserts exactly that, so the claim stays
// measured rather than remembered.
//
// A binary with no readable build info is reported as a problem rather than
// waved through: every artifact this harness builds is stamped by the same
// toolchain that just built it, so an unreadable stamp means something is
// wrong with the artifact, not with the expectation.
func checkBuildInfo(path, goos, goarch string) (problems, notes []string) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("could not read Go build info: %v", err)}, nil
	}

	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}

	if got := settings["CGO_ENABLED"]; got != "0" {
		problems = append(problems, fmt.Sprintf("recorded CGO_ENABLED=%q, want \"0\" (the artifact would need a C runtime on the target)", got))
	}
	if got := settings["GOOS"]; got != goos {
		problems = append(problems, fmt.Sprintf("recorded GOOS=%q, want %q", got, goos))
	}
	if got := settings["GOARCH"]; got != goarch {
		problems = append(problems, fmt.Sprintf("recorded GOARCH=%q, want %q", got, goarch))
	}

	notes = append(notes, fmt.Sprintf("        built by %s, CGO_ENABLED=%s (corroborating)", info.GoVersion, settings["CGO_ENABLED"]))
	return problems, notes
}
