"""Durable threaded messages for task-loop review and progress files."""

import fcntl
import os
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable, Optional, Tuple, Union

PathLike = Union[str, Path]
BlockBuilder = Callable[[str], Tuple[str, str, bool]]

ALLOWED_SENDERS = ("user", "reviewer", "developer")
_THREAD_HEADER_RE = re.compile(r"^-- Thread (?P<id>\S+)$", re.MULTILINE)


class InvalidSenderError(ValueError):
    """Raised when the sender role is unsupported."""


class EmptyMessageError(ValueError):
    """Raised when the message is empty or blank."""


class UnknownThreadError(ValueError):
    """Raised when a reply references a thread that does not exist."""


class UnsafePathError(ValueError):
    """Raised when the target is a directory or leaves the working tree."""


class UnwritablePathError(OSError):
    """Raised when the target cannot be written."""


@dataclass(frozen=True)
class ThreadMessage:
    """The thread associated with a successful append."""

    thread_id: str
    is_new_thread: bool


def _validate_sender(sender: str) -> None:
    if sender not in ALLOWED_SENDERS:
        raise InvalidSenderError(
            f"-from must be one of {ALLOWED_SENDERS}, got {sender!r}"
        )


def _validate_message(message: str) -> None:
    if not message.strip():
        raise EmptyMessageError("-message must not be empty")


def _thread_ids(existing_content: str) -> set[str]:
    return {
        match.group("id") for match in _THREAD_HEADER_RE.finditer(existing_content)
    }


def _next_thread_id(existing_content: str) -> str:
    ids = [int(thread_id) for thread_id in _thread_ids(existing_content)]
    return str(max(ids, default=0) + 1)


def _validate_path(path: Path) -> None:
    if path.is_dir():
        raise UnsafePathError(f"path must be a file, not a directory: {path}")

    root = Path.cwd().resolve()
    resolved = path.resolve()
    if root != resolved and root not in resolved.parents:
        raise UnsafePathError(
            f"path must stay within the current working tree: {path}"
        )

    nearest_existing = path.parent
    while not nearest_existing.exists():
        nearest_existing = nearest_existing.parent
    if not os.access(nearest_existing, os.W_OK):
        raise UnwritablePathError(f"path is not writable: {path}")


def _locked_read_and_append(
    path: Path, build_block: BlockBuilder
) -> Tuple[str, bool]:
    fd = os.open(str(path), os.O_RDWR | os.O_CREAT, 0o644)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX)
        try:
            os.lseek(fd, 0, os.SEEK_SET)
            existing_content = os.read(fd, os.fstat(fd).st_size).decode("utf-8")
            thread_id, block, is_new_thread = build_block(existing_content)
            os.lseek(fd, 0, os.SEEK_END)
            os.write(fd, block.encode("utf-8"))
            return thread_id, is_new_thread
        finally:
            fcntl.flock(fd, fcntl.LOCK_UN)
    finally:
        os.close(fd)


def add_message(
    file_path: PathLike,
    message: str,
    sender: str,
    to: Optional[str] = None,
) -> ThreadMessage:
    """Append a new thread or reply to a review or progress file."""
    _validate_sender(sender)
    _validate_message(message)

    path = Path(file_path)
    _validate_path(path)
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    if to is not None and not path.is_file():
        raise UnknownThreadError(f"unknown thread id: {to!r}")

    def build_block(existing_content: str) -> Tuple[str, str, bool]:
        if to is not None:
            if to not in _thread_ids(existing_content):
                raise UnknownThreadError(f"unknown thread id: {to!r}")
            block = (
                f"-- Reply to Thread {to}\n"
                f"[{sender}] - {timestamp}\n\n{message}\n"
            )
            return to, block, False

        thread_id = _next_thread_id(existing_content)
        block = (
            f"-- Thread {thread_id}\n"
            f"[{sender}] - {timestamp}\n\n{message}\n"
        )
        return thread_id, block, True

    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        thread_id, is_new_thread = _locked_read_and_append(path, build_block)
    except UnknownThreadError:
        raise
    except OSError as exc:
        raise UnwritablePathError(f"failed to write to {path}: {exc}") from exc

    return ThreadMessage(thread_id=thread_id, is_new_thread=is_new_thread)


def build_progress_update_instruction(
    progress_path: PathLike, sender: str
) -> str:
    """Build the common append-only progress instruction for phase agents."""
    _validate_sender(sender)
    return (
        "If you change any code, tests, documentation, configuration, generated "
        "artifacts, or issue-tracker files, append a concise summary before "
        "responding. Do not rewrite prior progress. Run "
        f"`task-loop add-message -file {Path(progress_path)} "
        f'-message "<summary of files and meaningful changes>" -from {sender}`.'
    )
