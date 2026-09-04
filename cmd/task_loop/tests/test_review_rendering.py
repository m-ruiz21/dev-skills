"""Tests for rendering scored review dimensions to a terminal-readable
report: every dimension prints its numeric score, a progress bar, and an
unambiguous `passed`/`failed` tag, colored with ANSI codes when enabled and
plain when disabled, plus a clear overall pass/fail verdict line.
"""
import unittest

from task_loop.review import (
    REQUIRED_DIMENSIONS,
    DimensionScore,
    ReviewScore,
    render_review,
)


def _dimension(name, score, passed):
    return DimensionScore(
        name=name,
        grade=90,
        counts={"low": 0, "medium": 0, "high": 0, "critical": 0, "blocker": 0},
        load=0,
        score=score,
        passed=passed,
    )


def _score(passing=True):
    dimensions = tuple(
        _dimension(name, 100.0 if passing else 50.0, passing)
        for name in REQUIRED_DIMENSIONS
    )
    return ReviewScore(dimensions=dimensions, passed=passing)


class RendersEveryDimensionWithScoreAndBarTests(unittest.TestCase):
    def test_every_dimension_is_rendered_with_its_numeric_score_and_a_bar(self):
        rendered = render_review(_score(passing=True), use_color=False)

        for name in REQUIRED_DIMENSIONS:
            self.assertIn(name, rendered)
        self.assertIn("100.0", rendered)
        self.assertIn("[", rendered)
        self.assertIn("]", rendered)


class PlainPassedFailedTagsWithoutColorTests(unittest.TestCase):
    def test_passing_and_failing_dimensions_render_unambiguous_plain_tags(self):
        rendered = render_review(_score(passing=True), use_color=False)
        self.assertIn("PASSED", rendered)
        self.assertNotIn("\x1b[", rendered)

        rendered = render_review(_score(passing=False), use_color=False)
        self.assertIn("FAILED", rendered)
        self.assertNotIn("\x1b[", rendered)


class ColoredTagsWhenColorEnabledTests(unittest.TestCase):
    def test_passing_dimensions_are_colored_green_and_failing_ones_red(self):
        rendered = render_review(_score(passing=True), use_color=True)
        self.assertIn("\x1b[32m", rendered)
        self.assertIn("passed", rendered)

        rendered = render_review(_score(passing=False), use_color=True)
        self.assertIn("\x1b[31m", rendered)
        self.assertIn("failed", rendered)


class OverallVerdictLineTests(unittest.TestCase):
    def test_the_overall_verdict_line_reflects_the_review_scores_passed_flag(self):
        rendered = render_review(_score(passing=True), use_color=False)
        self.assertIn("Overall: PASSED", rendered)

        rendered = render_review(_score(passing=False), use_color=False)
        self.assertIn("Overall: FAILED", rendered)


class ProgressBarReflectsScoreTests(unittest.TestCase):
    def test_a_full_score_renders_a_fully_filled_bar_and_a_low_score_does_not(self):
        full = render_review(_score(passing=True), use_color=False)
        low = render_review(_score(passing=False), use_color=False)

        self.assertIn("[" + "#" * 20 + "]", full)
        self.assertNotIn("[" + "#" * 20 + "]", low)


if __name__ == "__main__":
    unittest.main()
