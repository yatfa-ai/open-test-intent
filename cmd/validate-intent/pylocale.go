package main

// CPython's DEFAULT codec — the one the locale picks — and the gate that
// refuses any environment where it is not UTF-8.
//
// This is the third variable in the same family as PYTHONIOENCODING
// (pyioerrors.go), and it was found the same way: by measuring rather than by
// assuming. Two claims this port already relied on turned out to be true only
// of the container it was written in.
//
//	pyioerrors.go   "an empty PYTHONIOENCODING encoding field means the locale
//	                 default, and the locale default here is utf-8"
//	pyfspath.go     "the filesystem encoding is utf-8/surrogateescape"
//
// Both are the SAME claim — CPython derives sys.std*.encoding (when
// PYTHONIOENCODING names no codec) and sys.getfilesystemencoding() from one
// default codec — and both are false under an environment nobody had tried.
// Measured in this container, CPython 3.13.5:
//
//	environment                  stdin/stdout   filesystem   `--help`
//	---------------------------  -------------  -----------  --------------------
//	(none)                       utf-8          utf-8        824 B, rc 0
//	LC_ALL=C                     utf-8          utf-8        824 B, rc 0
//	PYTHONUTF8=0                 utf-8          utf-8        824 B, rc 0
//	PYTHONUTF8=0 LC_ALL=C        ascii          ascii        0 B,  rc 1
//	PYTHONUTF8=0 LC_ALL=POSIX    ascii          ascii        0 B,  rc 1
//
// The last two rows are the round-5 defect one variable over: the reference
// DIES with `UnicodeEncodeError: 'ascii' codec can't encode character '—'
// in position 619` on the usage block's em dash, while the port printed 824
// clean bytes and exited 0. Nothing in PYTHONIOENCODING is set in those rows,
// so the gate that was added for it cannot see them.
//
// # Why the whole run is refused rather than the codec reproduced
//
// Under `ascii` the filesystem decoder escapes EVERY byte >= 0x80, including
// bytes that form perfectly valid UTF-8: `café.json` is `caf\udcc3\udca9.json`
// to the reference and `café.json` to a UTF-8 port. That is a divergence on
// ordinary, correctly-named files, not on exotic ones — and it lands on the
// `file` key of the --json contract.
//
// # Why this is a WHITELIST, and where it over-refuses
//
// CPython's answer, when UTF-8 mode is off, is libc's `nl_langinfo(CODESET)`
// for the locale `setlocale(LC_CTYPE, "")` resolved to. A Go binary cannot ask
// that question without cgo, and it cannot even enumerate which locales are
// INSTALLED — and installation is load-bearing: `PYTHONUTF8=0
// LC_ALL=en_US.UTF-8` answers `ascii` in this container precisely because
// en_US.UTF-8 is not installed (`locale -a` lists only C, C.utf8, POSIX), so
// libc falls back to the C locale.
//
// So this gate accepts only what it can PROVE is UTF-8, and refuses the rest.
// The failure asymmetry is the same one utf8Aliases is a whitelist for: a
// missed case over-refuses VISIBLY, with exit 2 naming the variable, where a
// missed case in the other direction answers confidently in a codec the
// reference never used.
//
// The known over-refusals, stated rather than discovered later:
//
//	PYTHONUTF8=0 with a genuinely UTF-8 locale (`PYTHONUTF8=0 LC_ALL=C.UTF-8`)
//	  — CPython answers utf-8; this refuses, because it cannot distinguish that
//	    from `PYTHONUTF8=0 LC_ALL=C`, which answers ascii.
//	a locale name carrying no codeset (`LANG=en_US`)
//	  — resolvable only by libc.
//	a locale naming a non-UTF-8 codeset that is not installed
//	  — CPython falls back to C and then to UTF-8 mode; this refuses.
//
// tests/parity/run_parity.sh section 17c ("the locale's default codec") and
// TestPyLocaleUnsupported_matchesCPython run the gate against python3's own
// sys.getfilesystemencoding() / sys.stdout.encoding across an environment
// matrix and fail on any UNDER-refusal. Over-refusals are counted and printed,
// not failed — that is the direction this gate is allowed to be wrong in.

import (
	"os"
	"strings"
)

// pyLocaleSpec is libc's LC_CTYPE resolution order: LC_ALL wins over LC_CTYPE,
// which wins over LANG, and an EMPTY value is not a setting (it is skipped, as
// `LC_ALL= python3` demonstrates by behaving exactly like an unset LC_ALL).
//
// The variable's NAME is returned alongside its value so the refusal can name
// the one actually in force — telling a caller "the LC_CTYPE locale" when they
// set LANG sends them to edit the wrong thing.
func pyLocaleSpec() (name, value string) {
	for _, candidate := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v, ok := os.LookupEnv(candidate); ok && v != "" {
			return candidate, v
		}
	}
	return "", ""
}

// pyLocaleCodeset extracts the codeset from a POSIX locale name:
// `language[_territory][.codeset][@modifier]`. `en_US.UTF-8@euro` yields
// `UTF-8`; a name with no `.` yields "" and is treated as unresolvable above.
func pyLocaleCodeset(locale string) string {
	if at := strings.IndexByte(locale, '@'); at >= 0 {
		locale = locale[:at]
	}
	dot := strings.IndexByte(locale, '.')
	if dot < 0 {
		return ""
	}
	return locale[dot+1:]
}

// pyLocaleUnsupported names the environment setting that makes CPython's
// default codec something other than UTF-8, or reports ok == false when the
// port can prove it IS UTF-8.
//
// The PYTHONUTF8 rules are measured, not remembered (`sys.flags.utf8_mode`
// alongside `sys.getfilesystemencoding()`):
//
//	"1"           UTF-8 mode ON  — the codec is utf-8 whatever the locale says.
//	unset or ""   UTF-8 mode AUTO — PEP 540 turns it ON when the LC_CTYPE locale
//	              is the C/POSIX one, and leaves it OFF otherwise. (An empty
//	              PYTHONUTF8 is measurably identical to an unset one.)
//	"0"           UTF-8 mode OFF — the codec is libc's, which is exactly what
//	              this binary cannot resolve. Refused unconditionally.
//	anything else CPython refuses to start: `Python runtime state:
//	              preinitializing`, rc 1, before bin/validate-intent's first
//	              line. An ACKNOWLEDGED NON-PARITY, the same shape as
//	              PYTHONIOENCODING=bogus: the port exits 2 naming the value.
//	              Neither side produces a report, so no consumer is misled, and
//	              reproducing a CPython startup failure is not a behaviour of
//	              this binary.
//
// In the AUTO case the locale decides, and both of its accepting branches are
// safe for the same reason — they end at utf-8 whether or not the named locale
// is installed:
//
//	C / POSIX / unset   PEP 540 auto-enables UTF-8 mode.
//	codeset is utf-8    installed -> utf-8; not installed -> libc falls back to
//	                    C -> auto-enabled -> utf-8.
func pyLocaleUnsupported() (string, bool) {
	switch value, set := os.LookupEnv("PYTHONUTF8"); {
	case !set || value == "":
		// UTF-8 mode is automatic; the locale below decides.
	case value == "1":
		return "", false
	case value == "0":
		return "the locale's own codec (PYTHONUTF8='0' turns UTF-8 mode off)", true
	default:
		return "the PYTHONUTF8 value " + PyReprString(value), true
	}

	name, locale := pyLocaleSpec()
	if locale == "" || locale == "C" || locale == "POSIX" {
		return "", false
	}
	if codeset := pyLocaleCodeset(locale); codeset != "" && utf8Aliases[pyNormalizeEncoding(codeset)] {
		return "", false
	}
	return "the " + name + " locale " + PyReprString(locale), true
}
