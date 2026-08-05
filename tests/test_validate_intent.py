#!/usr/bin/env python3
"""Regression tests for the pure validation core of ``bin/validate-intent``.

Scope: the pure functions ``validate`` / ``_type_matches`` / ``_type_of``. These
are the reference implementation of the OpenTestIntent draft-07 subset, so a
silent accept/reject bug here would cascade across every adopter.

``validate`` deliberately implements a *superset* of the keywords the shipped
schema uses ("the common neighbours so it keeps working if the schema grows").
Those extra branches — ``pattern``, ``maxLength``, ``minItems``/``maxItems``,
``minimum``/``maximum``, list-form ``type``, ``additionalProperties`` as a
schema, ``items`` recursion, and the bool/int guard — cannot be reached through
the shipped schema and its fixtures, so they are exercised here with synthetic
mini-schemas.

Deliberately **out of scope**: the CLI surface (argv parsing, stdin, exit codes,
file reads). This file locks the stable pure core only — plus ``_expand_files``,
the one path-layer helper that is itself pure (pattern in, list of paths out)
and is shared by every glob-expanding mode, so a regression there silently
breaks all of them at once. ``OneGlobExpanderTest`` is the narrow exception to
the CLI exclusion: the helper is only worth anything if every mode actually
*routes* through it, and twice now a mode was left behind on a raw
``glob.glob``, so that wiring is pinned here too. ``RunOverPatternsTest`` covers
the other half of that path layer for the same reason: ``_run_over_patterns`` is
where the modes' expansion, no-match diagnostic and exit-code aggregation were
centralized, so it is now the single place a silent regression would reach every
file-checking mode at once. ``NonUtf8InputTest`` is pinned here on the same
grounds: the readers open adopter input as strict UTF-8, so a mis-encoded file
raises ``UnicodeDecodeError`` — a ``ValueError``, caught by neither ``OSError``
nor ``json.JSONDecodeError``. Uncaught, it does not merely print badly: it
unwinds ``_run_over_patterns`` and every file sorting after the bad one goes
unchecked behind an exit code that reads as "one file failed". These tests
assert the readers *report* the bad encoding instead of raising it.
``JsonOutputTest`` is admitted on the same terms from the opposite direction:
``--json`` is not a nicer rendering of the human report, it is the *only*
machine-readable surface the tool has, so its document shape, its per-finding
``kind``, its exit-code parity with text mode, and its promise that the default
output did not move are the contract every non-Python adopter writes against.
All of it lives in the CLI layer, so nothing below the CLI can pin it — and a
regression is silent by construction: the document still parses, it just says
something else.

Zero dependencies, like the validator itself — stdlib ``unittest`` only.

Run:
    python3 tests/test_validate_intent.py
    python3 -m unittest discover -s tests
"""

import contextlib
import importlib.util
import io
import json
import os
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader

# Importing the validator would otherwise drop a ``bin/__pycache__`` next to the
# shipped script — keep the tree an adopter clones pristine.
sys.dont_write_bytecode = True

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
VALIDATOR_PATH = os.path.join(REPO_ROOT, "bin", "validate-intent")


def _load_validator():
    """Import the extensionless ``bin/validate-intent`` as a module.

    ``spec_from_file_location`` cannot infer a loader for a file with no ``.py``
    extension (it returns None), so name the ``SourceFileLoader`` explicitly.
    ``exec_module`` — not the deprecated ``load_module`` — runs the module; the
    validator guards its CLI behind ``if __name__ == "__main__"``, so importing
    it executes nothing but definitions.
    """
    loader = SourceFileLoader("validate_intent", VALIDATOR_PATH)
    spec = importlib.util.spec_from_loader(loader.name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


validate_intent = _load_validator()

validate = validate_intent.validate
_type_matches = validate_intent._type_matches
_type_of = validate_intent._type_of
_expand_files = validate_intent._expand_files
_run_over_patterns = validate_intent._run_over_patterns
check_file = validate_intent.check_file
run_stdin = validate_intent.run_stdin
main = validate_intent.main


class ExpandFilesTest(unittest.TestCase):
    """``_expand_files`` — glob expansion shared by every file-reading mode.

    A bare ``glob.glob`` also yields *directories*, which the readers then try to
    ``open()`` — turning ``intents/*`` into a FAIL per subdirectory with a bogus
    "could not read/parse JSON" diagnostic. Every mode must expand through here.
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name

        self.intent = os.path.join(self.root, "an-intent.json")
        with open(self.intent, "w", encoding="utf-8") as handle:
            handle.write("{}\n")

        self.subdir = os.path.join(self.root, "a-subdir")
        os.mkdir(self.subdir)
        self.nested = os.path.join(self.subdir, "nested.json")
        with open(self.nested, "w", encoding="utf-8") as handle:
            handle.write("{}\n")

    def test_directories_are_dropped_and_files_are_kept(self):
        # The regression: `*` matches both an-intent.json and a-subdir/.
        matched = _expand_files(os.path.join(self.root, "*"))
        self.assertEqual(matched, [self.intent])
        self.assertNotIn(self.subdir, matched)

    def test_results_are_sorted(self):
        for name in ("z.json", "a.json", "m.json"):
            with open(os.path.join(self.root, name), "w", encoding="utf-8") as handle:
                handle.write("{}\n")
        matched = _expand_files(os.path.join(self.root, "*.json"))
        self.assertEqual(matched, sorted(matched))

    def test_recursive_glob_still_reaches_nested_files(self):
        # `recursive=True` is what makes `**` meaningful; the isfile filter must
        # not cost us the nested match, only the directory entries.
        matched = _expand_files(os.path.join(self.root, "**", "*.json"))
        self.assertIn(self.nested, matched)

    def test_pattern_matching_only_directories_expands_to_nothing(self):
        # Empty — NOT a silent pass: each caller turns this into the loud
        # "error: no file(s) match ..." branch with a non-zero exit code.
        self.assertEqual(_expand_files(self.subdir), [])

    def test_pattern_matching_nothing_expands_to_nothing(self):
        self.assertEqual(_expand_files(os.path.join(self.root, "*.nope")), [])


class OneGlobExpanderTest(unittest.TestCase):
    """Every mode routes its globs through ``_expand_files`` — no exceptions.

    ``_expand_files`` only protects the modes that actually call it, and the
    back-fill has now been incomplete twice (the ``--source`` mode arrived after
    the helper; the later fix routed adopter and ``--source`` but left
    ``run_self_test`` on a raw ``glob.glob``, so the *same* glob filtered a
    directory in one mode and FAILed on it in another). Pinned two ways:
    structurally, one expander in the script; behaviourally, self-test agrees
    with adopter mode about directories.
    """

    def test_glob_glob_is_called_in_exactly_one_place(self):
        # Deliberately a textual, line-wise scan of the script: the point is to
        # fail loudly the moment a *new* `glob.glob(` is typed anywhere in the
        # file, including in a comment or docstring that would mislead the next
        # reader. Do not "fix" this into an AST walk that only counts real calls
        # — the false positives are the feature, and a call split across lines
        # is not a shape this file uses.
        with open(VALIDATOR_PATH, encoding="utf-8") as handle:
            calls = [line.strip() for line in handle if "glob.glob(" in line]
        self.assertEqual(
            len(calls),
            1,
            "expected the only glob.glob call to be _expand_files'; found: %r" % (calls,),
        )
        # ...and it is the helper's own — recursive, so `**` keeps working.
        self.assertIn("recursive=True", calls[0])

    def test_self_test_filters_a_directory_matching_an_examples_glob(self):
        """A directory named ``*.json`` is skipped, not reported as a broken fixture."""
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)

        fixture = os.path.join(tmp.name, "an-intent.json")
        with open(fixture, "w", encoding="utf-8") as handle:
            json.dump(
                {
                    "entity": "Order",
                    "action": "checkout",
                    "behavior": "returns 402 payment required on expired card",
                    "layer": "request",
                },
                handle,
            )
        # The regression: `*.json` matches this directory too.
        os.mkdir(os.path.join(tmp.name, "legacy.json"))

        empty = os.path.join(tmp.name, "empty")
        os.mkdir(empty)
        globs = {
            "VALID_EXAMPLES_GLOB": os.path.join(tmp.name, "*.json"),
            "INVALID_EXAMPLES_GLOB": os.path.join(empty, "*.json"),
            "VALID_SOURCES_GLOB": os.path.join(empty, "*"),
            "INVALID_SOURCES_GLOB": os.path.join(empty, "*"),
        }
        for name, value in globs.items():
            original = getattr(validate_intent, name)
            self.addCleanup(setattr, validate_intent, name, original)
            setattr(validate_intent, name, value)

        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            exit_code = validate_intent.run_self_test(validate_intent.load_schema())
        report = out.getvalue()

        self.assertEqual(exit_code, 0, report)
        self.assertNotIn("legacy.json", report)
        self.assertIn("1/1 fixtures matched expectation.", report)


class RunOverPatternsTest(unittest.TestCase):
    """``_run_over_patterns`` — the shared expand/report/aggregate driver.

    Centralizing the modes' pattern loop removed the duplication that let a fix
    land in some copies and not others — but it also means every file-checking
    mode now inherits this one function's bugs. Two invariants an adopter's CI
    actually depends on are pinned here, because neither is visible from the
    per-file checks the modes supply:

    * a pattern matching nothing (or only directories) is **loud** — a message
      on stderr and a non-zero exit, never a silent pass;
    * a per-file failure **reaches** the exit code.

    ``check_one`` is a plain callable, so this needs no CLI parsing, no argv and
    no subprocess: pass a stub and assert on what the driver did with it.
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name

        for name in ("b.json", "a.json"):
            with open(os.path.join(self.root, name), "w", encoding="utf-8") as handle:
                handle.write("{}\n")
        os.mkdir(os.path.join(self.root, "a-subdir"))

        self.pattern = os.path.join(self.root, "*.json")
        self.no_match = os.path.join(self.root, "*.nope")
        self.dirs_only = os.path.join(self.root, "a-subdir")

    @staticmethod
    def _run(patterns, check_one):
        """Run the driver, returning ``(exit_code, stdout, stderr)``."""
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            exit_code = _run_over_patterns(patterns, check_one)
        return exit_code, out.getvalue(), err.getvalue()

    def test_no_match_is_loud_and_non_zero(self):
        exit_code, _, err = self._run([self.no_match], lambda path: False)
        self.assertEqual(exit_code, 1)
        self.assertIn("no file(s) match", err)
        self.assertIn(self.no_match, err)

    def test_directory_only_pattern_is_loud_and_non_zero(self):
        # _expand_files drops the directory, so this reaches the driver as a
        # no-match. It must NOT read as "nothing to check, all good".
        exit_code, _, err = self._run([self.dirs_only], lambda path: False)
        self.assertEqual(exit_code, 1)
        self.assertIn("no file(s) match", err)

    def test_a_failing_file_sets_the_exit_code(self):
        exit_code, _, err = self._run([self.pattern], lambda path: True)
        self.assertEqual(exit_code, 1)
        self.assertEqual(err, "")

    def test_all_passing_files_exit_zero_and_say_nothing_on_stderr(self):
        exit_code, _, err = self._run([self.pattern], lambda path: False)
        self.assertEqual(exit_code, 0)
        self.assertEqual(err, "")

    def test_one_failure_among_many_still_fails(self):
        # The aggregate is a logical OR, not "the last file wins".
        seen = []
        exit_code, _, _ = self._run(
            [self.pattern], lambda path: seen.append(path) or len(seen) == 1
        )
        self.assertEqual(exit_code, 1)

    def test_all_matched_files_are_visited_once_each_in_expansion_order(self):
        seen = []
        exit_code, _, _ = self._run([self.pattern], lambda path: seen.append(path))
        self.assertEqual(exit_code, 0)
        self.assertEqual(seen, _expand_files(self.pattern))

    def test_a_no_match_pattern_does_not_stop_the_remaining_patterns(self):
        seen = []
        exit_code, _, err = self._run(
            [self.no_match, self.pattern], lambda path: seen.append(path)
        )
        self.assertEqual(exit_code, 1)  # the no-match still counts
        self.assertIn("no file(s) match", err)
        self.assertEqual(seen, _expand_files(self.pattern))


class NonUtf8InputTest(unittest.TestCase):
    """Mis-encoded adopter input is *reported*, never raised.

    ``behavior`` is a free-text English sentence, so accented characters and
    smart quotes are exactly what adopters type — and an editor saving
    latin-1/cp1252 is the ordinary way such a file arrives. The readers open
    strict UTF-8, so those bytes raise ``UnicodeDecodeError``. It is a
    ``ValueError``: neither ``OSError`` nor ``json.JSONDecodeError`` catches it.

    Both entry points are pinned because both have a caller that a raise would
    damage beyond the ugly output: ``check_file`` runs under
    ``_run_over_patterns``, where an escaping exception skips every remaining
    file; ``run_stdin`` is the documented programmatic path for scripts and AI
    coding agents, which parse the ``FAIL`` line and would instead get a
    traceback on stderr.

    The stdin case wraps its bytes in an explicitly strict UTF-8
    ``TextIOWrapper`` rather than leaning on the ambient locale: under C/POSIX,
    Python decodes stdin with ``surrogateescape`` and the read silently
    succeeds, so a locale-dependent probe passes against the *unpatched* script
    and proves nothing.
    """

    # 0xe9 is 'é' in latin-1 and never a valid UTF-8 lead byte on its own.
    LATIN1_JSON = (
        '{"entity": "Caf\xe9", "action": "serves", '
        '"behavior": "serves coffee to the caller", "layer": "unit"}'
    ).encode("latin-1")

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name
        self.schema = validate_intent.load_schema()

    def test_check_file_reports_a_parse_error_instead_of_raising(self):
        path = os.path.join(self.root, "latin1.json")
        with open(path, "wb") as handle:
            handle.write(self.LATIN1_JSON)

        valid, errors, parse_error, kind = check_file(path, self.schema)

        self.assertFalse(valid)
        self.assertEqual(errors, [])
        self.assertIsInstance(parse_error, str)
        self.assertIn("could not read/parse JSON", parse_error)
        # Non-UTF-8 is a *read* failure: the bytes never became a document.
        self.assertEqual(kind, "read")

    def test_run_stdin_reports_a_parse_error_instead_of_raising(self):
        stdin = io.TextIOWrapper(io.BytesIO(self.LATIN1_JSON), encoding="utf-8")
        original = sys.stdin
        sys.stdin = stdin
        self.addCleanup(setattr, sys, "stdin", original)

        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            exit_code = run_stdin(self.schema)

        self.assertEqual(exit_code, 1)
        self.assertIn("FAIL — could not read/parse JSON", out.getvalue())
        self.assertEqual(err.getvalue(), "")


class JsonOutputTest(unittest.TestCase):
    """``--json`` — the machine-readable contract adopters write against.

    Text mode discards the structured result at the print site: a consumer gets
    one exit code and prose whose two failure shapes (an indented ``-> rule``
    list; an inline em-dash ``— problem``) are told apart only by lookahead,
    with the no-match diagnostic on a *different stream* than the findings. The
    ``--json`` document is what replaces that, so what is pinned here is the
    document itself — the envelope, the per-finding ``kind`` taxonomy, the
    no-match finding landing on stdout, exit-code parity with text mode, and the
    hard promise that adding all of it moved the default output by not one byte.

    Driven through ``main`` with ``redirect_stdout`` rather than a subprocess:
    the flag is stripped in ``main`` before a positional dispatch, so argv is
    part of what is being tested and calling the ``run_*`` functions directly
    would skip it.
    """

    BROKEN = os.path.join(REPO_ROOT, "examples", "sources", "invalid", "broken_intent_spec.rb")
    VALID_SOURCE = os.path.join(REPO_ROOT, "examples", "sources", "order_spec.rb")
    VALID_JSON = os.path.join(REPO_ROOT, "examples", "unit-order-total.json")

    ANNOTATION = '{"entity": "Order", "action": "checkout", ' \
                 '"behavior": "returns 402 payment required on expired card", ' \
                 '"layer": "request"}'

    # Shared with NonUtf8InputTest so the text and JSON renderers are pinned
    # against the *same* mis-encoded bytes rather than two look-alike literals.
    LATIN1_JSON = NonUtf8InputTest.LATIN1_JSON

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name

    def _write(self, name, content):
        path = os.path.join(self.root, name)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(content)
        return path

    def _run(self, argv, stdin=None, stdin_bytes=None):
        """Run ``main(argv)``, returning ``(exit_code, stdout, stderr)``.

        ``stdin_bytes`` feeds raw bytes through an explicitly strict UTF-8
        ``TextIOWrapper``, for the same reason :class:`NonUtf8InputTest`
        documents: under a C/POSIX locale Python decodes stdin with
        ``surrogateescape``, so a locale-dependent probe of mis-encoded input
        proves nothing.
        """
        if stdin_bytes is not None:
            stdin = io.TextIOWrapper(io.BytesIO(stdin_bytes), encoding="utf-8")
        elif stdin is not None:
            stdin = io.StringIO(stdin)
        if stdin is not None:
            original = sys.stdin
            sys.stdin = stdin
            self.addCleanup(setattr, sys, "stdin", original)
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            exit_code = main(argv)
        return exit_code, out.getvalue(), err.getvalue()

    def _document(self, argv, stdin=None, stdin_bytes=None):
        """Run ``main`` under ``--json`` and return the parsed stdout document.

        Asserts stdout is *exactly one* JSON document — a stray print alongside
        it is the failure mode that breaks every consumer at once.
        """
        exit_code, out, _err = self._run(argv, stdin=stdin, stdin_bytes=stdin_bytes)
        try:
            document = json.loads(out)
        except json.JSONDecodeError as exc:
            self.fail("stdout was not one JSON document (%s): %r" % (exc, out))
        self.document_exit_code = exit_code
        return document

    # -- envelope ---------------------------------------------------------- #

    def test_document_envelope_names_the_schema_mode_and_summary(self):
        document = self._document(["--json", "--source", self.BROKEN])
        self.assertEqual(document["schema"], "open-test-intent.v1.json")
        self.assertEqual(document["mode"], "source")
        self.assertFalse(document["ok"])
        self.assertEqual(document["summary"], {"files": 1, "annotations": 5, "failed": 5})

    def test_every_finding_has_the_same_keys_in_every_mode(self):
        # A consumer must never have to branch on which mode produced a finding.
        keys = {"file", "line", "ok", "kind", "errors"}
        runs = [
            (["--json", "--source", self.BROKEN], None),
            (["--json", self.VALID_JSON], None),
            (["--json", "-"], self.ANNOTATION),
            (["--json", os.path.join(self.root, "*.nope")], None),
        ]
        for argv, stdin in runs:
            with self.subTest(argv=argv):
                for finding in self._document(argv, stdin=stdin)["findings"]:
                    self.assertEqual(set(finding), keys)
                    self.assertIsInstance(finding["errors"], list)
                    self.assertIsInstance(finding["ok"], bool)

    def test_ok_is_true_and_findings_pass_for_a_conforming_run(self):
        document = self._document(["--json", "--source", self.VALID_SOURCE])
        self.assertEqual(self.document_exit_code, 0)
        self.assertTrue(document["ok"])
        self.assertEqual(document["summary"]["failed"], 0)
        self.assertTrue(document["findings"])  # the fixture does carry annotations
        for finding in document["findings"]:
            self.assertTrue(finding["ok"])
            self.assertIsNone(finding["kind"])
            self.assertEqual(finding["errors"], [])

    # -- source mode ------------------------------------------------------- #

    def test_source_mode_reports_every_annotation_at_its_line(self):
        document = self._document(["--json", "--source", self.BROKEN])
        self.assertEqual(self.document_exit_code, 1)
        self.assertEqual([f["line"] for f in document["findings"]], [9, 15, 21, 28, 34])
        self.assertTrue(all(f["file"] == self.BROKEN for f in document["findings"]))
        self.assertTrue(all(f["ok"] is False for f in document["findings"]))

    def test_kind_distinguishes_the_two_prose_failure_shapes(self):
        # The whole point of the field: lines 28/34 are the inline em-dash shape
        # in text mode, 9/15/21 the indented rule list, and nothing in the text
        # output names the difference.
        document = self._document(["--json", "--source", self.BROKEN])
        kinds = {f["line"]: f["kind"] for f in document["findings"]}
        self.assertEqual(kinds, {9: "schema", 15: "schema", 21: "schema",
                                 28: "extraction", 34: "extraction"})

    def test_extraction_problems_arrive_as_errors_not_as_a_separate_field(self):
        document = self._document(["--json", "--source", self.BROKEN])
        by_line = {f["line"]: f for f in document["findings"]}
        self.assertEqual(
            by_line[28]["errors"],
            ["unterminated object literal (an annotation must fit on one line)"],
        )
        self.assertEqual(len(by_line[9]["errors"]), 2)  # every violated rule, not just the first

    def test_an_unparseable_payload_is_kind_parse_not_kind_extraction(self):
        # Captured fine, but `Order` is a bare word in *value* position, so the
        # normalizer leaves it and json.loads rejects it. Both failures land in
        # the same prose slot today — only `kind` separates them.
        path = self._write("bad_payload_spec.rb", "# @intent: { entity: Order }\n")
        document = self._document(["--json", "--source", path])
        self.assertEqual([f["kind"] for f in document["findings"]], ["parse"])
        self.assertIn("could not parse annotation", document["findings"][0]["errors"][0])

    def test_an_unreadable_source_file_is_kind_read(self):
        path = os.path.join(self.root, "gone_spec.rb")
        with open(path, "w", encoding="utf-8") as handle:
            handle.write("# @intent: %s\n" % self.ANNOTATION)
        os.chmod(path, 0o000)
        self.addCleanup(os.chmod, path, 0o644)
        if os.access(path, os.R_OK):
            self.skipTest("running as root — the mode bits do not deny the read")

        document = self._document(["--json", "--source", path])
        self.assertEqual(self.document_exit_code, 1)
        self.assertEqual([f["kind"] for f in document["findings"]], ["read"])
        self.assertIsNone(document["findings"][0]["line"])

    def test_a_file_with_no_annotations_is_counted_but_yields_no_finding(self):
        path = self._write("bare_spec.rb", "def test_nothing:\n    pass\n")
        document = self._document(["--json", "--source", path])
        self.assertEqual(self.document_exit_code, 0)
        self.assertTrue(document["ok"])
        self.assertEqual(document["findings"], [])
        self.assertEqual(document["summary"], {"files": 1, "annotations": 0, "failed": 0})

    # -- stdin mode -------------------------------------------------------- #

    def test_stdin_mode_reports_every_violated_rule_separately(self):
        document = self._document(
            ["--json", "-"],
            stdin='{"entity":"Order","action":"checkout","behavior":"x","layer":"nope"}',
        )
        self.assertEqual(self.document_exit_code, 1)
        self.assertEqual(document["mode"], "stdin")
        self.assertFalse(document["ok"])
        self.assertEqual(len(document["findings"]), 1)
        finding = document["findings"][0]
        self.assertEqual(finding["kind"], "schema")
        self.assertEqual(len(finding["errors"]), 2)  # bad layer AND short behavior
        joined = " | ".join(finding["errors"])
        self.assertIn("minLength is 15", joined)
        self.assertIn("is not one of", joined)

    def test_stdin_mode_passes_a_conforming_annotation(self):
        document = self._document(["--json", "-"], stdin=self.ANNOTATION)
        self.assertEqual(self.document_exit_code, 0)
        self.assertTrue(document["ok"])
        self.assertEqual(document["summary"], {"files": 0, "annotations": 1, "failed": 0})

    def test_malformed_stdin_json_is_kind_parse_and_still_a_document(self):
        # The failure a consumer is most likely to hit, and the one where a
        # traceback instead of a document would be worst.
        document = self._document(["--json", "-"], stdin="{not json at all")
        self.assertEqual(self.document_exit_code, 1)
        self.assertEqual(document["findings"][0]["kind"], "parse")

    # -- adopter mode ------------------------------------------------------ #

    def test_adopter_mode_reports_one_finding_per_file_with_a_null_line(self):
        bad = self._write("bad.json", '{"entity": "Order"}')
        document = self._document(["--json", self.VALID_JSON, bad])
        self.assertEqual(self.document_exit_code, 1)
        self.assertEqual(document["mode"], "adopter")
        self.assertEqual([f["file"] for f in document["findings"]], [self.VALID_JSON, bad])
        self.assertEqual([f["ok"] for f in document["findings"]], [True, False])
        self.assertEqual([f["line"] for f in document["findings"]], [None, None])
        self.assertEqual(document["findings"][1]["kind"], "schema")

    def test_adopter_mode_unparseable_json_is_kind_parse_not_read(self):
        # The file opened and decoded fine — only its *content* was malformed.
        # Calling that `read` would send an author chasing a checkout problem.
        path = self._write("garbage.json", "this is not json")
        document = self._document(["--json", path])
        self.assertEqual([f["kind"] for f in document["findings"]], ["parse"])

    def test_adopter_mode_an_unreadable_file_is_kind_read(self):
        # The other direction, so the distinction is pinned from both sides:
        # bytes that never decoded into a document are `read`, never `parse`.
        path = os.path.join(self.root, "latin1.json")
        with open(path, "wb") as handle:
            handle.write(b'{"behavior": "caf\xe9"}')
        document = self._document(["--json", path])
        self.assertEqual([f["kind"] for f in document["findings"]], ["read"])

    def test_stdin_mode_undecodable_bytes_are_kind_read_not_parse(self):
        # `sys.stdin.read()` raises before anything reaches `json.loads`, so
        # the bytes never became a document — the same failure a file gets, and
        # the same kind. Reporting it as `parse` sent a consumer to fix the
        # payload when the producer's *encoding* is what is wrong.
        document = self._document(["--json", "-"], stdin_bytes=self.LATIN1_JSON)
        self.assertEqual(self.document_exit_code, 1)
        self.assertFalse(document["ok"])
        self.assertEqual([f["kind"] for f in document["findings"]], ["read"])
        self.assertIn("could not read/parse JSON", document["findings"][0]["errors"][0])
        # Coupled to the kind by the documented rule: a site whose payload was
        # never captured is not a site examined, exactly as for an unreadable
        # adopter file.
        self.assertEqual(document["summary"]["annotations"], 0)

    def test_the_same_undecodable_bytes_are_kind_read_in_every_mode(self):
        # The encoding twin of the malformed-payload invariant below: latin-1
        # bytes are a `read` failure whether they arrive as a file or on stdin.
        path = os.path.join(self.root, "latin1.json")
        with open(path, "wb") as handle:
            handle.write(self.LATIN1_JSON)
        adopter = self._document(["--json", path])
        stdin = self._document(["--json", "-"], stdin_bytes=self.LATIN1_JSON)

        for label, document in (("adopter", adopter), ("stdin", stdin)):
            kinds = [f["kind"] for f in document["findings"]]
            self.assertEqual(kinds, ["read"], "%s mode reported %r" % (label, kinds))
            self.assertEqual(document["summary"]["annotations"], 0, label)

    def test_the_same_malformed_payload_is_kind_parse_in_every_mode(self):
        # The cross-mode invariant the document exists to provide: one failure
        # class gets one `kind`, so a consumer never branches on which mode
        # produced the finding. This is what regressed when adopter mode
        # reported malformed JSON as `read`.
        # The literal is balanced so `--source` *captures* it (an uncapturable
        # one would be `extraction`) and only then fails to parse it.
        malformed = '{"entity": }'
        adopter = self._document(["--json", self._write("bad.json", malformed)])
        stdin = self._document(["--json", "-"], stdin=malformed)
        source = self._document(
            ["--json", "--source", self._write("x_spec.rb", "# @intent: %s\n" % malformed)])

        for label, document in (("adopter", adopter), ("stdin", stdin), ("source", source)):
            kinds = [f["kind"] for f in document["findings"]]
            self.assertEqual(kinds, ["parse"], "%s mode reported %r" % (label, kinds))

    def test_annotations_counts_sites_examined_the_same_way_in_every_mode(self):
        # A malformed payload is still a site that was examined; a file that
        # could not be read is not. Both modes must agree, or summing
        # `annotations` across a run means nothing.
        malformed = self._document(["--json", self._write("bad.json", "{not json")])
        self.assertEqual(malformed["summary"]["annotations"], 1)

        path = os.path.join(self.root, "latin1.json")
        with open(path, "wb") as handle:
            handle.write(b'{"behavior": "caf\xe9"}')
        unreadable = self._document(["--json", path])
        self.assertEqual(unreadable["summary"]["annotations"], 0)

        # Stdin obeys the same rule: an unparseable payload is a site, an
        # undecodable stream is not.
        self.assertEqual(
            self._document(["--json", "-"], stdin="{not json")["summary"]["annotations"], 1)
        self.assertEqual(
            self._document(["--json", "-"],
                           stdin_bytes=self.LATIN1_JSON)["summary"]["annotations"], 0)

    def test_no_match_error_text_carries_no_python_repr_quoting(self):
        pattern = os.path.join(self.root, "*.nope")
        document = self._document(["--json", pattern])
        self.assertEqual(document["findings"][0]["errors"], ["no file(s) match %s" % pattern])

    # -- no-match ---------------------------------------------------------- #

    def test_a_pattern_matching_nothing_is_a_finding_on_stdout(self):
        # The regression this closes: text mode puts it on *stderr*, so a
        # stdout-only consumer saw a clean pass list and an unexplained exit 1.
        pattern = os.path.join(self.root, "*.nope")
        exit_code, out, err = self._run(["--json", pattern])
        document = json.loads(out)

        self.assertEqual(exit_code, 1)
        self.assertFalse(document["ok"])
        self.assertEqual(len(document["findings"]), 1)
        finding = document["findings"][0]
        self.assertEqual(finding["kind"], "no-match")
        self.assertEqual(finding["file"], pattern)
        self.assertFalse(finding["ok"])
        self.assertEqual(err, "")  # ...and it is no longer split across streams

    def test_a_no_match_does_not_hide_the_findings_of_the_other_patterns(self):
        document = self._document(
            ["--json", "--source", os.path.join(self.root, "*.nope"), self.BROKEN]
        )
        kinds = [f["kind"] for f in document["findings"]]
        self.assertEqual(kinds.count("no-match"), 1)
        self.assertEqual(len([k for k in kinds if k != "no-match"]), 5)

    # -- flag handling ----------------------------------------------------- #

    def test_the_flag_is_accepted_anywhere_on_the_command_line(self):
        # There is no option parser — the flag is stripped before a *positional*
        # dispatch, so every position has to reach the same place.
        for argv in (["--json", "--source", self.BROKEN],
                     ["--source", "--json", self.BROKEN],
                     ["--source", self.BROKEN, "--json"],
                     ["--json", "-s", self.BROKEN]):
            with self.subTest(argv=argv):
                document = self._document(argv)
                self.assertEqual(document["mode"], "source")
                self.assertEqual(len(document["findings"]), 5)

    def test_the_flag_is_accepted_anywhere_for_stdin_mode(self):
        for argv in (["--json", "-"], ["-", "--json"]):
            with self.subTest(argv=argv):
                document = self._document(argv, stdin=self.ANNOTATION)
                self.assertEqual(document["mode"], "stdin")
                self.assertTrue(document["ok"])

    def test_json_in_self_test_mode_is_a_usage_error_not_a_prose_fallback(self):
        # Answering a --json request with the fixture harness's prose would hand
        # a consumer text it then fails to parse. Refuse loudly instead.
        exit_code, out, err = self._run(["--json"])
        self.assertEqual(exit_code, 2)
        self.assertEqual(out, "")
        self.assertIn("--json is not supported in self-test mode", err)

    def test_help_still_wins_over_the_flag_and_documents_it(self):
        exit_code, out, _err = self._run(["--json", "--help"])
        self.assertEqual(exit_code, 0)
        self.assertIn("--json", out)

    # -- parity with text mode --------------------------------------------- #

    def _parity_cases(self):
        return [
            ["--source", self.BROKEN],
            ["--source", self.VALID_SOURCE],
            ["-s", self.VALID_SOURCE, self.BROKEN],
            [self.VALID_JSON],
            [self.VALID_JSON, self._write("bad.json", '{"entity": "Order"}')],
            [self._write("garbage.json", "nope")],
            [os.path.join(self.root, "*.nope")],
            ["--source", os.path.join(self.root, "*.nope")],
        ]

    def test_json_exit_code_equals_text_exit_code_for_the_same_input(self):
        for argv in self._parity_cases():
            with self.subTest(argv=argv):
                text_code, _, _ = self._run(list(argv))
                json_code, _, _ = self._run(["--json"] + list(argv))
                self.assertEqual(json_code, text_code)

    def test_json_stdin_exit_code_equals_text_stdin_exit_code(self):
        for payload in (self.ANNOTATION, '{"entity": "Order"}', "{not json"):
            with self.subTest(payload=payload[:20]):
                text_code, _, _ = self._run(["-"], stdin=payload)
                json_code, _, _ = self._run(["--json", "-"], stdin=payload)
                self.assertEqual(json_code, text_code)

    # -- the default output did not move ----------------------------------- #

    def test_source_mode_text_output_is_unchanged_to_the_byte(self):
        expected = (
            "FAIL  {p}:9\n"
            "        -> <root>: missing required property 'entity'\n"
            "        -> <root>: additional property 'entiity' is not allowed\n"
            "FAIL  {p}:15\n"
            "        -> layer: value 'model' is not one of "
            "['unit', 'integration', 'request', 'system']\n"
            "FAIL  {p}:21\n"
            "        -> <root>: missing required property 'layer'\n"
            "        -> behavior: string is 4 char(s), minLength is 15\n"
            "FAIL  {p}:28 — unterminated object literal "
            "(an annotation must fit on one line)\n"
            "FAIL  {p}:34 — no '{{...}}' object literal follows the @intent: token\n"
        ).format(p=self.BROKEN)

        exit_code, out, err = self._run(["--source", self.BROKEN])
        self.assertEqual(exit_code, 1)
        self.assertEqual(out, expected)
        self.assertEqual(err, "")

    def test_adopter_mode_text_output_is_unchanged_to_the_byte(self):
        bad = self._write("bad.json", '{"entity": "Order"}')
        exit_code, out, _err = self._run([self.VALID_JSON, bad])
        self.assertEqual(exit_code, 1)
        self.assertEqual(
            out,
            "PASS  %s\n"
            "FAIL  %s\n"
            "        -> <root>: missing required property 'action'\n"
            "        -> <root>: missing required property 'behavior'\n"
            "        -> <root>: missing required property 'layer'\n" % (self.VALID_JSON, bad),
        )

    def test_stdin_mode_text_output_is_unchanged_to_the_byte(self):
        exit_code, out, _err = self._run(["-"], stdin=self.ANNOTATION)
        self.assertEqual((exit_code, out), (0, "PASS\n"))

        exit_code, out, _err = self._run(["-"], stdin='{"entity": "Order", "layer": "nope"}')
        self.assertEqual(exit_code, 1)
        self.assertTrue(out.startswith("FAIL\n        -> "), out)

    def test_the_no_match_diagnostic_still_goes_to_stderr_without_the_flag(self):
        pattern = os.path.join(self.root, "*.nope")
        exit_code, out, err = self._run([pattern])
        self.assertEqual(exit_code, 1)
        self.assertEqual(out, "")
        self.assertEqual(err, "error: no file(s) match %r\n" % pattern)


class TypeMatchesTest(unittest.TestCase):
    """``_type_matches`` — the draft-07 ``type`` predicate."""

    def test_matches_each_json_type(self):
        cases = [
            ("object", {}),
            ("array", []),
            ("string", "s"),
            ("integer", 7),
            ("number", 7),
            ("number", 7.5),
            ("boolean", True),
            ("null", None),
        ]
        for kind, value in cases:
            with self.subTest(kind=kind, value=value):
                self.assertTrue(_type_matches(value, [kind]))

    def test_rejects_mismatched_type(self):
        cases = [
            ("object", []),
            ("array", {}),
            ("string", 1),
            ("integer", 1.5),
            ("number", "1"),
            ("boolean", 1),
            ("null", 0),
        ]
        for kind, value in cases:
            with self.subTest(kind=kind, value=value):
                self.assertFalse(_type_matches(value, [kind]))

    def test_bool_is_not_an_integer_or_number(self):
        # bool subclasses int in Python: without the explicit guard, True would
        # silently satisfy {"type": "integer"}. This is the classic silent bug.
        for value in (True, False):
            with self.subTest(value=value):
                self.assertFalse(_type_matches(value, ["integer"]))
                self.assertFalse(_type_matches(value, ["number"]))

    def test_union_matches_if_any_member_matches(self):
        self.assertTrue(_type_matches("s", ["string", "null"]))
        self.assertTrue(_type_matches(None, ["string", "null"]))
        self.assertFalse(_type_matches(1, ["string", "null"]))

    def test_empty_allowed_list_matches_nothing(self):
        self.assertFalse(_type_matches("s", []))


class TypeOfTest(unittest.TestCase):
    """``_type_of`` — the JSON type name used in error messages."""

    def test_reports_json_type_names(self):
        cases = [
            (True, "boolean"),
            (False, "boolean"),
            ("s", "string"),
            ({}, "object"),
            ([], "array"),
            (1, "number"),
            (1.5, "number"),
            (None, "null"),
        ]
        for value, expected in cases:
            with self.subTest(value=value):
                self.assertEqual(_type_of(value), expected)

    def test_bool_is_reported_as_boolean_not_number(self):
        # bool must be checked before int, or True would report as "number".
        self.assertEqual(_type_of(True), "boolean")

    def test_unknown_python_type_falls_back_to_class_name(self):
        self.assertEqual(_type_of(set()), "set")


class ValidateTypeKeywordTest(unittest.TestCase):
    """``validate`` — the ``type`` keyword, including its short-circuit."""

    def test_accepts_matching_scalar_type(self):
        self.assertEqual(validate("hello", {"type": "string"}), [])

    def test_reports_type_mismatch_with_expected_and_actual(self):
        errors = validate(5, {"type": "string"})
        self.assertEqual(len(errors), 1)
        self.assertIn("expected type string", errors[0])
        self.assertIn("got number", errors[0])

    def test_accepts_list_form_type_union(self):
        schema = {"type": ["string", "null"]}
        self.assertEqual(validate("s", schema), [])
        self.assertEqual(validate(None, schema), [])
        self.assertEqual(len(validate(1, schema)), 1)

    def test_list_form_type_mismatch_names_every_allowed_type(self):
        errors = validate(1, {"type": ["string", "null"]})
        self.assertIn("expected type string|null", errors[0])

    def test_bool_does_not_satisfy_integer_type(self):
        errors = validate(True, {"type": "integer"})
        self.assertEqual(len(errors), 1)
        self.assertIn("expected type integer", errors[0])
        self.assertIn("got boolean", errors[0])

    def test_bool_does_not_satisfy_number_type(self):
        self.assertEqual(len(validate(False, {"type": "number"})), 1)

    def test_type_mismatch_short_circuits_remaining_keywords(self):
        # A type mismatch makes the other keywords moot — exactly one error.
        errors = validate(5, {"type": "string", "minLength": 100, "enum": ["a"]})
        self.assertEqual(len(errors), 1)
        self.assertIn("expected type string", errors[0])

    def test_non_dict_schema_enforces_nothing(self):
        # Boolean schemas (true/false) and unknown forms are a no-op.
        for schema in (True, False, None, "string"):
            with self.subTest(schema=schema):
                self.assertEqual(validate({"anything": 1}, schema), [])

    def test_empty_schema_accepts_everything(self):
        for value in ({}, [], "s", 1, True, None):
            with self.subTest(value=value):
                self.assertEqual(validate(value, {}), [])


class ValidateEnumTest(unittest.TestCase):
    def test_accepts_member_of_enum(self):
        self.assertEqual(validate("unit", {"enum": ["unit", "system"]}), [])

    def test_rejects_non_member(self):
        errors = validate("e2e", {"enum": ["unit", "system"]})
        self.assertEqual(len(errors), 1)
        self.assertIn("is not one of", errors[0])


class ValidateObjectKeywordsTest(unittest.TestCase):
    def test_reports_each_missing_required_property(self):
        schema = {"type": "object", "required": ["a", "b", "c"]}
        errors = validate({"a": 1}, schema)
        self.assertEqual(len(errors), 2)
        self.assertTrue(all("missing required property" in err for err in errors))

    def test_additional_properties_false_rejects_unknown_key(self):
        schema = {"type": "object", "properties": {"a": {}}, "additionalProperties": False}
        errors = validate({"a": 1, "typo": 2}, schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("additional property 'typo' is not allowed", errors[0])

    def test_additional_properties_defaults_to_permissive(self):
        schema = {"type": "object", "properties": {"a": {}}}
        self.assertEqual(validate({"a": 1, "extra": 2}, schema), [])

    def test_additional_properties_as_schema_validates_unknown_keys(self):
        schema = {"type": "object", "additionalProperties": {"type": "string"}}
        self.assertEqual(validate({"anything": "ok"}, schema), [])
        errors = validate({"anything": 1}, schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("expected type string", errors[0])

    def test_additional_properties_schema_does_not_apply_to_declared_properties(self):
        schema = {
            "type": "object",
            "properties": {"count": {"type": "integer"}},
            "additionalProperties": {"type": "string"},
        }
        self.assertEqual(validate({"count": 1, "note": "hi"}, schema), [])

    def test_object_keywords_are_skipped_for_non_objects(self):
        # required/properties only apply once the instance is a dict.
        self.assertEqual(validate("not-an-object", {"required": ["a"]}), [])

    def test_nested_property_error_carries_dotted_path(self):
        schema = {
            "type": "object",
            "properties": {
                "outer": {
                    "type": "object",
                    "properties": {"inner": {"type": "string"}},
                }
            },
        }
        errors = validate({"outer": {"inner": 1}}, schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("outer.inner", errors[0])

    def test_root_errors_are_labelled_root(self):
        errors = validate(1, {"type": "string"})
        self.assertIn("<root>", errors[0])


class ValidateArrayKeywordsTest(unittest.TestCase):
    def test_min_items(self):
        schema = {"type": "array", "minItems": 2}
        self.assertEqual(validate([1, 2], schema), [])
        errors = validate([1], schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("minimum is 2", errors[0])

    def test_max_items(self):
        schema = {"type": "array", "maxItems": 1}
        self.assertEqual(validate([1], schema), [])
        errors = validate([1, 2], schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("maximum is 1", errors[0])

    def test_items_schema_is_applied_to_every_element(self):
        schema = {"type": "array", "items": {"type": "string"}}
        self.assertEqual(validate(["a", "b"], schema), [])
        errors = validate([5], schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("expected type string", errors[0])

    def test_items_errors_carry_the_element_index(self):
        schema = {"type": "array", "items": {"type": "string"}}
        errors = validate(["ok", 5, 6], schema)
        self.assertEqual(len(errors), 2)
        self.assertIn("[1]", errors[0])
        self.assertIn("[2]", errors[1])

    def test_non_dict_items_is_ignored(self):
        # Tuple-form `items` (a list of schemas) is not supported — and must not crash.
        self.assertEqual(validate([1, "a"], {"type": "array", "items": [{"type": "string"}]}), [])

    def test_array_keywords_are_skipped_for_non_arrays(self):
        self.assertEqual(validate("abc", {"minItems": 10}), [])


class ValidateStringKeywordsTest(unittest.TestCase):
    def test_min_length(self):
        schema = {"type": "string", "minLength": 3}
        self.assertEqual(validate("abc", schema), [])
        errors = validate("ab", schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("minLength is 3", errors[0])

    def test_max_length(self):
        schema = {"type": "string", "maxLength": 2}
        self.assertEqual(validate("ab", schema), [])
        errors = validate("abc", schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("maxLength is 2", errors[0])

    def test_pattern_accepts_a_match(self):
        self.assertEqual(validate("123", {"type": "string", "pattern": "^[0-9]+$"}), [])

    def test_pattern_rejects_a_non_match(self):
        errors = validate("ab", {"type": "string", "pattern": "^[0-9]+$"})
        self.assertEqual(len(errors), 1)
        self.assertIn("does not match pattern", errors[0])

    def test_pattern_is_unanchored_search_not_fullmatch(self):
        # draft-07 `pattern` is a search, not a full match — an unanchored
        # pattern matches anywhere in the string.
        self.assertEqual(validate("xx123xx", {"type": "string", "pattern": "[0-9]+"}), [])

    def test_string_keywords_are_skipped_for_non_strings(self):
        self.assertEqual(validate(5, {"minLength": 10}), [])


class ValidateNumberKeywordsTest(unittest.TestCase):
    def test_minimum(self):
        schema = {"type": "number", "minimum": 0}
        self.assertEqual(validate(0, schema), [])
        errors = validate(-1, schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("below minimum", errors[0])

    def test_maximum(self):
        schema = {"type": "number", "maximum": 10}
        self.assertEqual(validate(10, schema), [])
        errors = validate(11, schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("above maximum", errors[0])

    def test_bounds_are_inclusive(self):
        schema = {"minimum": 1, "maximum": 1}
        self.assertEqual(validate(1, schema), [])

    def test_number_keywords_are_skipped_for_booleans(self):
        # bool is an int subclass — the guard must keep min/max off booleans.
        self.assertEqual(validate(True, {"minimum": 5}), [])
        self.assertEqual(validate(False, {"maximum": -5}), [])

    def test_number_keywords_are_skipped_for_non_numbers(self):
        self.assertEqual(validate("abc", {"minimum": 5}), [])


class ValidateAccumulationTest(unittest.TestCase):
    """Errors accumulate across keyword families rather than stopping at the first."""

    def test_multiple_violations_are_all_reported(self):
        schema = {
            "type": "object",
            "required": ["missing"],
            "properties": {"name": {"type": "string", "minLength": 5}},
            "additionalProperties": False,
        }
        errors = validate({"name": "ab", "junk": 1}, schema)
        self.assertEqual(len(errors), 3)
        joined = " | ".join(errors)
        self.assertIn("missing required property 'missing'", joined)
        self.assertIn("minLength is 5", joined)
        self.assertIn("additional property 'junk' is not allowed", joined)


class ShippedSchemaContractTest(unittest.TestCase):
    """Lock the v1 contract itself, driven through the pure ``validate`` core."""

    @classmethod
    def setUpClass(cls):
        cls.schema = validate_intent.load_schema()

    def annotation(self, **overrides):
        base = {
            "entity": "Order",
            "action": "checkout",
            "behavior": "returns 402 payment required on expired card",
            "layer": "request",
        }
        base.update(overrides)
        return base

    def test_minimal_valid_annotation_passes(self):
        self.assertEqual(validate(self.annotation(), self.schema), [])

    def test_optional_preconditions_array_of_strings_passes(self):
        instance = self.annotation(preconditions=["user is signed in"])
        self.assertEqual(validate(instance, self.schema), [])

    def test_preconditions_rejects_non_string_elements(self):
        instance = self.annotation(preconditions=[1])
        errors = validate(instance, self.schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("preconditions[0]", errors[0])

    def test_each_required_field_is_enforced(self):
        for field in ("entity", "action", "behavior", "layer"):
            with self.subTest(field=field):
                instance = self.annotation()
                del instance[field]
                errors = validate(instance, self.schema)
                self.assertEqual(len(errors), 1)
                self.assertIn("missing required property '%s'" % field, errors[0])

    def test_layer_enum_accepts_only_the_four_layers(self):
        for layer in ("unit", "integration", "request", "system"):
            with self.subTest(layer=layer):
                self.assertEqual(validate(self.annotation(layer=layer), self.schema), [])
        self.assertEqual(len(validate(self.annotation(layer="e2e"), self.schema)), 1)

    def test_behavior_below_min_length_is_rejected(self):
        errors = validate(self.annotation(behavior="too short"), self.schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("minLength is 15", errors[0])

    def test_unknown_property_is_rejected(self):
        errors = validate(self.annotation(layers="request"), self.schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("additional property 'layers' is not allowed", errors[0])

    def test_non_object_annotation_is_rejected(self):
        errors = validate(["not", "an", "object"], self.schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("expected type object", errors[0])

    def test_boolean_field_value_is_rejected_as_a_string(self):
        errors = validate(self.annotation(entity=True), self.schema)
        self.assertEqual(len(errors), 1)
        self.assertIn("got boolean", errors[0])


if __name__ == "__main__":
    unittest.main(verbosity=2)
