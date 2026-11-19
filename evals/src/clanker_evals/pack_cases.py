"""The eval cases that an application pack brings.

A pack can have a directory of eval cases (the field `evals` of its manifest), with one file
`<task>.json` for each task of this suite. A task that reads its samples from the pack tests
the application of that pack, with the same checks for every pack. The Go command
`clanker pack validate` checks the form of each file (internal/pack/parts.go); this module
checks the fields that a task needs.
"""

import json
import re
from typing import Any

from clanker_evals.repo import PACK, PACK_DIR

SCHEMA = 1
# The task file (tasks/<file>.py) of each task that reads its samples from the pack.
TASK_FILES = {"investigator_diagnosis": "investigator"}
SAMPLE_ID = re.compile(r"^[a-z0-9][a-z0-9-]{0,60}$")
Case = dict[str, Any]


def tasks_with_cases() -> list[str]:
    """Return the names of the tasks that the pack has cases for."""
    if "evals" not in PACK:
        return []
    return sorted(path.stem for path in (PACK_DIR / PACK["evals"]).glob("*.json"))


def load(task: str, required: dict[str, type]) -> list[Case]:
    """Return the cases of one task from the pack.

    Args:
        task: The name of the task, which is the name of the file without `.json`.
        required: The fields that each case must have, with the type of each.

    Returns:
        The cases, in the order of the file.

    Raises:
        ValueError: The pack has no cases for the task, or a case is not correct.
    """
    if task not in tasks_with_cases():
        raise ValueError(
            f"the application pack {PACK['name']} has no eval cases for the task {task}; "
            f"add {PACK.get('evals', 'evals')}/{task}.json to the pack"
        )
    path = PACK_DIR / PACK["evals"] / f"{task}.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    if data.get("schema") != SCHEMA or data.get("task") != task:
        raise ValueError(f"{path}: schema must be {SCHEMA} and task must be {task!r}")
    cases: list[Case] = data.get("samples") or []
    if not cases:
        raise ValueError(f"{path}: the file has no samples")
    seen: set[str] = set()
    for case in cases:
        _check(path.name, case, required, seen)
    return cases


def _check(file: str, case: Case, required: dict[str, type], seen: set[str]) -> None:
    name = case.get("id")
    if not isinstance(name, str) or not SAMPLE_ID.match(name) or name in seen:
        raise ValueError(f"{file}: the id {name!r} is not correct, or is in the file two times")
    seen.add(name)
    for field, kind in required.items():
        if not isinstance(case.get(field), kind):
            raise ValueError(f"{file}: sample {name} needs the field {field} ({kind.__name__})")


def main() -> None:
    """Print one line `<task file> <task>` for each task that the pack has cases for."""
    names = tasks_with_cases()
    unknown = [name for name in names if name not in TASK_FILES]
    if unknown or not names:
        raise SystemExit(
            f"the application pack {PACK['name']} has eval cases for {names or 'no task'}; "
            f"this suite reads cases for {sorted(TASK_FILES)}"
        )
    for name in names:
        print(TASK_FILES[name], name)


if __name__ == "__main__":
    main()
