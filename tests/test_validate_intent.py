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
breaks all of them at once.

Zero dependencies, like the validator itself — stdlib ``unittest`` only.

Run:
    python3 tests/test_validate_intent.py
    python3 -m unittest discover -s tests
"""

import importlib.util
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
