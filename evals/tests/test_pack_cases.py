"""The eval cases of the application pack give the samples that the task had before."""

import json
from pathlib import Path

import pytest

from clanker_evals import pack_cases
from clanker_evals.tasks.investigator import CASE_FIELDS, diagnosis_samples

GOLDEN = Path(__file__).parent / "golden" / "investigator_diagnosis.json"


def test_the_investigator_samples_come_from_the_pack_unchanged() -> None:
    golden = json.loads(GOLDEN.read_text(encoding="utf-8"))
    samples = [
        {"id": s.id, "input": s.input, "target": s.target, "metadata": s.metadata}
        for s in diagnosis_samples()
    ]
    assert samples == golden


def test_the_pack_names_its_tasks() -> None:
    assert pack_cases.tasks_with_cases() == ["investigator_diagnosis"]


def test_a_task_with_no_cases_in_the_pack_is_an_error() -> None:
    with pytest.raises(ValueError, match="has no eval cases for the task author_correctness"):
        pack_cases.load("author_correctness", {})


def test_a_case_with_a_missing_field_is_an_error() -> None:
    with pytest.raises(ValueError, match="needs the field no_such_field"):
        pack_cases.load("investigator_diagnosis", {**CASE_FIELDS, "no_such_field": str})
