#!/usr/bin/env python3
"""Verify that the README's Go-port status block matches the built binary.

WHY THIS EXISTS
===============

README.md carries the only statement of the Go port's surface that an adopter
ever reads: two lists, "Implemented so far" and "Not yet". Everything else that
states the same surface already has a checker aimed at it -- main.go's `usage`
constant is byte-compared against the reference, the excluded surfaces are
asserted to refuse in run_parity.sh section 16 ("Go-side refusals — the
excluded surfaces, still asserted"), and the cross-references between those
files are checked by check_section_refs.py.

The README had none, and it drifted exactly the way an unchecked claim does.
Two commits shipped self-test mode, `--source`, `--source --json` and recursive
`**` globs without touching README.md, which went on listing all four under
"Not yet" for several slices. The text was not vague or out of date at the
edges: it told a reader that four working features refuse to run. A reader who
believed it would have reached for `python3 bin/validate-intent` forever, and
nothing in the repo would have contradicted them.

Prose is not documentation here, it is an assertion, and an assertion nobody
executes is one nobody maintains.

WHAT IT DOES
============

Parse both lists out of README.md, map each named surface onto a real argument
vector, RUN THE BUILT BINARY with it, and require the outcome to agree with the
list the surface was named in:

    named under "Not yet"           must refuse: exit 2 whose stderr carries
                                    notImplemented's wording
    named under "Implemented so far"  must not refuse

That is the same refused/not-refused split `assert_refusal` and
`assert_not_refused` use in run_parity.sh, tied to the same string, so the two
cannot drift apart into different notions of "implemented".

WHY IT REFUSES MORE THAN A MISMATCHED CLAIM
===========================================

Three ways this check could report success while checking nothing, all of them
errors here:

  * An EMPTY WORK SET. If the labels are reworded or the block is moved, a
    naive parser finds no surfaces, every probe is unreachable, and the check
    passes by having nothing to say. check_section_refs.py carries the same
    guard for the same reason, and this project has a name for the shape it
    stops ("vacuous green").

  * A SURFACE NAMED IN NEITHER LIST. The registry below is the set of surfaces
    this repo knows about; one that appears in no list is unchecked, and
    deleting a line from the README would otherwise be the way to silence a
    disagreement.

  * A SURFACE THIS SCRIPT CANNOT PROBE. An item that matches no registry entry
    is a claim with no experiment behind it. Adding vocabulary to the README
    means adding it to ALIASES, which is the point at which somebody has to say
    what invocation would prove it.

And because a checker that only ever passes is indistinguishable from one that
cannot fail, `self_check` runs first, every time: it feeds deliberately-wrong
READMEs through this same parser and the same recorded probe outcomes, and
requires each to produce the SPECIFIC problem it was built to produce -- not
merely "a" problem. Its positive control is derived from what the binary
actually does, so it stays a valid control as surfaces ship.

WHEN IT CANNOT COMPARE
======================

bin/validate-intent-go is a build artifact, not a committed file. With no
binary and no Go toolchain nothing was compared, and this exits 2 saying so --
never a pass it did not earn. run_ruby_parity.sh's preflight takes the same
line for the same reason.

USAGE
=====

    python3 tests/parity/check_readme_surfaces.py

    VALIDATE_INTENT_GO=/path/to/binary   use this binary, never build
    GO=/path/to/go                       toolchain to build with

Exit 0 when the README agrees with the binary, 1 on disagreement (with a
diagnostic naming the surface, the list it was found in, and what the binary
actually did), 2 when there was nothing to compare against.
"""

import os
import re
import shutil
import subprocess
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

README = os.path.join("README.md")

# The two labels that own the status block. They are matched literally: a
# reworded label is a parse failure (reported), not a silent empty run.
IMPLEMENTED_LABEL = "**Implemented so far:**"
NOT_YET_LABEL = "**Not yet:**"

# notImplemented's wording in cmd/validate-intent/main.go. A refusal is exit 2
# carrying this; any other exit 2 (a usage error, an unloadable schema) is not
# a refusal and must not be read as one. run_parity.sh's `assert_not_refused`
# keys off the same substring.
REFUSAL_MARKER = "not implemented in the Go port"

# An item that means "this list is deliberately empty", so the last surface can
# leave the "Not yet" list without the block becoming unparseable.
EMPTY_LIST_MARKER = ("none", "nothing")


class Surface(object):
    """One named surface, the words the README may call it, and the argument
    vector that settles whether it is implemented.

    argv is run from the repo root with stdin closed. It only has to reach the
    dispatch in main.go's `run`: whether the run then passes or fails over the
    corpus is irrelevant here, and deliberately so -- this check is about which
    surfaces exist, and coupling it to fixture outcomes would make it fail for
    reasons the README has nothing to do with.
    """

    def __init__(self, key, aliases, argv):
        self.key = key
        self.aliases = aliases
        self.argv = argv


SURFACES = (
    Surface(
        key="adopter FILE... mode",
        aliases=("adopter (file...) mode", "file... (adopter) mode", "adopter file... mode"),
        argv=("examples/*.json",),
    ),
    Surface(
        key="-h/--help",
        aliases=("-h/--help", "-h / --help", "--help"),
        argv=("--help",),
    ),
    Surface(
        key="self-test mode",
        aliases=("self-test mode", "self-test", "self-test (bare invocation)"),
        # No arguments at all -- the bare invocation IS the self-test.
        argv=(),
    ),
    Surface(
        key="--source",
        aliases=("--source", "--source mode", "in-source (--source) mode"),
        argv=("--source", "examples/sources/*"),
    ),
    Surface(
        key="--source --json",
        aliases=("--source --json", "--json for --source", "--source --json mode"),
        argv=("--source", "examples/sources/*", "--json"),
    ),
    Surface(
        key="recursive ** globs",
        aliases=("recursive ** globs", "recursive ** glob", "recursive globs (**)"),
        argv=("examples/**/*.json",),
    ),
    Surface(
        key="stdin (-)",
        aliases=("stdin (-)", "stdin", "stdin mode (-)"),
        argv=("-",),
    ),
    Surface(
        key="--json for adopter (FILE...) mode",
        aliases=(
            "--json for adopter (file...) mode",
            "--json for adopter mode",
            "--json for adopter file... mode",
        ),
        argv=("--json", "examples/*.json"),
    ),
)


def build_alias_index():
    """Alias -> Surface, refusing an ambiguous or unsplittable vocabulary.

    Two surfaces sharing an alias would make the mapping depend on iteration
    order, and an alias containing a comma would be torn in half by the list
    splitter. Both are bugs in this file rather than in the README, so they
    abort rather than being reported as a README problem.
    """
    index = {}
    for surface in SURFACES:
        if not surface.aliases:
            abort("surface %r declares no aliases, so the README can never name it" % surface.key)
        for alias in surface.aliases:
            if "," in alias:
                abort(
                    "alias %r (surface %r) contains a comma; the list splitter "
                    "would tear it in half" % (alias, surface.key)
                )
            if normalise(alias) != alias:
                abort(
                    "alias %r (surface %r) is not in normalised form (%r)"
                    % (alias, surface.key, normalise(alias))
                )
            if alias in index and index[alias] is not surface:
                abort(
                    "alias %r is claimed by both %r and %r"
                    % (alias, index[alias].key, surface.key)
                )
            index[alias] = surface
    return index


def normalise(text):
    """Reduce a README list item to a comparable form.

    Backticks are markdown, not meaning. Case and internal whitespace are free,
    which is what lets a list wrap across lines. A leading conjunction from an
    Oxford-comma'd tail is dropped.

    The trailing sentence period is dropped with a lookbehind rather than
    rstrip('.'), because several surfaces are named with an ellipsis and
    `FILE...` must not be shortened to `FILE`.
    """
    text = text.replace("`", "").replace("*(", "(")
    text = re.sub(r"\s+", " ", text).strip().lower()
    text = re.sub(r"^(?:and|or)\s+", "", text)
    text = re.sub(r"(?<!\.)\.$", "", text).strip()
    return text


class Problem(object):
    """A finding, tagged so self_check can require the RIGHT one.

    Asserting only that a wrong README produces "a problem" would pass on a
    parser that reported every README as broken, which is a falsifier that
    cannot tell the two apart -- so the controls match on `kind` and `key`.
    """

    def __init__(self, kind, key, message):
        self.kind = kind
        self.key = key
        self.message = message


def extract_list(lines, label):
    """The text following `label`, up to the first blank line or next bold label.

    Returns (blob, line_no) or (None, None) when the label is absent.
    """
    for index, line in enumerate(lines):
        if not line.startswith(label):
            continue
        chunk = [line[len(label):]]
        for follow in lines[index + 1:]:
            if not follow.strip() or follow.startswith("**"):
                break
            chunk.append(follow)
        return " ".join(chunk), index + 1
    return None, None


def split_items(blob):
    return [item for item in (normalise(part) for part in blob.split(",")) if item]


def parse(readme_text, problems):
    """Return {surface_key: list_name} for every surface the README names."""
    lines = readme_text.splitlines()
    placement = {}
    aliases = build_alias_index()
    total_items = 0

    for label, list_name in ((IMPLEMENTED_LABEL, "Implemented so far"),
                             (NOT_YET_LABEL, "Not yet")):
        blob, line_no = extract_list(lines, label)
        if blob is None:
            problems.append(Problem(
                "missing-label", label,
                "%s carries no %s line.\n"
                "        This checker finds the Go-port status block by that\n"
                "        exact label. A reworded or relocated block leaves it\n"
                "        with nothing to check, which must be a failure rather\n"
                "        than a quiet pass." % (README, label)))
            continue

        for item in split_items(blob):
            if item in EMPTY_LIST_MARKER:
                continue
            total_items += 1
            surface = aliases.get(item)
            if surface is None:
                problems.append(Problem(
                    "unrecognised", item,
                    '%s:%d names "%s" under %s, and this checker has no\n'
                    "        probe for it, so the claim is unverified.\n"
                    "        Add the wording to that surface's aliases in %s, or\n"
                    "        add a Surface with the argument vector that would\n"
                    "        settle it."
                    % (README, line_no, item, label, os.path.basename(__file__))))
                continue
            if surface.key in placement:
                problems.append(Problem(
                    "duplicate", surface.key,
                    '%s names "%s" in both lists (%s and %s).\n'
                    "        It cannot be implemented and not implemented at\n"
                    "        the same time."
                    % (README, surface.key, placement[surface.key], list_name)))
                continue
            placement[surface.key] = list_name

    if total_items == 0:
        problems.append(Problem(
            "empty-lists", None,
            "%s's Go-port status block parsed to ZERO surfaces.\n"
            "        Every probe below is then unreachable and this check would\n"
            "        report success having run nothing. Either the block moved,\n"
            "        or the item formatting changed -- fix the parser, do not\n"
            "        accept the silence." % README))

    return placement


def evaluate(readme_text, outcomes):
    """Compare the README's claims against recorded probe outcomes.

    `outcomes` is {surface_key: (refused, exit_code, first_stderr_line)}. Kept
    separate from the probing so self_check can drive this same code with
    synthetic READMEs and the real binary's behaviour.
    """
    problems = []
    placement = parse(readme_text, problems)

    for surface in SURFACES:
        list_name = placement.get(surface.key)
        if list_name is None:
            problems.append(Problem(
                "uncovered", surface.key,
                '"%s" is named in NEITHER list of %s.\n'
                "        Deleting a surface from the block would otherwise be a\n"
                "        way to silence a disagreement about it. Every surface\n"
                "        this checker knows how to probe must be claimed one way\n"
                "        or the other." % (surface.key, README)))
            continue

        refused, exit_code, stderr_line = outcomes[surface.key]
        wants_refusal = list_name == "Not yet"
        if refused == wants_refusal:
            continue

        argv = " ".join(surface.argv) or "(no arguments)"
        if wants_refusal:
            detail = ("but the binary ran it: exit %d, and stderr does not carry\n"
                      "        the port's \"%s\" refusal.\n"
                      "        The surface shipped and the README was not updated."
                      % (exit_code, REFUSAL_MARKER))
        else:
            detail = ("but the binary refuses it: exit %d\n"
                      "          %s\n"
                      "        The README promises a surface that is not there."
                      % (exit_code, stderr_line or "(no stderr)"))
        problems.append(Problem(
            "wrong-list", surface.key,
            '%s lists "%s" under %s,\n'
            "        %s\n"
            "        probe: validate-intent-go %s"
            % (README, surface.key, list_name, detail, argv)))

    return problems


# --------------------------------------------------------------------------- #
# probing the binary
# --------------------------------------------------------------------------- #

def cannot_compare(message):
    sys.stderr.write("check_readme_surfaces: %s\n" % message)
    sys.stderr.write("check_readme_surfaces: nothing was compared "
                     "— this is NOT a pass\n")
    sys.exit(2)


def abort(message):
    sys.stderr.write("check_readme_surfaces: %s\n" % message)
    sys.exit(2)


def resolve_binary():
    """The built port, or exit 2.

    bin/validate-intent-go is a build artifact ignored by .gitignore, so it is
    routinely absent on a fresh checkout. Build it when a toolchain is around;
    otherwise there is no port to interrogate and the honest answer is "could
    not check".
    """
    override = os.environ.get("VALIDATE_INTENT_GO")
    target = override or os.path.join(REPO_ROOT, "bin", "validate-intent-go")

    if os.path.isfile(target) and os.access(target, os.X_OK):
        return target
    if override:
        cannot_compare("VALIDATE_INTENT_GO=%s is not an executable file" % override)

    go = os.environ.get("GO", "go")
    if shutil.which(go) is None:
        cannot_compare(
            "no binary at %s and no Go toolchain on PATH (set GO=/path/to/go, or\n"
            "                       VALIDATE_INTENT_GO=/path/to/validate-intent-go).\n"
            "                       That path is a build artifact (.gitignore), not a "
            "committed file." % target)

    result = subprocess.run(
        [go, "build", "-o", target, "./cmd/validate-intent"],
        cwd=REPO_ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if result.returncode != 0:
        sys.stderr.write(result.stdout.decode("utf-8", "replace"))
        cannot_compare("go build failed")
    return target


def probe(binary, surface):
    """Run one surface and classify it refused / not refused."""
    result = subprocess.run(
        [binary] + list(surface.argv),
        cwd=REPO_ROOT, stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    stderr = result.stderr.decode("utf-8", "replace")
    first_line = stderr.splitlines()[0] if stderr.strip() else ""
    refused = result.returncode == 2 and REFUSAL_MARKER in stderr
    return refused, result.returncode, first_line


# --------------------------------------------------------------------------- #
# the falsifier's own falsifier
# --------------------------------------------------------------------------- #

def synth_readme(implemented, not_yet, extra_items=()):
    """A synthetic status block in the shape this parser expects.

    Built from surface KEYS via each surface's canonical alias, so the controls
    exercise the real parser rather than a hand-typed approximation of it.
    """
    def render(keys):
        names = [by_key(k).aliases[0] for k in keys] + list(extra_items)
        return ", ".join(names) if names else EMPTY_LIST_MARKER[0]

    return (
        "## The Go port\n"
        "\n"
        "%s %s.\n"
        "\n"
        "%s %s.\n"
        "\n"
        "Trailing prose, which is not a list item and must not be read as one.\n"
        % (IMPLEMENTED_LABEL, render(implemented), NOT_YET_LABEL, render(not_yet)))


def by_key(key):
    for surface in SURFACES:
        if surface.key == key:
            return surface
    abort("no surface keyed %r" % key)


def kinds(problems):
    return sorted({(p.kind, p.key) for p in problems})


def self_check(outcomes):
    """Prove this checker can go red before believing it when it goes green.

    Every control is derived from what the binary ACTUALLY did this run, so the
    calibration stays valid as surfaces ship: the positive control is the
    truthful README for today's binary, and each negative control is that same
    README with exactly one thing wrong.
    """
    truth_implemented = [s.key for s in SURFACES if not outcomes[s.key][0]]
    truth_not_yet = [s.key for s in SURFACES if outcomes[s.key][0]]
    failures = []

    def expect(label, text, want_kind, want_key):
        problems = evaluate(text, outcomes)
        if (want_kind, want_key) not in kinds(problems):
            failures.append(
                "%s: expected a %r problem for %r, got %s"
                % (label, want_kind, want_key, kinds(problems) or "no problems at all"))

    def expect_clean(label, text):
        problems = evaluate(text, outcomes)
        if problems:
            failures.append(
                "%s: expected NO problems, got %s"
                % (label, "; ".join(p.message.splitlines()[0] for p in problems)))

    # Positive control first. Without it the negative controls below could all
    # be passing because this checker rejects every README it is handed, which
    # would be a falsifier that has stopped discriminating.
    expect_clean("positive control (a truthful block)",
                 synth_readme(truth_implemented, truth_not_yet))

    # One negative control per surface: put it in the wrong list and require
    # the disagreement to be reported AGAINST THAT SURFACE. This is the shape
    # of the defect that produced this ticket -- self-test, `--source`,
    # `--source --json` and recursive `**` globs each shipped while the README
    # went on listing them as unavailable -- reproduced for every surface
    # rather than only the four that historically drifted.
    for surface in SURFACES:
        flipped_impl = [k for k in truth_implemented if k != surface.key]
        flipped_not_yet = [k for k in truth_not_yet if k != surface.key]
        if outcomes[surface.key][0]:
            flipped_impl.append(surface.key)
        else:
            flipped_not_yet.append(surface.key)
        expect("negative control (%s in the wrong list)" % surface.key,
               synth_readme(flipped_impl, flipped_not_yet),
               "wrong-list", surface.key)

    # A surface quietly dropped from the block.
    dropped = SURFACES[0].key
    expect("negative control (%s named in neither list)" % dropped,
           synth_readme([k for k in truth_implemented if k != dropped],
                        [k for k in truth_not_yet if k != dropped]),
           "uncovered", dropped)

    # The empty work set: both lists present, both empty. Every probe becomes
    # unreachable, which must read as a failure and not as a clean sheet.
    expect("negative control (both lists empty)",
           synth_readme([], []), "empty-lists", None)

    # A claim with no experiment behind it.
    expect("negative control (a surface with no probe)",
           synth_readme(truth_implemented, truth_not_yet,
                        extra_items=("interpretive dance mode",)),
           "unrecognised", "interpretive dance mode")

    # Both lists at once.
    both = truth_implemented[0] if truth_implemented else truth_not_yet[0]
    expect("negative control (%s in both lists)" % both,
           synth_readme(truth_implemented, truth_not_yet + [both]),
           "duplicate", both)

    # A block this parser cannot find at all.
    expect("negative control (the block is gone)",
           "## The Go port\n\nNo status block here at all.\n",
           "missing-label", IMPLEMENTED_LABEL)

    if failures:
        sys.stderr.write(
            "\ncheck_readme_surfaces: THIS CHECKER IS NOT WORKING.\n"
            "  Its self-calibration failed, so a green result from it would mean\n"
            "  nothing. Fix the checker before trusting the README.\n\n")
        for failure in failures:
            sys.stderr.write("  %s\n\n" % failure)
        sys.exit(1)

    return 1 + len(SURFACES) + 5


def main():
    binary = resolve_binary()

    outcomes = {}
    for surface in SURFACES:
        outcomes[surface.key] = probe(binary, surface)

    controls = self_check(outcomes)

    with open(os.path.join(REPO_ROOT, README), encoding="utf-8") as handle:
        readme_text = handle.read()

    problems = evaluate(readme_text, outcomes)
    if problems:
        sys.stderr.write(
            "\ncheck_readme_surfaces: %d disagreement(s) between %s and %s\n\n"
            % (len(problems), README, os.path.relpath(binary, REPO_ROOT)))
        for problem in problems:
            sys.stderr.write("  %s\n\n" % problem.message)
        if any(p.kind == "unrecognised" for p in problems):
            sys.stderr.write(
                "  Wordings this checker recognises:\n%s\n\n"
                % "\n".join("    %s" % alias for alias in sorted(build_alias_index())))
        return 1

    print("README Go-port surfaces OK (%d surfaces probed against %s, "
          "%d self-checks)"
          % (len(SURFACES), os.path.relpath(binary, REPO_ROOT), controls))
    return 0


if __name__ == "__main__":
    sys.exit(main())
