"""Task 6: the Investigator names the right cause and proposes only an allowed mitigation."""

import re
from typing import Any

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.scorer import Score, Scorer, Target, mean, scorer
from inspect_ai.solver import TaskState

from clanker_evals import pack_cases
from clanker_evals.checks import (
    NO_MITIGATION,
    Verdict,
    answer_text,
    has_string_fields,
    is_component,
    is_mitigation,
    mitigation,
    parse_object,
    rubric,
    source_text,
    to_score,
)
from clanker_evals.inputs import finding, investigator_input
from clanker_evals.repo import PACK, primary_check
from clanker_evals.solvers import role_turn

FIELDS = ["diagnosis", "evidence", "proposed_runbook", "component", "write_up"]
POD_NAME = re.compile(
    rf"\b((?:{re.escape(PACK['name'])}-[a-z]+-[a-z0-9]{{8,10}}-[a-z0-9]{{5}})|postgres-\d)\b"
)
GRADER_NOTE = (
    "The criterion names the true cause of the incident. The submission meets it when its "
    "diagnosis names that cause, or says plainly that the evidence does not show a cause "
    "when the criterion says so. Ignore the proposed mitigation and the writing style."
)


def alert(failed: int = 9, version: str = "1.0.0") -> dict[str, Any]:
    """Build the incident finding that CheckHealth opens."""
    return finding(
        "health",
        "incident:local:2026-03-02T08:57:00Z",
        f"{PACK['title']} in local fails {failed} of 20 {primary_check()} requests",
        {"kind": "incident", "ok": 20 - failed, "total": 20, "p95_ms": 41, "version": version},
    )


# The fields of one case of the application pack, in <evals>/investigator_diagnosis.json:
# the transcript of the debug session, the true cause (the criterion for the grader), the
# acceptable mitigation ("<runbook>:<component>", "none", or "either:<runbook>:<component>"
# when that mitigation and no mitigation are both acceptable), patterns of the evidence in
# the transcript (the answer must cite one), and the arguments of alert(). The optional
# field "history" has the lines of the change history, newest first.
CASE_FIELDS: dict[str, type] = {
    "transcript": str,
    "cause": str,
    "expect": str,
    "evidence": list,
    "alert": dict,
}


def expected_mitigation(expect: str) -> tuple[bool, str, str]:
    """Read the field "expect" of a case.

    Args:
        expect: "none", "<runbook>:<component>" or "either:<runbook>:<component>".

    Returns:
        Whether no mitigation is acceptable, and the runbook and the component of the
        acceptable mitigation. The two names are empty when the case expects no mitigation.

    Raises:
        ValueError: The runbook is not a mitigation of the pack, or the component is not a
            component of the pack.
    """
    if expect == "none":
        return (True, "", "")
    wanted = expect.removeprefix("either:")
    runbook, _, component = wanted.partition(":")
    if not is_mitigation(runbook) or not is_component(component):
        raise ValueError(
            f"expect is {expect!r}; it must be none, <runbook>:<component> or "
            "either:<runbook>:<component>, with a mitigation runbook and a component of the pack"
        )
    return (wanted != expect, runbook, component)


def incident_sample(case: pack_cases.Case) -> Sample:
    """Build one Investigator sample from a case of the application pack."""
    name, transcript, evidence = case["id"], case["transcript"], case["evidence"]
    missing = [p for p in evidence if not re.search(p, transcript, re.IGNORECASE)]
    if missing:
        raise ValueError(f"sample {name}: evidence {missing} is not in its transcript")
    try:
        expected_mitigation(case["expect"])
    except ValueError as error:
        raise ValueError(f"sample {name}: {error}") from error
    return Sample(
        id=name,
        input=investigator_input(alert(**case["alert"]), transcript, case.get("history")),
        target=case["cause"],
        metadata={"role": "investigator", "expect": case["expect"], "evidence": evidence},
    )


def diagnosis_samples() -> list[Sample]:
    """Return the samples of investigator_diagnosis: the incident cases of the pack."""
    cases = pack_cases.load("investigator_diagnosis", CASE_FIELDS)
    return [incident_sample(case) for case in cases]


def _mitigation_verdicts(state: TaskState) -> dict[str, Verdict]:
    runbook, component = mitigation(parse_object(state.output.completion))
    proposed = is_mitigation(runbook)
    none_fits, wanted_runbook, wanted_component = expected_mitigation(state.metadata["expect"])
    right = proposed and (runbook, component) == (wanted_runbook, wanted_component)
    return {
        "runbook_from_list": (
            proposed or runbook in NO_MITIGATION,
            f"proposed_runbook {runbook!r} is not in the list of routine runbooks",
        ),
        "component_allowed": (
            not proposed or is_component(component),
            f"component {component!r} is not an allowed target",
        ),
        "mitigation_fits_cause": (
            right or (none_fits and runbook in NO_MITIGATION),
            f"proposed {runbook!r} on {component!r}; expected {state.metadata['expect']}",
        ),
    }


def diagnosis_verdicts(state: TaskState) -> dict[str, Verdict]:
    """Check the JSON fields of one diagnosis against the transcript and the expectation."""
    parsed = parse_object(state.output.completion)
    text, source = answer_text(state), source_text(state)
    cited = [p for p in state.metadata["evidence"] if re.search(p, text, re.IGNORECASE)]
    invented = sorted(set(POD_NAME.findall(text)) - set(POD_NAME.findall(source)))
    return {
        "json_fields": has_string_fields(parsed, FIELDS, exact=False),
        **_mitigation_verdicts(state),
        "cites_transcript_evidence": (bool(cited), "cites none of the evidence patterns"),
        "no_invented_pods": (not invented, f"not in the transcript: {', '.join(invented)}"),
    }


@scorer(metrics={"*": [mean()]})
def diagnosis_fields() -> Scorer:
    """Score a diagnosis with the deterministic checks on its JSON fields."""

    async def score(state: TaskState, target: Target) -> Score:
        return to_score(diagnosis_verdicts(state), state.output.completion)

    return score


@task
def investigator_diagnosis() -> Task:
    """Task 6: distinct causes, the right diagnosis and only an allowed mitigation."""
    return Task(
        dataset=diagnosis_samples(),
        solver=role_turn("investigator"),
        scorer=[diagnosis_fields(), rubric(GRADER_NOTE)],
    )
