"""Command line entry point for task-loop."""
import argparse
import sys
from typing import Optional, Sequence

from .clarity import UserInputAgent
from .development import DevelopmentAgent
from .discovery import discover_prds
from .messages import (
    EmptyMessageError,
    InvalidSenderError,
    UnknownThreadError,
    UnsafePathError,
    UnwritablePathError,
    add_message,
)
from .orchestration import (
    DEFAULT_MAX_ITERATIONS,
    InvalidMaxIterationsError,
    InvalidPrdPathError,
    run_issue_loop,
    start_run,
)
from .review import ReviewAgent
from .testing import TestingAgent
from .triage import (
    InvalidTriageResponseError,
    NoActionableIssuesError,
    TriageAgent,
    select_issue,
)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="task-loop",
        description=(
            "Coordinate one PRD issue at a time through triage, TDD "
            "development, testing, and review."
        ),
    )
    parser.add_argument(
        "prd_path",
        nargs="?",
        default=None,
        help="Path to a PRD.md file. Omit to list available PRDs.",
    )
    parser.add_argument(
        "--max-iterations",
        default=None,
        help=f"Hard cap on iterations for this run (default: {DEFAULT_MAX_ITERATIONS}).",
    )
    return parser


def build_add_message_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="task-loop add-message",
        description="Append a threaded message to a review or progress file.",
    )
    parser.add_argument("-file", dest="file", required=True)
    parser.add_argument("-message", dest="message", required=True)
    parser.add_argument("-from", dest="sender", required=True)
    parser.add_argument("-to", dest="to", default=None)
    return parser


def main(
    argv: Optional[Sequence[str]] = None,
    triage_agent: Optional[TriageAgent] = None,
    development_agent: Optional[DevelopmentAgent] = None,
    clarity_agent: Optional[UserInputAgent] = None,
    testing_agent: Optional[TestingAgent] = None,
    review_agent: Optional[ReviewAgent] = None,
) -> int:
    """Run the public task-loop CLI.

    ``triage_agent``, ``development_agent``, ``clarity_agent``,
    ``testing_agent``, and ``review_agent`` are optional replaceable agent
    seams, used by tests to script agent and user-input responses without
    invoking a real agent process or blocking on real terminal input. None
    is exposed as a command-line flag.
    """
    effective_argv = list(argv) if argv is not None else sys.argv[1:]
    if effective_argv and effective_argv[0] == "add-message":
        args = build_add_message_parser().parse_args(effective_argv[1:])
        try:
            result = add_message(args.file, args.message, args.sender, to=args.to)
        except (
            InvalidSenderError,
            EmptyMessageError,
            UnknownThreadError,
            UnsafePathError,
            UnwritablePathError,
        ) as exc:
            print(f"task-loop: error: {exc}", file=sys.stderr)
            return 1
        print(f"Thread {result.thread_id}")
        return 0

    parser = build_parser()
    args = parser.parse_args(effective_argv)

    if args.prd_path is None:
        prds = discover_prds()
        if not prds:
            print("No PRDs found under .scratch/<feature>/PRD.md")
        else:
            for prd in prds:
                print(prd)
        return 0

    try:
        run = start_run(args.prd_path, args.max_iterations)
    except (InvalidPrdPathError, InvalidMaxIterationsError) as exc:
        print(f"task-loop: error: {exc}", file=sys.stderr)
        return 1

    print(f"Selected PRD: {run.prd_path}")
    print(f"Max iterations: {run.max_iterations}")

    try:
        issue_path = select_issue(run.prd_path, agent=triage_agent)
    except (NoActionableIssuesError, InvalidTriageResponseError) as exc:
        print(f"task-loop: error: {exc}", file=sys.stderr)
        return 1

    print(f"Selected issue: {issue_path}")

    return run_issue_loop(
        run,
        issue_path,
        development_agent=development_agent,
        testing_agent=testing_agent,
        review_agent=review_agent,
        clarity_agent=clarity_agent,
    )


if __name__ == "__main__":
    sys.exit(main())
