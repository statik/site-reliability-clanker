"""Task 6: the Investigator names the right cause and proposes only an allowed mitigation."""

import re
from typing import Any

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.scorer import Score, Scorer, Target, mean, scorer
from inspect_ai.solver import TaskState

from clanker_evals import pack_cases
from clanker_evals.checks import (
    ALLOWED_MITIGATIONS,
    NO_MITIGATION,
    RUNBOOKS,
    Verdict,
    answer_text,
    has_string_fields,
    mitigation,
    parse_object,
    rubric,
    source_text,
    to_score,
)
from clanker_evals.inputs import finding, investigator_input
from clanker_evals.repo import PACK
from clanker_evals.solvers import role_turn

FIELDS = ["diagnosis", "evidence", "proposed_runbook", "deployment", "write_up"]
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
        f"{PACK['title']} in local fails {failed} of 20 {PACK['health']['probe_name']} requests",
        {"kind": "incident", "ok": 20 - failed, "total": 20, "p95_ms": 41, "version": version},
    )


# The fields of one case of the application pack, in <evals>/investigator_diagnosis.json:
# the transcript of the debug session, the true cause (the criterion for the grader), the
# acceptable mitigation ("restart:<deployment>", "none", or "either:<deployment>" when a
# restart of that deployment and no mitigation are both acceptable), patterns of the
# evidence in the transcript (the answer must cite one), and the arguments of alert(). The
# optional field "history" has the lines of the change history, newest first.
CASE_FIELDS: dict[str, type] = {
    "transcript": str,
    "cause": str,
    "expect": str,
    "evidence": list,
    "alert": dict,
}


def incident_sample(case: pack_cases.Case) -> Sample:
    """Build one Investigator sample from a case of the application pack."""
    name, transcript, evidence = case["id"], case["transcript"], case["evidence"]
    missing = [p for p in evidence if not re.search(p, transcript, re.IGNORECASE)]
    if missing:
        raise ValueError(f"sample {name}: evidence {missing} is not in its transcript")
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
    runbook, deployment = mitigation(parse_object(state.output.completion))
    restart = runbook == RUNBOOKS[0]
    kind, _, wanted = state.metadata["expect"].partition(":")
    right_restart = restart and deployment == wanted
    matches = {
        "restart": right_restart,
        "none": runbook in NO_MITIGATION,
        "either": right_restart or runbook in NO_MITIGATION,
    }[kind]
    return {
        "runbook_from_list": (
            restart or runbook in NO_MITIGATION,
            f"proposed_runbook {runbook!r} is not in the list of routine runbooks",
        ),
        "deployment_allowed": (
            not restart or deployment in ALLOWED_MITIGATIONS,
            f"deployment {deployment!r} is not an allowed target",
        ),
        "mitigation_fits_cause": (
            matches,
            f"proposed {runbook!r} on {deployment!r}; expected {state.metadata['expect']}",
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
