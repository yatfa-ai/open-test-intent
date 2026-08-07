#!/usr/bin/env python3
"""Verify the two numbering schemes run_parity.sh owns are cited accurately.

The file carries TWO independent numbering schemes, and both are cited from
other files:

  * SECTIONS -- "# 7. --help" and its divider rule. Checked by name as well as
    number; see "WHY IT CHECKS THE NAME AND NOT JUST THE NUMBER" below.
  * EXCLUDED GROUPS -- the "#   5. `--version`." entries in the "Excluded cases,
    and why" header block, cited as "excluded group 5". Checked for existence
    and against the spelled-out count in the header prose; see "THE SECOND
    SCHEME" below.

The script keeps its original name because run_parity.sh invokes it by path and
because sections are still the bulk of what it checks.

WHY THIS EXISTS
===============

tests/parity/run_parity.sh is organised into numbered sections, and other files
-- including cmd/validate-intent/main.go, a different language in a different
directory -- cite those numbers to say "the evidence for this claim lives over
there, go look".

Those citations drift. The numbering is not stable: each slice of the Go port
inserts sections in the middle, renumbering everything below. A citation written
against slice 1's layout silently starts pointing at an unrelated section once
slice 2 lands. This has now happened four times in this file's history, twice in
the very commit that was fixing an earlier instance of it.

The failure is not that the reader gets an error. It is worse than that: the
cited section EXISTS and is full of plausible-looking cases, so a reader who
follows the pointer finds a suite that simply does not corroborate the claim,
and cannot tell whether the claim is wrong or the pointer is. The comments that
carry these citations are precisely the ones asking to be believed -- main.go's
says "do not hoist this refusal back above LoadSchema(), the harness proves it
breaks parity" -- so a pointer that does not corroborate is a comment that
quietly loses its authority.

WHY IT CHECKS THE NAME AND NOT JUST THE NUMBER
==============================================

Checking that section N merely EXISTS would pass on all four historical
defects -- every one of them pointed at a real section. That check would report
success while verifying nothing, which is this project's house defect (see the
"vacuous green" note in the knowledge base): a gate whose empty work set reads
as a clean bill of health.

So the contract is stronger, and deliberately annoying to satisfy: a citation
must carry the section's NAME as well as its number, and this script requires
the two to agree. A renumbering that moves a section now breaks the build
instead of rotting a comment, and a reader who hits a disagreement has the name
-- the durable half -- to recover the intent from.

WHAT ELSE IT REFUSES
====================

A line that reads like a section heading but carries no divider rule beneath it
defines nothing, and every check here is blind to it. run_parity.sh carried one
for three slices -- a stale "# 16." duplicate directly above the real "# 17."
for the same section -- so the file named that section by two different numbers
and nothing complained. That is now an error too: add the divider, or delete
the leftover.

THE SECOND SCHEME: EXCLUDED GROUPS
==================================

Sections are not the only numbers in this file that drift, and the group
numbering drifted first. Slice 6 (SPGD-141) added `--version` as excluded group
6, was rebased onto slice 4 (SPGD-123) -- which had retired the recursive-glob
group and freed the number 5 -- and renumbered it to 5 in four places and not in
two. The file's header arithmetic said five groups; two comments pointed at
group 6. Group 6 was nothing.

Section refs survived that same rebase intact, because THIS SCRIPT caught the
one that had gone stale. Groups had no such guard, which is the whole reason
they were the half that drifted. They have one now.

The contract here is weaker than the section contract, deliberately:

  * every cited group number must be DEFINED in the header block;
  * the group numbers must run 1..N with no gaps, so a retirement renumbers
    rather than leaving a hole that later reads as an available slot;
  * the spelled-out count in the header prose ("FIVE groups of inputs are
    deliberately NOT compared") must equal N.

It is weaker because group citations are prose -- "`--version` is a Go-only flag
(excluded group 5)" -- where the name is usually the surrounding sentence
already, so demanding a quoted name would only add ceremony to text that says it
better. Be clear about what that costs: this catches a citation pointing at a
group that does not EXIST, and it catches the hand-maintained count going stale.
It does NOT catch a citation that points at a real but wrong group. The count
check is what makes the pair worth having anyway -- a renumbering that shifts
groups without touching the count word fails here, and a renumbering that
touches both is one a human has already had to think about.

USAGE
=====

    python3 tests/parity/check_section_refs.py

Exits 0 when every citation agrees, 1 (with a diagnostic naming file, line,
cited name and actual heading) otherwise. Run by tests/parity/run_parity.sh as
a preflight, so it cannot be forgotten.
"""

import os
import re
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# The file that OWNS the numbering. Sections are defined here and nowhere else.
SECTION_FILE = os.path.join("tests", "parity", "run_parity.sh")

# Files allowed to cite section or excluded-group numbers. Adding a file here is
# the point at which you inherit the naming contract below.
#
# Keep this list complete rather than convenient. A file that cites a number and
# is not listed here is not "unchecked" in a way anyone notices -- the run still
# prints a green "cross-references OK" line, having simply not looked. That is
# this project's house defect wearing a checker's uniform, so the rule is: if it
# says "section N" or "excluded group N", it goes in this list.
CITING_FILES = [
    os.path.join("tests", "parity", "run_parity.sh"),
    os.path.join("tests", "parity", "globtree", "README.md"),
    os.path.join("cmd", "validate-intent", "main.go"),
    os.path.join("cmd", "validate-intent", "version.go"),
    os.path.join("cmd", "validate-intent", "version_test.go"),
    os.path.join("tests", "parity", "check_readme_surfaces.py"),
]

# A section heading: "# 7. OS-level failures", immediately followed by the
# divider rule. Requiring the divider is what keeps the EXCLUSIONS list at the
# top of run_parity.sh -- whose entries are indented "#   1. ..." -- from being
# mistaken for sections. Those are exclusion groups; they share the numbering
# space but not the meaning, and conflating them is its own way to be wrong.
# They get their own parser below rather than being ignored.
HEADING_RE = re.compile(r"^# (\d+)\. (.+?)\s*$")
DIVIDER_RE = re.compile(r"^# -{3,} #\s*$")

# An excluded-group definition: "#   5. `--version`." -- the indentation is what
# separates it from a section heading, which starts at "# 1.".
GROUP_DEF_RE = re.compile(r"^#   (\d+)\. (.+?)\s*$")

# The end of the group list. Everything after it describes groups that USED to
# exist; they are bulleted rather than numbered, but bounding the scan means a
# future slice can renumber the retired list without silently inventing groups.
GROUP_LIST_END_RE = re.compile(r"^#\s*RETIRED EXCLUSIONS\b")

# The hand-maintained count: "# FIVE groups of inputs are deliberately NOT
# compared against Python." The header carries a whole paragraph of arithmetic
# justifying this word across five slices, which is exactly why it is the part
# most worth checking mechanically.
GROUP_COUNT_RE = re.compile(r"^#\s*([A-Z]+) groups of inputs\b")
COUNT_WORDS = {
    "ZERO": 0, "ONE": 1, "TWO": 2, "THREE": 3, "FOUR": 4, "FIVE": 5,
    "SIX": 6, "SEVEN": 7, "EIGHT": 8, "NINE": 9, "TEN": 10,
}

# "excluded group 5", or the bare "group 5" the header paragraph's arithmetic
# uses. Anchored on the word so "grouped" and RE2's "(?  groups" cannot match.
GROUP_CITATION_RE = re.compile(r"\bgroup (\d+)\b", re.IGNORECASE)

# "section 7", then its name in double quotes. The name is allowed to be on a
# later line, and to wrap across several: these citations live inside comment
# blocks in two languages, and hard-wrapping at 80 columns is not optional.
# Both punctuations that read naturally are accepted -- section 7 ("name") and
# section 7, "name" -- because forcing one house style here would only add a
# second way for this check to fail on correct prose.
CITATION_RE = re.compile(r"section (\d+)", re.IGNORECASE)
NAME_RE = re.compile(r'[\s,]*\(?\s*"([^"]+)"')

# How far past "section N" to look for the quoted name before giving up. Long
# enough to clear a wrapped line and its comment prefix, short enough that an
# unrelated quoted string further down the paragraph cannot be mistaken for it.
NAME_LOOKAHEAD = 8


def normalise(text):
    """Collapse whitespace and case so a hard-wrapped citation compares equal
    to a single-line heading. Nothing else is stripped: punctuation and the
    em-dashes in these headings are load-bearing enough to keep comparing."""
    return re.sub(r"\s+", " ", text).strip().lower()


def read_lines(rel_path):
    with open(os.path.join(REPO_ROOT, rel_path), encoding="utf-8") as handle:
        return handle.read().splitlines()


def parse_sections(lines):
    """Return {number: (heading_text, line_no)} for the real sections."""
    sections = {}
    orphans = []
    for index, line in enumerate(lines):
        match = HEADING_RE.match(line)
        if not match:
            continue
        if index + 1 >= len(lines) or not DIVIDER_RE.match(lines[index + 1]):
            # A line that READS like a section heading but is not followed by
            # the divider rule. Historically this was skipped in silence, and
            # that silence is how run_parity.sh came to carry a stale duplicate
            # heading ("# 16. Go-side refusals -- schemas carrying a pattern
            # RE2 cannot reproduce") sitting directly above the real "# 17."
            # for the same section: the file said two different numbers named
            # that section, and the only thing stopping the wrong one from
            # being believed was that this script happened to ignore it.
            #
            # That is the same defect class this script exists for -- a comment
            # that has quietly stopped being true while still reading like
            # evidence -- so it is now an error rather than a `continue`. The
            # divider is cheap to add when the heading is real; when it is a
            # leftover, deleting it is the fix.
            orphans.append((index + 1, line))
            continue
        number, heading = match.group(1), match.group(2)
        if number in sections:
            fail_hard(
                "%s defines section %s twice (lines %d and %d)"
                % (SECTION_FILE, number, sections[number][1], index + 1)
            )
        sections[number] = (heading, index + 1)
    if orphans:
        fail_hard(
            "%s has %d line(s) that read as a section heading but carry no\n"
            "  divider rule beneath them, so they define nothing and are\n"
            "  invisible to every check below:\n%s\n"
            "  Add the '# ---- #' divider if the section is real, or delete\n"
            "  the line if it is a leftover from a renumbering."
            % (
                SECTION_FILE,
                len(orphans),
                "\n".join("    %s:%d  %s" % (SECTION_FILE, no, text) for no, text in orphans),
            )
        )
    return sections


def strip_comment_prefix(line):
    """Turn a comment line into its prose, so a citation that wraps across
    lines can be read as one string."""
    return re.sub(r"^\s*(//|#)\s?", "", line)


def parse_groups(lines):
    """Return ({number: (title, line_no)}, declared_count, count_line_no) for
    the excluded-group list in the header block.

    The scan stops at RETIRED EXCLUSIONS. Retired entries are bulleted rather
    than numbered today, so bounding it changes nothing right now -- it is here
    so that a future slice numbering the retired list cannot silently promote
    those numbers into live groups that every citation would then resolve
    against."""
    groups = {}
    declared = None
    declared_line = 0
    for index, line in enumerate(lines):
        if GROUP_LIST_END_RE.match(line):
            break

        count_match = GROUP_COUNT_RE.match(line)
        if count_match and declared is None:
            word = count_match.group(1)
            if word not in COUNT_WORDS:
                fail_hard(
                    '%s:%d spells the group count "%s", which this script '
                    "cannot read.\n"
                    "        Known words: %s"
                    % (SECTION_FILE, index + 1, word, ", ".join(COUNT_WORDS))
                )
            declared = COUNT_WORDS[word]
            declared_line = index + 1
            continue

        match = GROUP_DEF_RE.match(line)
        if not match:
            continue
        number, title = match.group(1), match.group(2)
        if number in groups:
            fail_hard(
                "%s defines excluded group %s twice (lines %d and %d)"
                % (SECTION_FILE, number, groups[number][1], index + 1)
            )
        groups[number] = (title, index + 1)
    return groups, declared, declared_line


def check_group_numbering(groups, declared, declared_line, problems):
    """The two things about the group list itself that go stale on a rebase:
    the numbers running 1..N, and the spelled-out count agreeing with N."""
    numbers = sorted(int(n) for n in groups)
    expected = list(range(1, len(numbers) + 1))
    if numbers != expected:
        problems.append(
            "%s: the excluded groups are numbered %s.\n"
            "        They must run %s with no gaps and no repeats. A hole\n"
            "        left by a retired group reads as an available slot to\n"
            "        the next slice, and every citation below a gap is\n"
            "        ambiguous about which side of it it meant."
            % (
                SECTION_FILE,
                ", ".join(str(n) for n in numbers),
                "1..%d" % len(numbers),
            )
        )

    if declared != len(groups):
        problems.append(
            "%s:%d says there are %s excluded groups; %d are defined (%s).\n"
            "        This count is maintained by hand, in a paragraph that\n"
            "        spells out its own arithmetic across five slices. Fix\n"
            "        the word and the arithmetic together -- a count that\n"
            "        disagrees with the list is how a citation ends up\n"
            "        pointing at a group nobody wrote."
            % (
                SECTION_FILE,
                declared_line,
                declared,
                len(groups),
                ", ".join(str(n) for n in numbers),
            )
        )


def check_file_groups(rel_path, groups, problems):
    """Every "excluded group N" citation must name a group that exists."""
    count = 0
    for index, line in enumerate(read_lines(rel_path)):
        for match in GROUP_CITATION_RE.finditer(line):
            count += 1
            number = match.group(1)
            if number in groups:
                continue
            problems.append(
                '%s:%d cites "group %s", which is not an excluded group.\n'
                "        Groups defined in %s: %s\n"
                "        Trust the surrounding sentence: it almost always\n"
                "        names the surface (`--version`, `--source`, ...).\n"
                "        Find the group that still describes it and use its\n"
                "        number, rather than adding a group to match."
                % (
                    rel_path,
                    index + 1,
                    number,
                    SECTION_FILE,
                    ", ".join(
                        "%s (%s)" % (n, groups[n][0])
                        for n in sorted(groups, key=int)
                    ),
                )
            )
    return count


def fail_hard(message):
    sys.stderr.write("check_section_refs: %s\n" % message)
    sys.exit(1)


def check_file(rel_path, sections, problems):
    lines = read_lines(rel_path)
    for index, line in enumerate(lines):
        for match in CITATION_RE.finditer(line):
            number = match.group(1)
            line_no = index + 1

            # The citation's tail: the rest of this line plus the following
            # few, comment markers removed, so a wrapped name reads as one
            # string.
            tail = line[match.end():]
            for follow in lines[index + 1:index + 1 + NAME_LOOKAHEAD]:
                tail += " " + strip_comment_prefix(follow)

            name_match = NAME_RE.match(tail)
            if not name_match:
                problems.append(
                    '%s:%d cites "section %s" without naming it.\n'
                    "        Every citation must carry the section name, e.g.\n"
                    '        section %s ("%s").\n'
                    "        A bare number cannot be checked, and silently\n"
                    "        rots the next time the harness is renumbered."
                    % (
                        rel_path,
                        line_no,
                        number,
                        number,
                        sections.get(number, ("<no such section>", 0))[0],
                    )
                )
                continue

            cited_name = name_match.group(1)

            if number not in sections:
                problems.append(
                    '%s:%d cites "section %s", which does not exist.\n'
                    "        Sections defined in %s: %s"
                    % (
                        rel_path,
                        line_no,
                        number,
                        SECTION_FILE,
                        ", ".join(sorted(sections, key=int)),
                    )
                )
                continue

            heading, heading_line = sections[number]
            if normalise(cited_name) not in normalise(heading):
                problems.append(
                    "%s:%d cites the wrong section.\n"
                    '        cited:  section %s ("%s")\n'
                    '        actual: section %s is "%s" (%s:%d)\n'
                    "        Trust the NAME: find the section that still\n"
                    "        carries it and renumber the citation, rather\n"
                    "        than renaming the section to match."
                    % (
                        rel_path,
                        line_no,
                        number,
                        cited_name,
                        number,
                        heading,
                        SECTION_FILE,
                        heading_line,
                    )
                )


def main():
    lines = read_lines(SECTION_FILE)

    sections = parse_sections(lines)
    if not sections:
        # An empty section table would make every "does it exist" test
        # vacuously... absent, and every name test unreachable. If the heading
        # format ever changes, this script must fail loudly rather than pass by
        # having nothing to say.
        fail_hard(
            "found no section headings in %s -- the heading format must have "
            "changed, so this check is no longer checking anything" % SECTION_FILE
        )

    groups, declared, declared_line = parse_groups(lines)
    # The same guard, for the same reason, on the same failure mode: a reworded
    # header block that stops matching GROUP_DEF_RE would leave every citation
    # unresolvable and every one of them reported -- but a header block that
    # loses the LIST entirely, or the count sentence, would leave nothing to
    # check and print a green line about it.
    if not groups:
        fail_hard(
            "found no excluded-group definitions in %s -- the header block's "
            'indented "#   N. ..." format must have changed, so the group half '
            "of this check is no longer checking anything" % SECTION_FILE
        )
    if declared is None:
        fail_hard(
            '%s no longer states how many excluded groups there are ("FIVE '
            'groups of inputs are deliberately NOT compared ..."). That '
            "sentence is the only thing the group count can be checked "
            "against; without it the count is unverifiable." % SECTION_FILE
        )

    problems = []
    check_group_numbering(groups, declared, declared_line, problems)

    citations = 0
    group_citations = 0
    for rel_path in CITING_FILES:
        check_file(rel_path, sections, problems)
        group_citations += check_file_groups(rel_path, groups, problems)
        citations += sum(
            len(CITATION_RE.findall(line)) for line in read_lines(rel_path)
        )

    # Third instance of the same guard. Every citation could be correct, or the
    # phrasing could have moved on from "excluded group N" -- from here those
    # look identical, and only one of them is a reason to print OK.
    if group_citations == 0:
        fail_hard(
            "found no excluded-group citations across %d files -- the "
            '"group N" phrasing must have changed, so nothing was checked'
            % len(CITING_FILES)
        )

    if problems:
        sys.stderr.write(
            "\ncheck_section_refs: %d stale cross-reference(s)\n\n" % len(problems)
        )
        for problem in problems:
            sys.stderr.write("  %s\n\n" % problem)
        return 1

    print(
        "cross-references OK (%d section citations and %d group citations "
        "across %d files; %d sections, %d excluded groups)"
        % (
            citations,
            group_citations,
            len(CITING_FILES),
            len(sections),
            len(groups),
        )
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
