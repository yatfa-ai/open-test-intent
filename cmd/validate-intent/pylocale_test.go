package main

// The locale gate, pinned against CPython's own answer rather than against the
// table in pylocale.go.
//
// The rule the differential enforces is asymmetric on purpose, and the asymmetry
// IS the design: an UNDER-refusal (the port answers where CPython's default
// codec is not UTF-8) is a wrong answer and fails; an OVER-refusal is a visible
// exit 2 and is counted and logged instead. A test that failed on both would
// force this gate to resolve libc's `nl_langinfo(CODESET)`, which is precisely
// what a cgo-free Go binary cannot do.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// localeEnvs is the environment matrix. Every row is reachable: PYTHONUTF8 is
// the documented switch for PEP 540, and the three locale variables are libc's
// own LC_CTYPE resolution order.
var localeEnvs = []struct {
	utf8Mode string // "" means "do not set PYTHONUTF8"
	setUTF8  bool
	variable string // "" means "set no locale variable"
	locale   string
}{
	{"", false, "", ""},
	{"", false, "LC_ALL", "C"},
	{"", false, "LC_ALL", "POSIX"},
	{"", false, "LC_ALL", "C.UTF-8"},
	{"", false, "LC_ALL", "C.utf8"},
	{"", false, "LC_CTYPE", "C.UTF-8"},
	{"", false, "LANG", "C.UTF-8"},
	{"", false, "LC_ALL", "en_US.UTF-8"},
	{"", false, "LC_ALL", "en_US.ISO-8859-1"},
	{"", false, "LC_ALL", "en_US"},
	{"", true, "", ""},
	{"1", true, "LC_ALL", "C"},
	{"1", true, "LC_ALL", "en_US.ISO-8859-1"},
	{"1", true, "", ""},
	// The rows this gate exists for: UTF-8 mode explicitly OFF, where CPython
	// hands the whole question to libc.
	{"0", true, "LC_ALL", "C"},
	{"0", true, "LC_ALL", "POSIX"},
	{"0", true, "LC_CTYPE", "C"},
	{"0", true, "LANG", "C"},
	{"0", true, "LC_ALL", "en_US.UTF-8"},
	{"0", true, "LC_ALL", "C.UTF-8"},
	{"0", true, "", ""},
	// A value CPython refuses to start on at all.
	{"2", true, "", ""},
	{"x", true, "LC_ALL", "C.UTF-8"},
}

// TestPyLocaleUnsupported_matchesCPython asks python3, in each environment,
// what its default codec actually is, and requires that the port answer a run
// ONLY when all three consumers of that codec are utf-8/surrogateescape.
//
// All three are checked, not just the filesystem one, because they are the same
// codec wearing three hats and the port depends on every one of them:
// sys.getfilesystemencoding() decodes argv and every directory listing
// (pyfspath.go), and sys.stdout.encoding is what pyIOEncodingSupported waves
// through whenever PYTHONIOENCODING names no codec (pyioerrors.go).
func TestPyLocaleUnsupported_matchesCPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH — no oracle, so this test verified nothing")
	}
	const script = `
import os, sys
os.write(1, ('%s %s %s %s\n' % (sys.getfilesystemencoding(),
                                sys.getfilesystemencodeerrors(),
                                sys.stdin.encoding,
                                sys.stdout.encoding)).encode('ascii'))
`
	accepted, refused := 0, 0
	for _, tc := range localeEnvs {
		name := fmt.Sprintf("PYTHONUTF8=%s/%s=%s", tc.utf8Mode, tc.variable, tc.locale)
		t.Run(name, func(t *testing.T) {
			// A pristine environment plus exactly the variables under test, so
			// an inherited LANG cannot make a row pass by accident. PATH is
			// kept because python3 needs it; it carries no codec.
			env := []string{"PATH=" + os.Getenv("PATH")}
			for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG", "PYTHONUTF8"} {
				os.Unsetenv(v)
			}
			if tc.setUTF8 {
				env = append(env, "PYTHONUTF8="+tc.utf8Mode)
				t.Setenv("PYTHONUTF8", tc.utf8Mode)
			}
			if tc.variable != "" {
				env = append(env, tc.variable+"="+tc.locale)
				t.Setenv(tc.variable, tc.locale)
			}

			cmd := exec.Command(python, "-c", script)
			cmd.Env = env
			out, probeErr := cmd.Output()

			// CPython refusing to START is a state the port cannot be
			// byte-identical to (it dies before bin/validate-intent's first
			// line, rc 1). It must still not ANSWER — an acknowledged
			// non-parity, the same shape as PYTHONIOENCODING=bogus.
			cpythonIsUTF8 := false
			if probeErr == nil {
				fields := strings.Fields(string(out))
				if len(fields) != 4 {
					t.Fatalf("probe returned %q, want four codec names", out)
				}
				cpythonIsUTF8 = fields[0] == "utf-8" && fields[1] == "surrogateescape" &&
					fields[2] == "utf-8" && fields[3] == "utf-8"
			}

			surface, unsupported := pyLocaleUnsupported()
			if !unsupported {
				accepted++
				if !cpythonIsUTF8 {
					t.Errorf("UNDER-REFUSAL: the port answers, but CPython reports %q (probe error %v) — "+
						"argv, the glob and both std streams would be decoded in the wrong codec",
						strings.TrimSpace(string(out)), probeErr)
				}
				return
			}
			refused++
			if surface == "" {
				t.Error("refused without naming a surface")
			}
			if cpythonIsUTF8 {
				// The allowed direction. Logged so a gate that has quietly
				// tightened to "refuse everything" is visible in the output
				// rather than silently green.
				t.Logf("over-refusal (allowed): %s", surface)
			}
		})
	}

	// The non-vacuity guard. A gate that accepted everything would show zero
	// refusals and pass every assertion above; one that refused everything
	// would show zero acceptances and pass them all too.
	if accepted == 0 || refused == 0 {
		t.Errorf("matrix exercised only one verdict (%d accepted, %d refused) — "+
			"the differential above proved nothing", accepted, refused)
	}
}

// TestPyLocaleUnsupported_rules pins the rule set itself, with no oracle, so
// the gate is still asserted on a machine with no python3 — and so a change to
// the rules has to be a deliberate edit here rather than a silent widening.
func TestPyLocaleUnsupported_rules(t *testing.T) {
	cases := []struct {
		name        string
		env         map[string]string
		unsupported bool
		mentions    string
	}{
		{"nothing set", nil, false, ""},
		{"C locale", map[string]string{"LC_ALL": "C"}, false, ""},
		{"POSIX locale", map[string]string{"LC_ALL": "POSIX"}, false, ""},
		{"a UTF-8 codeset", map[string]string{"LANG": "en_US.UTF-8"}, false, ""},
		{"a UTF-8 codeset spelled utf8", map[string]string{"LANG": "C.utf8"}, false, ""},
		{"a modifier after the codeset", map[string]string{"LANG": "en_US.UTF-8@euro"}, false, ""},
		{"UTF-8 mode forced on beats any locale",
			map[string]string{"PYTHONUTF8": "1", "LC_ALL": "en_US.ISO-8859-1"}, false, ""},
		{"an empty PYTHONUTF8 is not a setting",
			map[string]string{"PYTHONUTF8": "", "LC_ALL": "C"}, false, ""},
		{"an empty locale variable is not a setting",
			map[string]string{"LC_ALL": "", "LANG": "C.UTF-8"}, false, ""},

		{"UTF-8 mode forced off", map[string]string{"PYTHONUTF8": "0"}, true, "PYTHONUTF8"},
		{"UTF-8 mode off beats a UTF-8 locale",
			map[string]string{"PYTHONUTF8": "0", "LC_ALL": "C.UTF-8"}, true, "PYTHONUTF8"},
		{"a non-UTF-8 codeset", map[string]string{"LANG": "en_US.ISO-8859-1"}, true, "LANG"},
		{"a locale naming no codeset", map[string]string{"LC_CTYPE": "en_US"}, true, "LC_CTYPE"},
		{"a value CPython will not start on", map[string]string{"PYTHONUTF8": "2"}, true, "PYTHONUTF8"},

		// Resolution order: LC_ALL wins over LC_CTYPE wins over LANG, and the
		// refusal names the one actually in force.
		{"LC_ALL wins",
			map[string]string{"LC_ALL": "C.UTF-8", "LC_CTYPE": "en_US.ISO-8859-1"}, false, ""},
		{"LC_CTYPE wins over LANG",
			map[string]string{"LC_CTYPE": "en_US.ISO-8859-1", "LANG": "C.UTF-8"}, true, "LC_CTYPE"},
		{"LANG is the last resort",
			map[string]string{"LANG": "ja_JP.eucJP"}, true, "LANG"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG", "PYTHONUTF8"} {
				os.Unsetenv(v)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			surface, unsupported := pyLocaleUnsupported()
			if unsupported != tc.unsupported {
				t.Fatalf("pyLocaleUnsupported() = %v (%q), want %v", unsupported, surface, tc.unsupported)
			}
			if tc.mentions != "" && !strings.Contains(surface, tc.mentions) {
				t.Errorf("refusal %q does not name %s — the caller cannot tell which variable to change",
					surface, tc.mentions)
			}
		})
	}
}

func TestPyLocaleCodeset(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{
		{"en_US.UTF-8", "UTF-8"},
		{"en_US.UTF-8@euro", "UTF-8"},
		{"C.utf8", "utf8"},
		{"C", ""},
		{"POSIX", ""},
		{"en_US", ""},
		{"en_US@euro", ""},
		{"", ""},
	} {
		if got := pyLocaleCodeset(tc.locale); got != tc.want {
			t.Errorf("pyLocaleCodeset(%q) = %q, want %q", tc.locale, got, tc.want)
		}
	}
}
