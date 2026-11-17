"""Task 6: the Investigator names the right cause and proposes only an allowed mitigation."""

import re
from typing import Any

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.scorer import Score, Scorer, Target, mean, scorer
from inspect_ai.solver import TaskState

from clanker_evals import transcripts
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
from clanker_evals.solvers import role_turn

FIELDS = ["diagnosis", "evidence", "proposed_runbook", "deployment", "write_up"]
POD_NAME = re.compile(r"\b((?:paperboat-[a-z]+-[a-z0-9]{8,10}-[a-z0-9]{5})|postgres-\d)\b")
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
        f"Paperboat in local fails {failed} of 20 inventory requests",
        {"kind": "incident", "ok": 20 - failed, "total": 20, "p95_ms": 41, "version": version},
    )


CREATED = "WI-0001 Create environment local: Done, 288h0m0s ago"
HISTORY: dict[str, list[str]] = {
    # The newest change comes first, as recentChanges writes it.
    "bad-release-healthy-pods": [
        "WI-0008 Upgrade Paperboat to 1.2.0 (inventory snapshot): Done, 6m0s ago",
        "WI-0006 Patch Paperboat to 1.1.1 (base image security fix): Done, 143h0m0s ago",
        CREATED,
    ],
    "crash-loop-after-deploy": [
        "WI-0008 Upgrade Paperboat to 1.2.0 (inventory snapshot): Verifying, 7m0s ago",
        "WI-0006 Patch Paperboat to 1.1.1 (base image security fix): Done, 143h0m0s ago",
        CREATED,
    ],
    "image-pull-failure": [
        "WI-0009 Upgrade Paperboat to 1.1.2: Verifying, 8m0s ago",
        CREATED,
    ],
    "oom-kills": [
        "WI-0006 Patch Paperboat to 1.1.1 (base image security fix): Done, 216h0m0s ago",
        CREATED,
    ],
    "memory-fault-with-old-noise": [
        "WI-0003 Restart paperboat-worker: Done, 72h0m0s ago",
        "WI-0001 Create environment local: Done, 216h0m0s ago",
    ],
    "debug-session-failed": ["no change is on record"],
}


def incident_sample(
    name: str, transcript: str, cause: str, expect: str, evidence: list[str], **alert_args: Any
) -> Sample:
    """Build one Investigator sample.

    Args:
        name: The sample ID.
        transcript: The debug-session transcript.
        cause: The true cause, as the criterion for the grader.
        expect: The acceptable mitigation: "restart:<deployment>", "none", or
            "either:<deployment>" when a restart of that deployment and no mitigation are
            both acceptable.
        evidence: Patterns of the evidence in the transcript. The answer must cite one.
        **alert_args: Arguments for alert().
    """
    missing = [p for p in evidence if not re.search(p, transcript, re.IGNORECASE)]
    if missing:
        raise ValueError(f"sample {name}: evidence {missing} is not in its transcript")
    return Sample(
        id=name,
        input=investigator_input(alert(**alert_args), transcript, HISTORY.get(name)),
        target=cause,
        metadata={"role": "investigator", "expect": expect, "evidence": evidence},
    )


def _application_samples() -> list[Sample]:
    old_restart: transcripts.Row = (
        "3d",
        "Normal",
        "Killing",
        f"pod/{transcripts.WORKER_POD}",
        "Stopping container worker",
    )
    return [
        incident_sample(
            "memory-fault-healthy-pods",
            transcripts.memory_fault(),
            "A fault inside the running api process (in-memory fault state): pods are healthy "
            "and not restarted, no deploy happened, yet inventory requests answer 500.",
            "restart:paperboat-api",
            [r"request handler failed", r"/api/inventory", r"\b500\b"],
        ),
        incident_sample(
            "memory-fault-with-old-noise",
            transcripts.memory_fault(extra_events=(old_restart,)),
            "A fault inside the running api process (in-memory fault state). The worker "
            "restart three days ago is unrelated.",
            "restart:paperboat-api",
            [r"request handler failed", r"/api/inventory", r"\b500\b"],
        ),
        incident_sample(
            "bad-release-healthy-pods",
            transcripts.bad_release(),
            "The release 1.2.0-bad that rolled out minutes ago has a bug in the inventory code "
            "(build tank snapshot: index out of range). The pods are healthy, so a restart "
            "gives the same code and does not help; the release must be rolled back.",
            "none",
            [r"index out of range", r"1\.2\.0-bad", r"snapshot"],
            version="1.2.0-bad",
        ),
        incident_sample(
            "crash-loop-after-deploy",
            transcripts.crash_loop_after_deploy(),
            "The new release 1.2.0-bad crashes at start with a nil pointer panic, so the new "
            "api pod is in CrashLoopBackOff after the deploy. A restart does not help; the "
            "release must be rolled back or fixed.",
            "none",
            [r"CrashLoopBackOff", r"panic", r"1\.2\.0-bad", r"nil pointer"],
            version="1.2.0-bad",
        ),
    ]


def _platform_samples() -> list[Sample]:
    return [
        incident_sample(
            "database-unreachable",
            transcripts.database_unreachable(),
            "The Postgres database is down (its pod cannot mount its volume), so the api "
            "cannot connect to the database. Restarting an application deployment does not "
            "help.",
            "none",
            [r"connection refused", r"postgres-0", r"FailedMount", r"5432"],
            failed=20,
        ),
        incident_sample(
            "image-pull-failure",
            transcripts.image_pull_failure(),
            "The api pod cannot pull its image (tag 1.1.2 is not in the registry: manifest "
            "unknown), so no api pod runs. A restart does not help.",
            "none",
            [r"ImagePullBackOff", r"ErrImagePull", r"manifest unknown", r"pull"],
            failed=20,
        ),
        incident_sample(
            "healthy-system-false-alarm",
            transcripts.healthy_system(),
            "The debug session shows no fault: all pods are healthy and every request "
            "succeeds. The evidence does not show a cause; the alert may be a false alarm or "
            "the fault has cleared.",
            "none",
            [r"\b200\b", r"Running", r"1/1"],
        ),
        incident_sample(
            "oom-kills",
            transcripts.oom_kills(),
            "The api container runs out of memory and is OOM-killed again and again (heap "
            "near the 256 MB limit).",
            "either:paperboat-api",
            [r"OOMKill", r"out of memory", r"limit_mb"],
            failed=14,
            version="1.1.1",
        ),
        incident_sample(
            "shared-volume-full",
            transcripts.volume_full(),
            "The shared volume is full (no space left on device), so writes fail. A restart "
            "does not free space.",
            "none",
            [r"no space left on device", r"100% used"],
        ),
        incident_sample(
            "pod-unschedulable",
            transcripts.unschedulable(),
            "The api pod is Pending because no node has enough CPU (FailedScheduling: "
            "Insufficient cpu). A restart does not help.",
            "none",
            [r"Pending", r"Insufficient cpu", r"FailedScheduling"],
            failed=20,
        ),
        incident_sample(
            "web-readiness-fails",
            transcripts.web_not_ready(),
            "The web pod fails its readiness probe (timeout) while the api answers every "
            "request; the fault is in the web deployment, not in the api.",
            "either:paperboat-web",
            [r"Readiness probe failed", transcripts.WEB_POD, r"paperboat-web"],
        ),
        incident_sample(
            "debug-session-failed",
            transcripts.session_failed(),
            "The debug session could not reach the cluster, so there is no evidence of the "
            "cause. The diagnosis must say that the cause is unknown.",
            "none",
            [r"Unable to connect", r"connection refused", r"6443"],
        ),
    ]


def diagnosis_samples() -> list[Sample]:
    """Return the samples of investigator_diagnosis."""
    return [*_application_samples(), *_platform_samples()]


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
