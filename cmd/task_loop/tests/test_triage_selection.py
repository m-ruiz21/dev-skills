import unittest

from task_loop.triage import (
    InvalidTriageResponseError,
    NoActionableIssuesError,
    select_issue,
)

from .support import temp_repo, write_issue, write_prd, write_progress


class SelectSingleActionableIssueTests(unittest.TestCase):
    def test_select_issue_returns_the_only_actionable_issue(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            selected = select_issue(prd_path)

            self.assertEqual(selected, issue_path)


class ReviewPriorityTests(unittest.TestCase):
    def test_an_issue_under_review_is_selected_before_ready_for_agent_work(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-ready.md", status="ready-for-agent")
            review_issue = write_issue(
                repo, "task-loop", "02-under-review.md", status="review"
            )

            selected = select_issue(prd_path)

            self.assertEqual(selected, review_issue)


class DependencyFilteringTests(unittest.TestCase):
    def test_issues_with_non_actionable_statuses_are_ignored(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-needs-triage.md", status="needs-triage")
            write_issue(repo, "task-loop", "02-wontfix.md", status="wontfix")
            ready = write_issue(repo, "task-loop", "03-ready.md")

            selected = select_issue(prd_path)

            self.assertEqual(selected, ready)

    def test_an_issue_blocked_by_an_open_issue_is_not_selected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            blocker = write_issue(repo, "task-loop", "02-blocker.md")
            write_issue(
                repo,
                "task-loop",
                "01-blocked.md",
                blocked_by=[str(blocker)],
            )

            selected = select_issue(prd_path)

            self.assertEqual(selected, blocker)


    def test_a_dependency_resolved_by_moving_the_issue_to_closed_is_respected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            closed_dep = write_issue(
                repo, "task-loop", "02-dep.md", status="closed", closed=True
            )
            original_dep_path = (
                repo / ".scratch" / "task-loop" / "issues" / "02-dep.md"
            )
            dependent = write_issue(
                repo,
                "task-loop",
                "01-dependent.md",
                blocked_by=[str(original_dep_path)],
            )

            selected = select_issue(prd_path)

            self.assertEqual(selected, dependent)
            self.assertTrue(closed_dep.is_file())


class ProgressContextTests(unittest.TestCase):
    def test_the_triage_agent_receives_the_prd_and_progress_content(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")
            write_progress(repo, "task-loop", "2026-01-01 did some prior work\n")

            captured = {}

            def capturing_agent(context):
                captured["prd_path"] = context.prd_path
                captured["progress"] = context.progress
                return str(context.candidates[0].path)

            select_issue(prd_path, agent=capturing_agent)

            self.assertEqual(captured["prd_path"], prd_path)
            self.assertEqual(
                captured["progress"], "2026-01-01 did some prior work\n"
            )


class ResponseValidationTests(unittest.TestCase):
    def test_the_agents_selected_candidate_is_honored(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")
            second = write_issue(repo, "task-loop", "02-second.md")

            def picks_second(context):
                return str(second)

            selected = select_issue(prd_path, agent=picks_second)

            self.assertEqual(selected, second)

    def test_an_empty_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            with self.assertRaises(InvalidTriageResponseError):
                select_issue(prd_path, agent=lambda context: "")

    def test_a_list_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            issue_path = write_issue(repo, "task-loop", "01-first.md")

            with self.assertRaises(InvalidTriageResponseError):
                select_issue(prd_path, agent=lambda context: [str(issue_path)])

    def test_a_multi_line_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            first = write_issue(repo, "task-loop", "01-first.md")
            second = write_issue(repo, "task-loop", "02-second.md")

            with self.assertRaises(InvalidTriageResponseError):
                select_issue(
                    prd_path,
                    agent=lambda context: f"{first}\n{second}",
                )

    def test_an_out_of_scope_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")
            other_prd_path = write_prd(repo, "other-feature")
            out_of_scope_issue = write_issue(repo, "other-feature", "01-other.md")

            with self.assertRaises(InvalidTriageResponseError):
                select_issue(
                    prd_path, agent=lambda context: str(out_of_scope_issue)
                )

    def test_selecting_a_blocked_issue_outside_the_eligible_set_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-ready.md")
            blocked_issue = write_issue(
                repo,
                "task-loop",
                "02-blocked.md",
                blocked_by=[".scratch/task-loop/issues/99-missing.md"],
            )

            with self.assertRaises(InvalidTriageResponseError):
                select_issue(
                    prd_path, agent=lambda context: str(blocked_issue)
                )


    def test_a_missing_response_is_rejected(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            with self.assertRaises(InvalidTriageResponseError):
                select_issue(prd_path, agent=lambda context: None)


class NoActionableIssuesTests(unittest.TestCase):
    def test_selecting_with_no_actionable_issues_raises_explicitly(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-blocked.md", status="blocked")

            with self.assertRaises(NoActionableIssuesError):
                select_issue(prd_path)


class ReviewDocTests(unittest.TestCase):
    def test_selecting_an_issue_creates_its_review_doc_when_absent(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")

            select_issue(prd_path)

            review_path = repo / "review" / "01-first.md"
            self.assertTrue(review_path.is_file())

    def test_selecting_an_issue_preserves_an_existing_review_doc(self):
        with temp_repo() as repo:
            prd_path = write_prd(repo, "task-loop")
            write_issue(repo, "task-loop", "01-first.md")
            review_path = repo / "review" / "01-first.md"
            review_path.parent.mkdir(parents=True, exist_ok=True)
            review_path.write_text("-- Thread 1\n[user] - 2026-01-01T00:00:00Z\n\nPrior context.\n")
            existing_content = review_path.read_text()

            select_issue(prd_path)

            self.assertEqual(review_path.read_text(), existing_content)


if __name__ == "__main__":
    unittest.main()
