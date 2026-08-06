#!/usr/bin/env python3
"""Verify that every "section N" cross-reference names the section it points at.

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

# Files allowed to cite section numbers. Adding a file here is the point at
# which you inherit the naming contract below.
CITING_FILES = [
    os.path.join("tests", "parity", "run_parity.sh"),
    os.path.join("cmd", "validate-intent", "main.go"),
]

# A section heading: "# 7. OS-level failures", immediately followed by the
# divider rule. Requiring the divider is what keeps the EXCLUSIONS list at the
# top of run_parity.sh -- whose entries are indented "#   1. ..." -- from being
# mistaken for sections. Those are exclusion groups; they share the numbering
# space but not the meaning, and conflating them is its own way to be wrong.
HEADING_RE = re.compile(r"^# (\d+)\. (.+?)\s*$")
DIVIDER_RE = re.compile(r"^# -{3,} #\s*$")

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
    for index, line in enumerate(lines):
        match = HEADING_RE.match(line)
        if not match:
            continue
        if index + 1 >= len(lines) or not DIVIDER_RE.match(lines[index + 1]):
            continue
        number, heading = match.group(1), match.group(2)
        if number in sections:
            fail_hard(
                "%s defines section %s twice (lines %d and %d)"
                % (SECTION_FILE, number, sections[number][1], index + 1)
            )
        sections[number] = (heading, index + 1)
    return sections


def strip_comment_prefix(line):
    """Turn a comment line into its prose, so a citation that wraps across
    lines can be read as one string."""
    return re.sub(r"^\s*(//|#)\s?", "", line)


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
    sections = parse_sections(read_lines(SECTION_FILE))
    if not sections:
        # An empty section table would make every "does it exist" test
        # vacuously... absent, and every name test unreachable. If the heading
        # format ever changes, this script must fail loudly rather than pass by
        # having nothing to say.
        fail_hard(
            "found no section headings in %s -- the heading format must have "
            "changed, so this check is no longer checking anything" % SECTION_FILE
        )

    problems = []
    citations = 0
    for rel_path in CITING_FILES:
        before = len(problems)
        check_file(rel_path, sections, problems)
        del before
    for rel_path in CITING_FILES:
        citations += sum(
            len(CITATION_RE.findall(line)) for line in read_lines(rel_path)
        )

    if problems:
        sys.stderr.write(
            "\ncheck_section_refs: %d stale section cross-reference(s)\n\n"
            % len(problems)
        )
        for problem in problems:
            sys.stderr.write("  %s\n\n" % problem)
        return 1

    print(
        "section cross-references OK (%d citations across %d files, "
        "%d sections)" % (citations, len(CITING_FILES), len(sections))
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
