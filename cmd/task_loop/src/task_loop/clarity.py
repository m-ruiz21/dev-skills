"""Interactive clarity-request handling for task-loop.

When development returns `[needs-clarity] message`, the CLI must present
that request to the user, capture their answer through a replaceable
"user input" adapter (`UserInputAgent`), and append a non-empty answer as a
`user` reply to the same review thread -- reusing
`task_loop.messages.add_message`, the shared message-writing interface,
rather than editing the review document directly. Cancelled, unavailable, or
blank input raises explicitly instead of fabricating an answer.
"""
from pathlib import Path
from typing import Callable, Optional, Union

from .messages import add_message

PathLike = Union[str, Path]

UserInputAgent = Callable[[str], Optional[str]]


class ClarityCancelledError(RuntimeError):
    """Raised when a clarity request is cancelled, unavailable, or unanswered."""


def default_user_input_agent(prompt: str) -> Optional[str]:
    """Production user-input adapter: prompts on the real terminal.

    Returns `None` to signal cancellation when input is unavailable (EOF,
    e.g. a non-interactive terminal) or interrupted (Ctrl-C), instead of
    raising, so callers decide how to stop safely.
    """
    print(prompt)
    try:
        return input("> ")
    except (EOFError, KeyboardInterrupt):
        return None


def resolve_clarity(
    review_path: PathLike,
    thread_id: str,
    message: str,
    agent: Optional[UserInputAgent] = None,
) -> str:
    """Prompt the user for the requested clarity and append their reply.

    Raises `ClarityCancelledError` -- without appending anything -- when the
    answer is cancelled (`None`), unavailable, or blank.
    """
    prompt = f"task-loop needs clarity: {message}"
    answer = (agent or default_user_input_agent)(prompt)
    if answer is None or not answer.strip():
        raise ClarityCancelledError(
            "clarity request was cancelled or received no answer"
        )

    add_message(review_path, answer, "user", to=thread_id)
    return answer
