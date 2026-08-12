#!/usr/bin/env bash
#
# Install the release artifact for THIS host, verified against SHA256SUMS.
# =======================================================================
#
#   scripts/install.sh --from dist/release --prefix /usr/local/bin
#   scripts/install.sh --from https://example.invalid/releases/download/v1.4.0
#
# This is the CONSUMING half of the release seam whose producing half is
# scripts/build-release.sh beside it. That script builds four artifacts and
# writes a SHA256SUMS manifest describing them; until this file existed, nothing
# in the repository read that manifest. A manifest with no consumer is the same
# shape of vacuous green the release tooling keeps having to name: a record
# generated, promoted, and never once acted on.
#
# It is also the missing step under two pieces of documentation that already
# presume it. README.md's release section told the reader to keep the manifest
# "beside the binary you downloaded" — and there was no download. specguard-rspec's
# README tells the reader to point
# SPECGUARD_VALIDATE_INTENT at a `validate-intent` binary, and its
# validator_backend.rb names the reason its Go backend cannot be default-on:
# "There is no release to depend on." Obtaining the binary meant cloning this
# repository AND installing a Go toolchain, which is strictly more friction than
# a linter is worth to anyone who only wants to run it.
#
# What this does NOT do: publish, tag, or upload anything. Release-asset upload
# runs on CI with `GITHUB_TOKEN`, and `.agents/README.md` makes
# `.github/workflows/` human-owned. This script only acquires and verifies.
#
#
# Why it uses the HOST's digest tool, and not tests/cross/sha256sums
# ------------------------------------------------------------------
# tests/cross/sha256sums is a Go program under tests/. build-release.sh builds
# it for the build host up front, because a Go toolchain is already a hard
# prerequisite there — it is cross-compiling Go. Here it is a hard prerequisite
# of NOTHING: the premise of this script is a host with no clone and no
# toolchain, which is the entire reason the artifact is worth shipping. Reaching
# for the helper would reintroduce, in the installer, the dependency the release
# exists to remove.
#
# So verification uses `sha256sum` (GNU coreutils) or `shasum -a 256`
# (BSD/macOS/perl) — the same two commands README.md already documents, probed in
# that order. The asymmetry is real in both directions: `sha256sum` is absent
# from a stock macOS install and `shasum` is absent from many minimal Linux
# images, so neither can be assumed and a host with neither must be told exactly
# that. It exits 2 naming the miss rather than installing something it could not
# check.
#
#
# Why it does NOT run `sha256sum -c SHA256SUMS`
# ---------------------------------------------
# Because the manifest describes FOUR artifacts and this script stages ONE — the
# one matching this host. `-c` walks every row, so it would report three missing
# files and fail every honest install. (tests/cross/sha256sums goes further
# still: its check mode additionally requires the manifest's directory to hold
# nothing beyond the manifest and its own entries. That is right for a release
# being promoted, where an artifact no row describes is a real defect, and wrong
# here, where a partial download is the normal case.)
#
# What is checked instead is the ONE row that describes the host's artifact:
# select it by basename, digest the staged file, compare. Kept honest by the two
# ways that selection can fail to be a check at all:
#
#   * a manifest with NO row for the host's artifact is exit 2, never 0. Nothing
#     was found wrong; nothing was checked. Falling through would install an
#     unverified binary under a green line, which is the entire failure mode
#     this file exists to close;
#   * a manifest with MORE THAN ONE row for it is exit 2 as well. Two rows agree
#     or they do not, and a script that silently takes the first is choosing
#     which claim to believe.
#
#
# Staging, and why the install is the last thing that happens
# -----------------------------------------------------------
# build-release.sh establishes the discipline: everything is built in a staging
# directory and promoted in a single move, so its output directory holds a
# complete verified set or is left exactly as it was, never half of one. The
# same rule applies to an install prefix, which is worse to corrupt because
# something else is already there — an older `validate-intent` that works.
#
# So: the download lands in a temporary directory, the digest is checked there,
# and only then does anything enter the prefix. Even then the artifact is written
# under a temporary name INSIDE the prefix and renamed onto `validate-intent` as
# the final act, so the last step is a same-directory rename rather than a copy
# that could fail halfway over a working binary. A digest mismatch, or a binary
# that does not run, leaves the prefix untouched.
#
# The `--version` run is deliberately performed from inside the prefix rather
# than from staging, for the ordinary reason: that is the layout the binary will
# actually be used in, so a failure there is a failure an adopter would have hit.
# What it proves is exactly three things — the artifact is executable on THIS
# host, it exits 0, and it reports itself as `validate-intent` with a non-empty
# version. build-release.sh's check 3 asks that on the BUILD host, and can only
# ask it for the one target matching that host; this is the first time it is
# asked on the target one, which is the only host whose answer an adopter cares
# about.
#
# What --version does NOT prove — spelled out because it is the natural thing to
# assume, and this file was committed once already asserting it:
#
#   it does NOT exercise the embedded-schema fallback.
#
# cmd/validate-intent/main.go answers `--version` and RETURNS, some thirty lines
# and three branches above its LoadSchema() call, so a binary whose compiled-in
# schema was broken prints its version here and installs cleanly.
#
#
# The bare self-test, and why it can be run here now
# --------------------------------------------------
# So a second question is asked, and it is the one an adopter actually has:
# DOES THIS BINARY VALIDATE ANYTHING?
#
# This script could not ask it before. Every surface reaching LoadSchema needed
# something the installer has not got: adopter and --source modes want a FILE,
# and self-test — a bare invocation — wanted the repo's examples/ trees and
# exited 1 without them, having loaded the schema perfectly well. Carrying a
# fixture inside the installer to reach that code path was a larger decision
# than this script, so the fallback stayed covered only where a clone exists.
#
# SPGD-315 removed the blocker rather than working around it: the fixture corpus
# is embedded on the same absent/present rule as the schema, so a bare
# invocation on a host with no checkout runs all twelve fixtures out of the
# binary. That is now run, from the prefix, and it is a real end-to-end pass —
# schema loaded, fixtures expanded, JSON validated, source annotations
# extracted, normalized and validated, and every verdict compared against its
# expectation. It is the same command --help's first line tells an adopter to
# run.
#
# WHICH schema and WHICH fixtures is a separate question, and one this script
# used to answer without asking: both are resolved from the parent of the
# directory holding the executable, and the compiled-in copies are reached only
# when nothing is there. See "What the self-test read" below — the run is a real
# end-to-end pass either way, but only one of the two ways exercises the
# embedded copies, and the outcome now picks the words rather than being assumed.
#
# A non-zero result is exit 1, not 2: the artifact was verified, executed, and
# EXAMINED — it ran the check and did not pass it. That is "found wrong" in the
# strict sense of the house rule below, and it is the reason nothing is
# installed on it.
#
# Only the exit code is required, deliberately. Requiring "12/12" would pin this
# script to the corpus's current size, so adding a fixture would break every
# install until someone edited a number here — and the count is already asserted
# where it belongs, against the binary rather than against the installer
# (scripts/build-release.sh check 4, which pins the exact tally, and
# cmd/validate-intent/selftest_embed_test.go, which compares an embedded run to
# an in-checkout run byte for byte). What THIS check adds is the one thing
# neither of those can: it asks the question on the adopter's host, of the
# artifact that is about to be installed there.
#
# What the self-test read, and why that is now printed rather than assumed
# ------------------------------------------------------------------------
# The bare run proves the binary validates. It does not, on its own, say which
# corpus it validated — and this script used to print a line claiming the
# embedded one unconditionally.
#
# It cannot know that without looking. cmd/validate-intent/selftest.go's
# RepoRoot() and cmd/validate-intent/fileio.go's SchemaPath() resolve from the
# same dirname(dirname(exe)), and each prefers what it finds there over the copy
# compiled into it. The binary under test runs from inside $PREFIX, so that root
# is the PARENT of $PREFIX, and `--prefix <checkout>/bin` in a repository shaped
# like this one puts both trees exactly there. The self-test then passes by
# reading a disk.
#
# The two halves share a root and NOT a question, which is the detail worth
# spelling out because collapsing them is the easy mistake:
#
#   - fixtures — newFixtureSource (selftest.go:133-140) stats root/examples, the
#     DIRECTORY, and takes the embedded branch on fs.ErrNotExist and on nothing
#     else. So the tree is the thing to ask about.
#   - schema — loadSchemaFrom (fileio.go:262-273) os.ReadFile's the FILE
#     root/schemas/open-test-intent.v1.json, built by SchemaPath()
#     (fileio.go:130-137), and substitutes the embedded copy on fs.ErrNotExist
#     OF THAT FILE. The directory is never stat'd. A schemas/ holding anything
#     else — another project's JSON schemas, a partial checkout — is a run that
#     read the compiled-in schema, so asking about the directory here would
#     announce a disk read that did not happen.
#
# So the parent is probed just before the success line is printed, each half at
# the path its own rule keys on, and the outcome picks the wording. The two
# absences are independent and are reported independently: the embedded CORPUS
# is claimed only when no examples/ there could have supplied fixtures, and a
# schema file there is named even on a run whose corpus really was the
# compiled-in one, because the schema is the half it substituted for.
#
# What the probe does NOT do is refuse. scripts/build-release.sh:600-612 dies on
# these same two paths and is right to — it builds a fresh mktemp prefix for the
# sole purpose of proving the embedded copies work, so a tree there is its own
# bug. This script does not own $PREFIX: the adopter chose it, installing into a
# checkout's bin/ is a legitimate thing to want, and dying there would turn a
# working install into a hard error to protect the accuracy of a sentence. The
# exit status is identical on both branches and the three codes below are
# untouched — the probe informs the message and nothing else.
#
# tests/cross/run_cross_build.sh:298-319 keeps its own version of this claim and
# should stay. It still says something neither branch here can: it CONSTRUCTS a
# prefix with no schemas/ on disk and then requires the run to pass, so a
# fallback that stopped working is a failure there. This script reports which
# branch fired; that harness requires a particular one to.
#
# That harness probes the schemas/ DIRECTORY where this script probes the file
# under it, and both are correct, for the reason the difference is easy to get
# backwards: it asserts an ABSENCE and dies on it, so the coarser question is a
# safe over-approximation — it refuses a superset of the layouts that could have
# fed the binary a schema, and never lets one through. A POSITIVE claim inverts
# that. Over-approximating there is how a directory with no schema in it gets
# reported as the schema that was read. The same probe is conservative in one
# direction and wrong in the other; which one it is depends entirely on whether
# the answer gates a refusal or fills in a sentence.
#
#
# Exit codes — the house rule, unchanged
# --------------------------------------
#   0  installed, the installed binary reported its version, and it self-tested
#      clean (the success line names the corpus that run actually read)
#   1  examined and found WRONG — the digest did not match, the binary did not
#      run, or it ran and failed its own self-test
#   2  COULD NOT CHECK — no bash to run this script under, no such source, no
#      manifest, no row for this host, no digest tool, an unsupported host, a bad
#      invocation
#
# There is a third thing 2 has to carry, named here rather than left to be
# discovered: everything checked out but the INSTALL ITSELF failed — an
# unwritable prefix, a full disk, a final rename that did not take. That is not
# "could not check" in the strict sense; by then the artifact has been fetched,
# digest-verified AND proven to run. It lands on 2 because the alternative is 1,
# and 1 means the release was examined and found wrong, which would point an
# adopter at a corrupt download when what they have is a permissions problem. Of
# the two wrong answers, 2 is the one whose diagnostics can carry the difference,
# and every such branch below says in words what failed.
#
# 1 and 2 are kept apart for the reason tests/cross/inspect-artifact and
# tests/cross/sha256sums keep theirs apart, and are asserted separately in
# tests/cross/install/install_test.go: "could not check" must never be readable
# as a pass, and must never be readable as a substantive failure either. Nothing
# is installed on either.
#
#
# What this is NOT
# ----------------
# The manifest is an integrity and identity record, not a signature: it is
# produced by the same run that produced the artifacts, so an attacker able to
# replace one can replace the other. Verifying against a manifest fetched from
# the same place as the artifact detects a truncated download, a mangled mirror
# or a swapped build. It does not establish WHO built it. Signing, attestation
# and provenance are a different mechanism and are not claimed here — see the
# package comment of tests/cross/sha256sums, which says the same thing about the
# producing half.


# --- the interpreter, probed rather than assumed ----------------------------- #
#
# This is the FIRST command list in the file, and it is here because the
# discipline stated in the header — probe a dependency, never assume it; a host
# that lacks one is told exactly that and exits 2 — was applied to every external
# tool this script reaches for and to none of the one holding it up.
# `sha256sum`/`shasum` are probed below, `curl`/`wget` are probed below, and bash
# was assumed.
#
# What the assumption cost: the shebang is `#!/usr/bin/env bash`, so it is
# honoured by `./install.sh` and bypassed entirely by `sh install.sh` and by
# `curl … | sh`. On the common CI base image /bin/sh is dash, and this script
# holds three constructs dash cannot run — the `TARGETS=(…)` array below, the
# `${line:0:64}` substring expansions the manifest parser reads rows with, and
# the `shopt -s nocasematch` around the digest compare. Run that way it died on a
# raw shell syntax error: no exit code from this script's own vocabulary, no
# diagnostic naming what was missing, and a failure that reads to an adopter as
# "this installer is broken" rather than as "this host is missing bash".
#
# So the interpreter is now probed like everything else: if the running shell is
# not bash, re-exec under a bash that is on PATH, or exit 2 naming the miss.
# Nothing is installed on that path, because nothing was checked — the release is
# not even fetched before this runs.
#
# Two properties this preamble has to keep, both of them checked by
# tests/cross/install/install_test.go rather than left to care:
#
#   * It is POSIX, and it is ABOVE `set -euo pipefail`. `set -o pipefail` is not
#     POSIX — dash 0.5.12+ accepts it, older dash and busybox ash reject it at
#     runtime — so a guard sitting below that line would be pre-empted by exactly
#     the wrong-reason failure it exists to remove. Nothing above this block is a
#     command.
#   * The whole FILE parses under a POSIX shell, which is a stronger requirement
#     than this guard merely running. dash parses and executes incrementally, so
#     this block would run before the array below is reached — but a shell that
#     parsed the file up front would die at the array without ever reaching the
#     explanation. That is what the `eval` around TARGETS is for.
#     `dash -n scripts/install.sh` is the check; the file is PARSEABLE by POSIX
#     sh, and is still bash to RUN. Porting the three construct sites is a larger
#     change and is deliberately not made here.
#
# The diagnostics are printed inline rather than through red()/unchecked(),
# because those are defined below this block — a guard that called them would be
# reaching for functions the interpreter it is refusing has not defined yet.

if [ -z "${BASH_VERSION:-}" ]; then
  bash_missing=""

  # Can this script hand ITSELF to bash? Only if $0 names this file. Under
  # `curl … | sh` the script arrives on stdin and $0 names the shell's own
  # binary — which is a real, readable file, so `[ -f "$0" ]` answers yes and
  # re-execing on it hands bash an ELF. The shebang line is what tells the two
  # apart, and reading one line with the shell's own `read` needs no external
  # tool (`head` would not survive the restricted-PATH cases below).
  self=""
  if [ -f "$0" ] && [ -r "$0" ]; then
    first_line=""
    IFS= read -r first_line < "$0" 2>/dev/null || :
    case "$first_line" in
      "#!"*bash*) self="$0" ;;
    esac
  fi

  if [ -n "${SPECGUARD_INSTALL_REEXEC:-}" ]; then
    # Second time through: something named `bash` was found and exec'd, and the
    # shell that came back still has no BASH_VERSION. Refuse rather than exec
    # again — an installer that loops forever is a worse answer than one that
    # stops. (Exporting this variable by hand therefore turns the re-exec off and
    # produces this refusal. That direction is fail-closed: it can cost an
    # install, never cause an unchecked one.)
    bash_missing="the 'bash' on PATH is not bash — this script re-ran itself
       under it and came back in a shell that has no BASH_VERSION. Point PATH at
       a real bash and re-run. Nothing has been installed."
  elif ! command -v bash >/dev/null 2>&1; then
    bash_missing="this script is bash and there is no 'bash' on PATH to run it
       under. It was started by a shell that is not bash, so the
       '#!/usr/bin/env bash' shebang was bypassed. Install bash and re-run.
       Nothing has been installed: the release was never fetched, so nothing
       about it could be checked."
  elif [ -z "$self" ]; then
    bash_missing="this script is bash and is being read by a shell that is not
       bash, from somewhere it cannot re-read — a pipe, most likely
       'curl … | sh'. bash is on this host, but there is no file here to hand to
       it. Pipe it into bash instead — 'curl -fsSL <url> | bash -s -- --from
       <src>' — or save it and run 'bash install.sh …'. Nothing has been
       installed."
  fi

  if [ -z "$bash_missing" ]; then
    SPECGUARD_INSTALL_REEXEC=1
    export SPECGUARD_INSTALL_REEXEC
    exec bash "$self" "$@"
  fi

  printf '\033[31m%s\033[0m\n' "error: $bash_missing" >&2
  exit 2
fi

set -euo pipefail

MANIFEST_NAME="SHA256SUMS"

# The name the binary is installed under. Fixed rather than derived from the
# artifact name: `validate-intent-linux-arm64` on an adopter's PATH would make
# every invocation, script and CI config host-specific for no reason, and
# cmd/validate-intent/version.go already refuses to let a renamed copy claim a
# different identity.
INSTALL_NAME="validate-intent"

# Where the binary lands when --prefix is not given. Written down ONCE, here,
# and read from nowhere else — not from the environment. `PREFIX` used to be
# picked up from the environment as a fallback, and `PREFIX` is one of the most
# commonly exported variables in a build shell, so a script whose --help promised
# /usr/local/bin would silently install somewhere else for anyone who happened to
# have it set. A tool that puts a binary on a host has exactly two acceptable
# ways to be told where it goes: this default, and --prefix.
# tests/cross/install/install_test.go's
# TestPrefixInTheEnvironmentDoesNotRedirectTheInstall pins that, and usage()
# below prints this variable rather than repeating its value, so the help text
# cannot drift away from the behaviour again.
DEFAULT_PREFIX="/usr/local/bin"

# The four targets scripts/build-release.sh builds, in its own vocabulary. This
# list is duplicated from that script ON PURPOSE — the installer must run on a
# host with no clone, so it cannot read TARGETS out of the producer at runtime —
# and the duplication is CHECKED rather than trusted:
# tests/cross/install/install_test.go's TestTheFourTargetListsAgree reads the
# TARGETS block out of build-release.sh, out of this file and out of
# tests/cross/run_cross_build.sh, and requires all three to be the same set as
# the one the test file itself names. Without that, a target renamed in one place
# leaves the installer asking a release for an artifact it does not contain, and
# every test still green — which is the same "a target quietly dropped or
# renamed" defect build-release.sh's own header exists to close on the producing
# side. Adding a target means editing all three files; the check tells you which
# one you missed.
#
# The block is wrapped in `eval` for one reason, and it is not a bash reason: a
# bash array is not POSIX SYNTAX, so a bare `TARGETS=(` is a PARSE error for
# /bin/sh rather than a runtime one. A shell that parsed this whole file before
# running any of it would therefore die right here — beneath the interpreter
# guard at the top that exists to explain exactly that, and so never reaching it.
# Quoted, the literal is one ordinary word to a POSIX parser and the same array
# to bash, which is the only shell that ever executes it. `dash -n
# scripts/install.sh` is the check.
#
# The block's SHAPE is load-bearing beyond this script — TestTheFourTargetListsAgree
# reads `TARGETS=(`, one quoted "goos/goarch" per line, `)` out of all three shell
# files — so the literal inside the quotes is left exactly as the other two write
# it.
eval '
TARGETS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
)
'

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
dim()   { printf '\033[2m%s\033[0m\n' "$*"; }

# The two failure verbs, kept apart at the point of use so that no branch below
# has to remember which number it meant. See the header.
wrong()     { red "error: $*" >&2; exit 1; }
unchecked() { red "error: $*" >&2; exit 2; }

usage() {
  # UNQUOTED heredoc, so --prefix's default is the one the code actually uses
  # rather than a second copy of it. The backticks below are escaped for the same
  # reason the delimiter is unquoted: nothing in here may run.
  cat <<USAGE
usage: scripts/install.sh --from <dir|url> [--prefix <dir>]

  --from <dir>     a directory holding the release artifacts and SHA256SUMS,
                   e.g. dist/release after scripts/build-release.sh
  --from <url>     a base URL the artifact and SHA256SUMS sit under, e.g.
                   https://host/owner/repo/releases/download/v1.4.0
  --prefix <dir>   where to install validate-intent (default: $DEFAULT_PREFIX)

The install location comes from --prefix or that default and from nothing else;
a PREFIX in the environment is deliberately ignored.

Installs the artifact matching this host, after verifying it against the
SHA256SUMS row that describes it, then runs \`validate-intent --version\` from
the prefix to prove the installed binary works and a bare \`validate-intent\` to
prove it validates. That self-test reads the schema and fixtures compiled into
the binary, unless an examples/ tree or a schemas/open-test-intent.v1.json sits
beside the prefix, in which case the binary reads that instead; the success line
says which it was.

Exit 0 installed and verified; 1 examined and found wrong; 2 could not check
(no source, no manifest row, no digest tool, an unsupported host) or checked out
but could not be installed (an unwritable prefix). Nothing is installed on 1
or 2.
USAGE
}

# --- arguments -------------------------------------------------------------- #
#
# A bad invocation is exit 2, not 1: nothing was examined, so nothing was found
# wrong. Same reason tests/cross/sha256sums' usage() exits 2.

SOURCE=""
PREFIX="$DEFAULT_PREFIX"

while [ $# -gt 0 ]; do
  case "$1" in
    --from)
      [ $# -ge 2 ] || unchecked "--from needs a directory or base URL"
      SOURCE="$2"; shift 2 ;;
    --from=*)
      SOURCE="${1#--from=}"; shift ;;
    --prefix)
      [ $# -ge 2 ] || unchecked "--prefix needs a directory"
      PREFIX="$2"; shift 2 ;;
    --prefix=*)
      PREFIX="${1#--prefix=}"; shift ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      red "error: unknown argument '$1'" >&2
      usage >&2
      exit 2 ;;
  esac
done

if [ -z "$SOURCE" ]; then
  red "error: no source given" >&2
  cat >&2 <<'WHY'

There is no default. This script installs a binary onto the host it runs on, so
where the bytes come from is the one thing it must never guess:

  scripts/install.sh --from dist/release        # after scripts/build-release.sh
  scripts/install.sh --from https://.../v1.4.0  # a release's asset base URL

WHY
  usage >&2
  exit 2
fi

[ -n "$PREFIX" ] || unchecked "--prefix was given an empty value"

# --- which artifact is this host's? ------------------------------------------ #
#
# uname's vocabulary is not Go's, and the artifacts are named in Go's. The
# mapping is explicit and total: anything not named here is exit 2 QUOTING THE
# HOST, because the alternative — installing the nearest match — is how a
# 32-bit ARM board ends up with an arm64 binary and an `Exec format error` from
# something that reported success.

uname_s="$(uname -s)" || unchecked "could not run 'uname -s' to identify this host"
uname_m="$(uname -m)" || unchecked "could not run 'uname -m' to identify this host"

case "$uname_s" in
  Linux)  host_os="linux" ;;
  Darwin) host_os="darwin" ;;
  *)      host_os="" ;;
esac

case "$uname_m" in
  x86_64|amd64)   host_arch="amd64" ;;
  aarch64|arm64)  host_arch="arm64" ;;
  *)              host_arch="" ;;
esac

host_target=""
if [ -n "$host_os" ] && [ -n "$host_arch" ]; then
  for target in "${TARGETS[@]}"; do
    if [ "$target" = "$host_os/$host_arch" ]; then
      host_target="$target"
      break
    fi
  done
fi

if [ -z "$host_target" ]; then
  unchecked "no release artifact is built for this host ($uname_s/$uname_m).
       scripts/build-release.sh builds ${TARGETS[*]}.
       Build from source instead: go build ./cmd/validate-intent"
fi

ARTIFACT="validate-intent-${host_os}-${host_arch}"

# --- can this host check a digest at all? ------------------------------------ #
#
# Probed BEFORE anything is fetched, so a host that cannot verify is told so
# instead of downloading a binary it would then have to refuse. `sha256sum` is
# GNU coreutils and a stock macOS does not have it; `shasum` is perl-based and
# many minimal Linux images do not have it. Both are checked; neither is
# assumed; a host with neither is exit 2 and no install.
#
# Deliberately NOT falling back to openssl or a Go helper: each would
# be a third format to parse for the two platforms README.md already documents,
# and a verification path that is never exercised is a verification path nobody
# should trust. A host with neither tool gets a diagnostic naming both.

DIGEST_TOOL=""
if command -v sha256sum >/dev/null 2>&1; then
  DIGEST_TOOL="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  DIGEST_TOOL="shasum"
else
  unchecked "no SHA-256 tool on PATH: neither 'sha256sum' (GNU coreutils) nor
       'shasum' (BSD/macOS) is available, so the download could not be verified
       against $MANIFEST_NAME. Install either one and re-run. Refusing to
       install a binary this host cannot check."
fi

# digest_of prints the lowercase hex SHA-256 of its argument, or fails.
#
# Both tools print `<hex><two spaces><name>`, so the digest is the first field.
# The result is length- and alphabet-checked before it is used: a tool that
# printed a warning first, or a usage message, would otherwise be compared
# against the manifest as though it were a digest — and would "mismatch",
# reporting a corrupt download for a broken probe. That would be exit 1 telling
# a lie about exit 2's situation.
digest_of() {
  local path="$1" out=""
  case "$DIGEST_TOOL" in
    sha256sum) out="$(sha256sum "$path")" || return 1 ;;
    shasum)    out="$(shasum -a 256 "$path")" || return 1 ;;
  esac
  out="${out%% *}"
  case "$out" in
    *[!0-9a-fA-F]*|"") return 1 ;;
  esac
  [ "${#out}" -eq 64 ] || return 1
  printf '%s' "$out"
}

# --- staging ----------------------------------------------------------------- #
#
# Nothing touches $PREFIX until the digest matches. See the header.

STAGE=""
PENDING=""
cleanup() {
  if [ -n "${STAGE:-}" ]; then rm -rf "$STAGE"; fi
  # Only ever set while an unverified or unproven file is sitting in the prefix
  # under a temporary name. Cleared once it has been renamed into place, so a
  # successful run has nothing to remove and every other exit leaves the prefix
  # as it found it.
  if [ -n "${PENDING:-}" ]; then rm -f "$PENDING"; fi
  return 0
}
trap cleanup EXIT

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/validate-intent-install.XXXXXX")" \
  || unchecked "could not create a temporary directory to download into"

# --- fetch: a local directory, or a base URL --------------------------------- #
#
# Either way BOTH files must arrive, and a failure to get either is exit 2. This
# is the one branch where "carry on without it" is most tempting and most wrong:
# a missing manifest is not permission to install unverified, it is the absence
# of the only evidence there was.

case "$SOURCE" in
  http://*|https://*)
    base="${SOURCE%/}"

    DOWNLOADER=""
    if command -v curl >/dev/null 2>&1; then
      DOWNLOADER="curl"
    elif command -v wget >/dev/null 2>&1; then
      DOWNLOADER="wget"
    else
      unchecked "no HTTP client on PATH: neither 'curl' nor 'wget' is available,
       so $base could not be fetched. Download the artifact and $MANIFEST_NAME
       by hand into a directory and pass it with --from <dir> instead."
    fi

    # `curl -f` is what turns a 404 into a non-zero exit instead of a 404 page
    # written to the output file — which would then be digested, mismatch, and
    # be reported as a corrupt artifact rather than as a URL that is not there.
    fetch() {
      local url="$1" dest="$2"
      case "$DOWNLOADER" in
        curl) curl -fsSL -o "$dest" "$url" ;;
        wget) wget -q -O "$dest" "$url" ;;
      esac
    }

    dim "fetching $MANIFEST_NAME from $base ..."
    fetch "$base/$MANIFEST_NAME" "$STAGE/$MANIFEST_NAME" \
      || unchecked "could not fetch $base/$MANIFEST_NAME.
       Without the manifest there is nothing to verify the download against, and
       an unverified install is not an install this script will do."

    dim "fetching $ARTIFACT from $base ..."
    fetch "$base/$ARTIFACT" "$STAGE/$ARTIFACT" \
      || unchecked "could not fetch $base/$ARTIFACT.
       This host needs the $host_target artifact and the release does not appear
       to have one under that name."
    ;;
  *)
    [ -d "$SOURCE" ] \
      || unchecked "--from '$SOURCE' is not a directory (and is not an http:// or
       https:// URL). Run scripts/build-release.sh <semver> to produce
       dist/release, or point --from at a directory holding a downloaded
       artifact and its $MANIFEST_NAME."

    [ -f "$SOURCE/$MANIFEST_NAME" ] \
      || unchecked "$SOURCE holds no $MANIFEST_NAME, so there is nothing to
       verify an artifact against. Refusing to install unverified."

    [ -f "$SOURCE/$ARTIFACT" ] \
      || unchecked "$SOURCE holds no $ARTIFACT, which is the artifact this host
       ($host_target) needs. It has the others, or none at all; either way this
       host's binary is not there."

    cp "$SOURCE/$MANIFEST_NAME" "$STAGE/$MANIFEST_NAME" \
      || unchecked "could not copy $MANIFEST_NAME out of $SOURCE"
    cp "$SOURCE/$ARTIFACT" "$STAGE/$ARTIFACT" \
      || unchecked "could not copy $ARTIFACT out of $SOURCE"
    ;;
esac

# --- the check: the one row that describes this host's artifact -------------- #
#
# The format is fixed and simple (tests/cross/sha256sums writes it):
#
#     <64 lowercase hex>  <basename>
#
# Two spaces in coreutils TEXT mode; " *" is the binary-mode separator, which
# neither GNU sha256sum nor BSD shasum minds reading, so both are accepted here.
#
# Parsing is STRICT: a line that cannot be read with certainty stops the run
# rather than being skipped. A skipped row is a row that might have been the
# host's — and "I could not read the row for your artifact" arriving as "there is
# no such row" would be indistinguishable from a release that genuinely omits it.
# Both are exit 2, but only one of them is a bug in this parser, and a strict
# refusal is what makes the difference visible.

recorded=""
matches=0
lineno=0

while IFS= read -r line || [ -n "$line" ]; do
  lineno=$((lineno + 1))

  # A trailing newline on the last row produces no iteration; a genuinely blank
  # line in the middle is malformed. tests/cross/sha256sums refuses it too.
  [ -n "$line" ] || unchecked "$MANIFEST_NAME:$lineno: blank line — this does not
       look like a manifest this release produced, and guessing at the rest of
       it is not a check."

  sum="${line:0:64}"
  sep="${line:64:2}"
  name="${line:66}"

  [ "${#sum}" -eq 64 ] \
    || unchecked "$MANIFEST_NAME:$lineno: line is too short to hold a SHA-256 digest"
  case "$sum" in
    *[!0-9a-fA-F]*) unchecked "$MANIFEST_NAME:$lineno: digest is not hexadecimal: '$sum'" ;;
  esac
  case "$sep" in
    "  "|" *") ;;
    *) unchecked "$MANIFEST_NAME:$lineno: digest and filename are not separated by
       two spaces (or ' *'): '$line'" ;;
  esac
  [ -n "$name" ] \
    || unchecked "$MANIFEST_NAME:$lineno: row has a digest but no filename"

  if [ "$name" = "$ARTIFACT" ]; then
    recorded="$sum"
    matches=$((matches + 1))
  fi
done < "$STAGE/$MANIFEST_NAME"

if [ "$lineno" -eq 0 ]; then
  unchecked "$MANIFEST_NAME is empty. It describes nothing, so it verifies
       nothing."
fi

if [ "$matches" -eq 0 ]; then
  # NOT exit 1. Nothing was examined and found wrong: the artifact may be
  # perfectly good and this manifest simply does not describe it. Saying "wrong"
  # here would send someone hunting a corruption that is not there; saying "ok"
  # would install a binary against no evidence at all.
  unchecked "$MANIFEST_NAME lists no row for $ARTIFACT, so the download could not
       be checked. It may be a manifest for a different release, or one built
       before this host's target existed. Nothing was found wrong — nothing was
       verified, which is why this is not a pass."
fi

if [ "$matches" -gt 1 ]; then
  unchecked "$MANIFEST_NAME lists $ARTIFACT $matches times. Either those rows
       agree, in which case one of them is noise, or they disagree, in which case
       taking the first would be choosing which claim to believe. Neither is a
       check."
fi

actual="$(digest_of "$STAGE/$ARTIFACT")" \
  || unchecked "$DIGEST_TOOL could not produce a SHA-256 digest of the downloaded
       $ARTIFACT, so it could not be compared against $MANIFEST_NAME."

# nocasematch so an uppercase-hex manifest from some other producer compares
# equal. Both operands are already alphabet-checked, so neither can smuggle a
# glob metacharacter into the pattern side of [[ ]].
shopt -s nocasematch
if [[ "$actual" != "$recorded" ]]; then
  shopt -u nocasematch
  # Examined, and found wrong. $PREFIX has not been touched and will not be.
  wrong "$ARTIFACT does not match $MANIFEST_NAME:
         recorded $recorded
         actual   $actual
       The download is corrupt, truncated, or is not the artifact this manifest
       describes. Nothing has been installed."
fi
shopt -u nocasematch

green "  ok    $ARTIFACT matches $MANIFEST_NAME"

# --- install: into the prefix, under a temporary name, then renamed ---------- #

mkdir -p "$PREFIX" || unchecked "could not create the install prefix $PREFIX"
PREFIX="$(cd "$PREFIX" && pwd -P)" || unchecked "could not resolve the install prefix"

TARGET_PATH="$PREFIX/$INSTALL_NAME"

# `mv src dir` moves src INTO dir, so a directory sitting on the destination name
# would take the artifact inside itself and leave the run reporting a successful
# install of a binary that is not on anyone's PATH. Refused rather than worked
# around: this is somebody else's file and the script has no business deciding
# what it was for.
if [ -d "$TARGET_PATH" ]; then
  unchecked "$TARGET_PATH is a directory, so there is nowhere to install to under
       that name. Move it aside, or choose another --prefix."
fi

# Same directory as the destination, so the promotion below is a rename within
# one filesystem rather than a copy that could fail halfway over a working
# binary. `mktemp` rather than a name built from $$: a predictable name in a
# directory this script does not own is one an existing symlink can sit on, and
# `cp` would follow it and write the artifact wherever it pointed. Creating the
# file is mktemp's job precisely so that it cannot already be something else.
# build-release.sh reaches for mktemp beside its own destination for the same
# reason.
#
# This is also the branch an unwritable prefix comes out of, and it fails while
# $TARGET_PATH is still whatever it was, which is the point.
PENDING="$(mktemp "$PREFIX/.$INSTALL_NAME.install.XXXXXX")" \
  || unchecked "could not write into $PREFIX (permissions? try a --prefix you own,
       or re-run under sudo). The artifact was verified and nothing has been
       installed."
cp "$STAGE/$ARTIFACT" "$PENDING" \
  || unchecked "could not write $PENDING (no space?). Nothing has been installed."
chmod 755 "$PENDING" \
  || unchecked "could not make $PENDING executable"

# --- prove it runs, from the prefix ------------------------------------------ #
#
# The first time anything asks this artifact a question on the host it will
# actually run on. build-release.sh's check 3 does this on the BUILD host and can
# only do it for the one target matching it; for every adopter on any other
# platform, this is it.
#
# It is run from inside $PREFIX rather than from staging on purpose: that is the
# layout the binary will be used in, so a failure here is a failure the adopter
# would have hit on their first invocation. It proves the artifact executes on
# this host, exits 0, and names itself — and nothing beyond that. In particular
# it does NOT reach the schema load, so it cannot speak for the embedded-schema
# fallback; the bare self-test below is what does, and the header says why that
# became possible.
#
# Only stdout is captured. Whatever the binary writes to stderr goes straight to
# this script's stderr, unfiltered: folding it into the captured value would put
# a diagnostic where the version line is expected, and the `validate-intent `
# prefix check below would then reject a working binary for what its stderr said.
reported=""
if ! reported="$("$PENDING" --version)"; then
  wrong "the verified $ARTIFACT does not run on this host (its output, if any, is
       above).
       Its digest matched $MANIFEST_NAME, so the download is intact — the
       artifact itself is wrong for this host, or is broken. Nothing has been
       installed."
fi

# `--version` writes one line: `validate-intent <version> (<go> <os>/<arch>)`.
# The identity token is required to be non-empty, because
# cmd/validate-intent/version.go's whole third tier exists so it never is, and a
# version line whose version is nothing reads as fine to anything checking only
# the exit code.
case "$reported" in
  "$INSTALL_NAME "*) ;;
  *) wrong "the verified $ARTIFACT ran, but did not report itself as
       $INSTALL_NAME:
         $reported
       Nothing has been installed." ;;
esac

version_token="${reported#"$INSTALL_NAME "}"
version_token="${version_token%% *}"
[ -n "$version_token" ] \
  || wrong "the verified $ARTIFACT reported an empty version:
         $reported
       Nothing has been installed."

# --- prove it VALIDATES, from the prefix ------------------------------------- #
#
# See "The bare self-test" in the header. `--version` returns above LoadSchema,
# so everything up to this point is compatible with a binary that cannot
# validate a single document. This is the check that is not.
#
# Run with no arguments, which is the self-test, and from $PENDING for the same
# reason --version is: fixtures and schema are both resolved relative to the
# EXECUTABLE, so running the staged copy instead would resolve them against a
# temporary directory and answer a question about the wrong layout.
#
# stdout is captured and thrown away on success — twenty-five lines of PASS in
# the middle of an install is noise — and printed on failure, because those
# lines ARE the diagnostic: they name the fixture that did not match. stderr is
# left to flow through unfiltered, exactly as it is for --version, so a binary
# that fails the empty-fixture guard says which set it could not find.
selftest_output=""
if ! selftest_output="$("$PENDING")"; then
  printf '%s\n' "$selftest_output" >&2
  wrong "the verified $ARTIFACT installed and reported its version, but failed its
       own self-test (its output is above).
       It ran, so this is not a corrupt download — the binary does not validate
       correctly on this host. Nothing has been installed."
fi

# --- say what that self-test actually read ----------------------------------- #
#
# See "What the self-test read" in the header. The binary resolves both its
# schema and its fixtures from dirname(dirname(exe)); $PENDING sits inside
# $PREFIX, so that root is the parent of $PREFIX. A tree there WINS over the
# compiled-in copy, so the parent is probed and the outcome picks the wording.
# Nothing here refuses: every branch below is the success path, and the exit
# status is the same 0 in all four.
#
# Parameter expansion rather than `dirname`, so this adds no tool to the set a
# host must have — tests/cross/install/install_test.go's restricted-PATH cases
# pin that set, and a new external command here would quietly turn each of them
# into a test about a missing dirname. $PREFIX has already been through
# `cd && pwd -P` above, so it is absolute and carries no trailing slash; the one
# spelling that strips to nothing is a prefix sitting directly under /, whose
# parent is / itself.
probe_root="${PREFIX%/*}"
[ -n "$probe_root" ] || probe_root="/"

# `-e` and not `-d`, matching the rule each half of the binary actually applies:
# the embedded branch is taken on fs.ErrNotExist and on NOTHING else, so a plain
# file or a broken directory sitting on the name is still a thing that is there,
# and it keeps the on-disk path. Reporting it as an absence here would put this
# line back to guessing.
#
# The two paths are NOT parallel, and that asymmetry is the whole of what this
# probe has to get right. The fixture rule stats the examples/ DIRECTORY, so the
# directory is what is asked about. The schema rule reads the FILE
# schemas/open-test-intent.v1.json and falls back on that file being absent, so
# the FILE is what is asked about — a schemas/ directory holding anything else is
# a run that read the compiled-in schema, and probing the directory would name a
# file that does not exist. See the header for why the mirror-image probe in
# tests/cross/run_cross_build.sh is right to be coarser than this one.
#
# The filename is spelled here rather than derived because nothing on this host
# can be asked for it: the path is built in Go, at fileio.go:130-137, and this
# script has only the compiled artifact. It is pinned by the schemas-beside-the-
# prefix cases in tests/cross/install/install_test.go, which seed a real one.
have_schema=""
have_examples=""
if [ -e "$probe_root/schemas/open-test-intent.v1.json" ]; then have_schema=yes; fi
if [ -e "$probe_root/examples" ]; then have_examples=yes; fi

if [ -z "$have_schema$have_examples" ]; then
  # Nothing at that root could have supplied a substitute, so the compiled-in
  # copies are what ran. This is the line as it has always read, now earned.
  green "  ok    $INSTALL_NAME self-tested its embedded fixture corpus"
else
  if [ -z "$have_examples" ]; then
    green "  ok    $INSTALL_NAME self-tested its embedded fixture corpus, against the schema"
    green "        in $probe_root/schemas/open-test-intent.v1.json rather than the one"
    green "        compiled into it"
  elif [ -z "$have_schema" ]; then
    green "  ok    $INSTALL_NAME self-tested clean against the fixture tree in"
    green "        $probe_root/examples, not the corpus compiled into it"
  else
    green "  ok    $INSTALL_NAME self-tested clean against the fixture tree in"
    green "        $probe_root/examples and the schema in"
    green "        $probe_root/schemas/open-test-intent.v1.json, not the copies compiled into it"
  fi
  dim "        that root is the parent of the prefix, which is where $INSTALL_NAME looks"
  dim "        first, so what this run proves is what is named above."
fi

# --- the final move ---------------------------------------------------------- #
#
# Everything above passed, so this is the first and only moment $TARGET_PATH
# changes. A rename within one directory, so there is no window in which
# $TARGET_PATH is a partial file.
mv -f "$PENDING" "$TARGET_PATH" \
  || unchecked "verified $ARTIFACT and confirmed it runs, but could not move it
       onto $TARGET_PATH."
PENDING=""   # installed — the EXIT trap must not remove it

echo
green "installed $TARGET_PATH"
dim "  $reported"
case ":${PATH:-}:" in
  *":$PREFIX:"*) ;;
  *) dim "  note: $PREFIX is not on your PATH" ;;
esac

# --- the wiring this install was for ----------------------------------------- #
#
# The header above names specguard-rspec's README as one of the two pieces of
# documentation this script exists under: it tells the reader to point
# SPECGUARD_VALIDATE_INTENT at a `validate-intent` binary, and until now this
# script installed exactly that binary and never once said the variable's name
# outside its own header comment. The reader was left to go back to the other
# repository's README and hand-assemble a path this script already knew.
#
# Printed on BOTH arms of the PATH case above, and deliberately not inside it.
# The PATH note is about invoking a bare command name; this line is an absolute
# path, so it is the branch where $PREFIX is ALREADY on PATH — the arm that says
# nothing at all today — that most needs it. $TARGET_PATH is absolute and
# symlink-free by construction ($PREFIX went through `pwd -P` before it was
# built), which is the only shape the gem accepts: it refuses a bare command
# name outright, because a name resolved through someone else's PATH could run a
# different validator than the one anybody meant.
#
# On the wording: this says how to POINT the gem at this binary. It does not say
# the gem will accept it, and must not be edited into saying so. The gem
# compares the schema-contract digest it vendors against the one the binary
# reports and exits 2 before selecting a single file if they differ. What this
# script verified is a different question with a different answer — that the
# artifact matches the manifest and passes the corpus embedded in the artifact
# itself. It has never seen any gem's vendored digest and cannot speak for it.
# Claiming acceptance here would be a fresh instance of the vacuous green the
# header at the top of this file was written against.
#
# On the quoting: unlike `installed $TARGET_PATH` above, which is only ever
# read, this line is shaped as a command and is printed to be pasted into a
# shell. --prefix accepts a path containing a space (or a `$`, or a backtick),
# and unquoted `export VAR=/tmp/pre fix/validate-intent` does not fail loudly on
# paste — it sets the variable to the truncated `/tmp/pre`, so the next run
# blames a path the reader never typed. `printf %q` is the fix for the class
# rather than for the space: it emits ordinary paths byte-for-byte unchanged and
# adds escaping only where a shell would otherwise re-split or expand the value.
# The redundant-looking escapes on an exotic path are the point; do not remove
# them because the common case renders without any.
echo
dim "to point specguard-lint at it:"
dim "  export SPECGUARD_VALIDATE_INTENT=$(printf '%q' "$TARGET_PATH")"
dim "  that names this binary; whether the gem accepts it is the gem's own check,
  made on its next run against the schema contract it vendors. This script
  checked the artifact against the manifest and against the corpus embedded in
  the artifact itself, which is a different question."
