import concurrent.futures
import re
import unittest

from task_loop.messages import (
    EmptyMessageError,
    InvalidSenderError,
    UnknownThreadError,
    UnsafePathError,
    add_message,
)

from .support import temp_repo

THREAD_ID_RE = re.compile(r"^-- Thread (\S+)$", re.MULTILINE)


class MessageStorageTests(unittest.TestCase):
    def test_new_threads_and_replies_preserve_existing_content(self):
        with temp_repo() as repo:
            target = repo / "review" / "01-issue.md"

            first = add_message(target, "Question about scope.", "reviewer")
            first_content = target.read_text()
            second = add_message(target, "Starting work.", "developer")
            add_message(target, "Clarified in the PRD.", "user", to=first.thread_id)

            content = target.read_text()
            self.assertTrue(content.startswith(first_content))
            self.assertNotEqual(first.thread_id, second.thread_id)
            self.assertIn(f"-- Reply to Thread {first.thread_id}", content)
            self.assertIn("[user]", content)
            self.assertIn("Clarified in the PRD.", content)
            self.assertRegex(
                content,
                r"\[reviewer\] - \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z",
            )

    def test_invalid_inputs_do_not_create_or_change_files(self):
        with temp_repo() as repo:
            target = repo / "review" / "01-issue.md"

            with self.assertRaises(InvalidSenderError):
                add_message(target, "Hello.", "admin")
            with self.assertRaises(EmptyMessageError):
                add_message(target, " ", "developer")
            self.assertFalse(target.exists())

            add_message(target, "Existing.", "developer")
            before = target.read_text()
            with self.assertRaises(UnknownThreadError):
                add_message(target, "Reply.", "user", to="999")
            self.assertEqual(target.read_text(), before)

    def test_directory_and_escaping_paths_are_rejected(self):
        with temp_repo() as repo:
            directory = repo / "review"
            directory.mkdir()

            with self.assertRaises(UnsafePathError):
                add_message(directory, "Hello.", "developer")
            with self.assertRaises(UnsafePathError):
                add_message("../escaped.md", "Hello.", "developer")

    def test_concurrent_appends_do_not_truncate_or_reuse_thread_ids(self):
        with temp_repo() as repo:
            target = repo / "review" / "01-issue.md"
            worker_count = 12

            def append_one(index: int) -> str:
                return add_message(
                    target, f"Message number {index}.", "developer"
                ).thread_id

            with concurrent.futures.ThreadPoolExecutor(
                max_workers=worker_count
            ) as pool:
                thread_ids = list(pool.map(append_one, range(worker_count)))

            content = target.read_text()
            self.assertEqual(len(thread_ids), len(set(thread_ids)))
            self.assertEqual(set(THREAD_ID_RE.findall(content)), set(thread_ids))
            for index in range(worker_count):
                self.assertIn(f"Message number {index}.", content)


if __name__ == "__main__":
    unittest.main()
