"""PRD discovery for task-loop.

Discovers PRDs that follow the local `.scratch/<feature>/PRD.md` convention.
"""
from pathlib import Path
from typing import List, Union

PathLike = Union[str, Path]


def discover_prds(root: PathLike = ".") -> List[Path]:
    """Return the PRD paths available under ``<root>/.scratch/<feature>/PRD.md``.

    Results are sorted so the order is deterministic across runs and platforms.
    An empty list is returned when there is no `.scratch` directory or PRDs.
    """
    scratch_dir = Path(root) / ".scratch"
    if not scratch_dir.is_dir():
        return []
    return sorted(scratch_dir.glob("*/PRD.md"))
