"""Tests for scoring the repository's real `review-diff` skill artifact
(see `skills/review-diff/SKILL.md`) into per-dimension and overall pass/fail
verdicts.

Covers the PRD's exponential decay formula at severity weight boundaries,
the 90-point pass threshold, how `info` findings and non-`open` statuses
(`addressed`, `waived`, `invalid`) are excluded from the load `L`, how the
skill's `blocker` severity is conservatively weighted the same as
`critical`, and explicit rejection of malformed artifacts: missing/duplicate
dimensions, empty evidence, and malformed finding identity, dimension,
severity, status, summary, and location.

`dimensions[].grade` is validated (shape and range) but never controls the
verdict -- only `findings` and the exponential formula do, per the PRD.
"""
import copy
import unittest

from task_loop.review import (
    REQUIRED_DIMENSIONS,
    MalformedReviewDataError,
    score_review,
)

# The exact structured artifact from skills/review-diff/SKILL.md's "Compile
# the structured review" section, used verbatim as the RED/GREEN fixture so
# tests prove the scorer consumes the skill's real schema, not an invented
# one.
SKILL_EXAMPLE_ARTIFACT = {
    "schemaVersion": "1.0",
    "runId": "caller-supplied-run-id",
    "dimensions": [
        {
            "dimension": "security",
            "grade": 90,
            "evidence": [
                "No changed code constructs commands from untrusted input."
            ],
        },
        {
            "dimension": "testAdequacy",
            "grade": 85,
            "evidence": ["New success and failure paths have focused tests."],
        },
        {
            "dimension": "planAlignment",
            "grade": 95,
            "evidence": ["All issue acceptance criteria are represented in the diff."],
        },
        {
            "dimension": "codeQuality",
            "grade": 88,
            "evidence": ["Errors remain typed and no broad fallback was introduced."],
        },
        {
            "dimension": "architecture",
            "grade": 90,
            "evidence": ["Workflow decisions remain in the orchestration module."],
        },
    ],
    "findings": [
        {
            "id": "TEST-001",
            "dimension": "testAdequacy",
            "severity": "medium",
            "status": "open",
            "summary": "The timeout branch lacks a regression test.",
            "location": {"path": "src/example.cs", "line": 42, "column": 5},
        }
    ],
}


def _artifact(dimensions=None, findings=None):
    """Build a full artifact with every required dimension present once,
    non-empty evidence, and the given findings (default: none).
    """
    return {
        "schemaVersion": "1.0",
        "runId": "run-1",
        "dimensions": dimensions
        if dimensions is not None
        else [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ],
        "findings": findings if findings is not None else [],
    }


def _finding(dimension, severity, status="open", finding_id="F-001", **overrides):
    finding = {
        "id": finding_id,
        "dimension": dimension,
        "severity": severity,
        "status": status,
        "summary": "A finding.",
    }
    finding.update(overrides)
    return finding


class ExactSkillArtifactShapeTests(unittest.TestCase):
    def test_the_exact_skill_documentation_artifact_scores_successfully(self):
        result = score_review(SKILL_EXAMPLE_ARTIFACT)

        test_adequacy = next(d for d in result.dimensions if d.name == "testAdequacy")
        # One open medium finding -> L = 5 -> score = 100 * e^(-lambda * 5)
        self.assertEqual(test_adequacy.load, 5)
        self.assertAlmostEqual(test_adequacy.score, 100.0 * (0.8 ** (5 / 20)))
        self.assertTrue(test_adequacy.passed)

        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.load, 0)
        self.assertEqual(security.score, 100.0)
        self.assertTrue(result.passed)

    def test_grade_is_recorded_but_does_not_influence_pass_fail(self):
        # A dimension grade of 0 must not fail the dimension: findings and
        # the formula are the sole authority on pass/fail.
        low_grade = copy.deepcopy(SKILL_EXAMPLE_ARTIFACT)
        low_grade["dimensions"][0]["grade"] = 0

        result = score_review(low_grade)

        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.grade, 0)
        self.assertTrue(security.passed)
        self.assertTrue(result.passed)


class NoFindingsScoreEveryDimensionAtOneHundredTests(unittest.TestCase):
    def test_a_review_with_no_findings_scores_every_dimension_at_one_hundred(self):
        result = score_review(_artifact())

        for dimension in result.dimensions:
            self.assertEqual(dimension.score, 100.0)
            self.assertTrue(dimension.passed)
        self.assertTrue(result.passed)


class SingleCriticalFindingScoresExactlyEightyTests(unittest.TestCase):
    def test_a_single_critical_finding_gives_a_load_of_twenty_and_a_score_of_eighty(self):
        result = score_review(
            _artifact(findings=[_finding("security", "critical")])
        )

        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.load, 20)
        self.assertAlmostEqual(security.score, 80.0)
        self.assertFalse(security.passed)
        self.assertFalse(result.passed)


class SeverityWeightMixTests(unittest.TestCase):
    def test_one_finding_of_each_severity_sums_the_prd_weights(self):
        result = score_review(
            _artifact(
                findings=[
                    _finding("codeQuality", "low", finding_id="F-1"),
                    _finding("codeQuality", "medium", finding_id="F-2"),
                    _finding("codeQuality", "high", finding_id="F-3"),
                    _finding("codeQuality", "critical", finding_id="F-4"),
                ]
            )
        )

        code_quality = next(d for d in result.dimensions if d.name == "codeQuality")
        # L = low(1) + medium(5) + high(10) + critical(20) = 36
        self.assertEqual(code_quality.load, 36)


class ThresholdBoundaryTests(unittest.TestCase):
    def test_a_load_of_nine_passes_at_just_above_ninety(self):
        result = score_review(
            _artifact(
                findings=[
                    _finding("architecture", "low", finding_id=f"F-{i}")
                    for i in range(9)
                ]
            )
        )
        architecture = next(d for d in result.dimensions if d.name == "architecture")
        self.assertAlmostEqual(architecture.score, 90.44623519256389)
        self.assertTrue(architecture.passed)
        self.assertTrue(result.passed)

    def test_a_load_of_ten_fails_at_just_below_ninety(self):
        result = score_review(
            _artifact(
                findings=[
                    _finding("architecture", "low", finding_id=f"F-{i}")
                    for i in range(10)
                ]
            )
        )
        architecture = next(d for d in result.dimensions if d.name == "architecture")
        self.assertAlmostEqual(architecture.score, 89.44271909999159)
        self.assertFalse(architecture.passed)
        self.assertFalse(result.passed)


class InfoFindingsDoNotIncreaseLoadTests(unittest.TestCase):
    def test_info_findings_are_accepted_but_never_increase_the_load(self):
        result = score_review(
            _artifact(
                findings=[
                    _finding("planAlignment", "info", finding_id=f"F-{i}")
                    for i in range(100)
                ]
            )
        )
        plan_alignment = next(d for d in result.dimensions if d.name == "planAlignment")
        self.assertEqual(plan_alignment.load, 0)
        self.assertEqual(plan_alignment.score, 100.0)


class NonOpenStatusesDoNotIncreaseLoadTests(unittest.TestCase):
    def test_an_addressed_critical_finding_does_not_increase_the_load(self):
        result = score_review(
            _artifact(
                findings=[_finding("security", "critical", status="addressed")]
            )
        )
        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.load, 0)
        self.assertEqual(security.score, 100.0)
        self.assertTrue(security.passed)

    def test_a_waived_critical_finding_does_not_increase_the_load(self):
        result = score_review(
            _artifact(findings=[_finding("security", "critical", status="waived")])
        )
        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.load, 0)
        self.assertEqual(security.score, 100.0)

    def test_an_invalid_critical_finding_does_not_increase_the_load(self):
        result = score_review(
            _artifact(findings=[_finding("security", "critical", status="invalid")])
        )
        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.load, 0)
        self.assertEqual(security.score, 100.0)


class BlockerSeverityWeightsLikeCriticalTests(unittest.TestCase):
    def test_an_open_blocker_finding_is_weighted_the_same_as_critical(self):
        result = score_review(
            _artifact(findings=[_finding("planAlignment", "blocker")])
        )
        plan_alignment = next(d for d in result.dimensions if d.name == "planAlignment")
        self.assertEqual(plan_alignment.load, 20)
        self.assertAlmostEqual(plan_alignment.score, 80.0)
        self.assertFalse(plan_alignment.passed)
        # The raw blocker count is still reported for visibility.
        self.assertEqual(plan_alignment.counts.get("blocker"), 1)

    def test_a_resolved_blocker_finding_does_not_increase_the_load(self):
        result = score_review(
            _artifact(
                findings=[
                    _finding("planAlignment", "blocker", status="addressed")
                ]
            )
        )
        plan_alignment = next(d for d in result.dimensions if d.name == "planAlignment")
        self.assertEqual(plan_alignment.load, 0)
        self.assertEqual(plan_alignment.score, 100.0)


class MissingDimensionFailsExplicitlyTests(unittest.TestCase):
    def test_a_missing_required_dimension_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
            if name != "security"
        ]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(dimensions=dimensions))
        self.assertIn("security", str(ctx.exception))


class DuplicateDimensionFailsExplicitlyTests(unittest.TestCase):
    def test_a_duplicate_dimension_entry_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ]
        dimensions.append(
            {"dimension": "security", "grade": 90, "evidence": ["dup"]}
        )

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(dimensions=dimensions))
        self.assertIn("security", str(ctx.exception))


class UnknownDimensionFailsExplicitlyTests(unittest.TestCase):
    def test_an_unsupported_dimension_name_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ]
        dimensions.append(
            {"dimension": "performance", "grade": 90, "evidence": ["ok"]}
        )

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(dimensions=dimensions))
        self.assertIn("performance", str(ctx.exception))


class EmptyEvidenceFailsExplicitlyTests(unittest.TestCase):
    def test_a_dimension_with_empty_evidence_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ]
        dimensions[0]["evidence"] = []

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(dimensions=dimensions))
        self.assertIn("evidence", str(ctx.exception))

    def test_evidence_that_is_not_a_list_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ]
        dimensions[0]["evidence"] = "just one string"

        with self.assertRaises(MalformedReviewDataError):
            score_review(_artifact(dimensions=dimensions))


class InvalidGradeFailsExplicitlyTests(unittest.TestCase):
    def test_a_non_integer_grade_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ]
        dimensions[0]["grade"] = "high"

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(dimensions=dimensions))
        self.assertIn("grade", str(ctx.exception))

    def test_an_out_of_range_grade_raises_explicitly(self):
        dimensions = [
            {"dimension": name, "grade": 90, "evidence": ["ok"]}
            for name in REQUIRED_DIMENSIONS
        ]
        dimensions[0]["grade"] = 101

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(dimensions=dimensions))
        self.assertIn("grade", str(ctx.exception))


class NonMappingPayloadFailsExplicitlyTests(unittest.TestCase):
    def test_a_non_object_payload_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(["not", "an", "object"])

    def test_a_missing_schema_version_raises_explicitly(self):
        artifact = _artifact()
        del artifact["schemaVersion"]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(artifact)
        self.assertIn("schemaVersion", str(ctx.exception))

    def test_a_missing_run_id_raises_explicitly(self):
        artifact = _artifact()
        del artifact["runId"]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(artifact)
        self.assertIn("runId", str(ctx.exception))

    def test_a_dimensions_value_that_is_not_a_list_raises_explicitly(self):
        artifact = _artifact()
        artifact["dimensions"] = {"security": {}}

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(artifact)
        self.assertIn("dimensions", str(ctx.exception))

    def test_a_findings_value_that_is_not_a_list_raises_explicitly(self):
        artifact = _artifact()
        artifact["findings"] = {"id": "F-1"}

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(artifact)
        self.assertIn("findings", str(ctx.exception))


class MalformedFindingIdentityFailsExplicitlyTests(unittest.TestCase):
    def test_a_finding_missing_an_id_raises_explicitly(self):
        finding = _finding("security", "high")
        del finding["id"]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(findings=[finding]))
        self.assertIn("id", str(ctx.exception))

    def test_a_finding_with_a_non_string_id_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(
                _artifact(findings=[_finding("security", "high", finding_id=7)])
            )

    def test_a_finding_with_an_empty_id_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(
                _artifact(findings=[_finding("security", "high", finding_id="")])
            )

    def test_duplicate_finding_ids_raise_explicitly(self):
        findings = [
            _finding("security", "high", finding_id="DUP-1"),
            _finding("codeQuality", "low", finding_id="DUP-1"),
        ]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(findings=findings))
        self.assertIn("DUP-1", str(ctx.exception))

    def test_a_finding_that_is_not_an_object_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(_artifact(findings=["high"]))


class MalformedFindingDimensionFailsExplicitlyTests(unittest.TestCase):
    def test_a_finding_missing_a_dimension_raises_explicitly(self):
        finding = _finding("security", "high")
        del finding["dimension"]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(findings=[finding]))
        self.assertIn("dimension", str(ctx.exception))

    def test_a_finding_with_an_unsupported_dimension_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(
                _artifact(findings=[_finding("performance", "high")])
            )
        self.assertIn("performance", str(ctx.exception))


class MalformedFindingSeverityFailsExplicitlyTests(unittest.TestCase):
    def test_a_finding_missing_severity_raises_explicitly(self):
        finding = _finding("security", "high")
        del finding["severity"]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(findings=[finding]))
        self.assertIn("severity", str(ctx.exception))

    def test_an_unrecognized_severity_string_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(findings=[_finding("security", "urgent")]))
        self.assertIn("urgent", str(ctx.exception))


class MalformedFindingStatusFailsExplicitlyTests(unittest.TestCase):
    def test_a_finding_with_an_invalid_status_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(
                _artifact(
                    findings=[_finding("security", "low", status="ignored")]
                )
            )
        self.assertIn("status", str(ctx.exception))

    def test_the_obsolete_resolved_status_is_no_longer_accepted(self):
        # `resolved` was the old custom schema's status; the real skill
        # artifact uses `open`/`addressed`/`waived`/`invalid` only.
        with self.assertRaises(MalformedReviewDataError):
            score_review(
                _artifact(
                    findings=[_finding("security", "low", status="resolved")]
                )
            )


class MalformedFindingSummaryFailsExplicitlyTests(unittest.TestCase):
    def test_a_finding_missing_a_summary_raises_explicitly(self):
        finding = _finding("security", "high")
        del finding["summary"]

        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(_artifact(findings=[finding]))
        self.assertIn("summary", str(ctx.exception))

    def test_a_finding_with_an_empty_summary_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(
                _artifact(
                    findings=[_finding("security", "high", summary="")]
                )
            )


class MalformedFindingLocationFailsExplicitlyTests(unittest.TestCase):
    def test_a_location_missing_a_path_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError) as ctx:
            score_review(
                _artifact(
                    findings=[
                        _finding(
                            "security", "high", location={"line": 1}
                        )
                    ]
                )
            )
        self.assertIn("path", str(ctx.exception))

    def test_a_location_with_a_non_integer_line_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(
                _artifact(
                    findings=[
                        _finding(
                            "security",
                            "high",
                            location={"path": "a.py", "line": "42"},
                        )
                    ]
                )
            )

    def test_a_location_that_is_not_an_object_raises_explicitly(self):
        with self.assertRaises(MalformedReviewDataError):
            score_review(
                _artifact(
                    findings=[
                        _finding("security", "high", location="a.py:42")
                    ]
                )
            )

    def test_a_well_formed_location_is_accepted(self):
        result = score_review(
            _artifact(
                findings=[
                    _finding(
                        "security",
                        "high",
                        location={"path": "a.py", "line": 42, "column": 5},
                    )
                ]
            )
        )
        security = next(d for d in result.dimensions if d.name == "security")
        self.assertEqual(security.load, 10)


class NoUnsupportedCountExtensionRequiredTests(unittest.TestCase):
    def test_a_finding_without_a_count_field_counts_once(self):
        # The skill artifact has no `count` extension; each finding object
        # is exactly one occurrence.
        result = score_review(
            _artifact(
                findings=[
                    _finding("testAdequacy", "low", finding_id=f"F-{i}")
                    for i in range(9)
                ]
            )
        )
        test_adequacy = next(d for d in result.dimensions if d.name == "testAdequacy")
        self.assertEqual(test_adequacy.load, 9)


if __name__ == "__main__":
    unittest.main()
