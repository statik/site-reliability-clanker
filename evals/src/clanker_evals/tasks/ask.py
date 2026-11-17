"""Task 7: an Ask answer uses only the records that plain code gathered."""

import re
from typing import Any

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.scorer import Score, Scorer, Target, mean, scorer
from inspect_ai.solver import TaskState

from clanker_evals.checks import (
    Verdict,
    count_sentences,
    first_match,
    ids_grounded,
    rubric,
    source_text,
    to_score,
    versions_grounded,
)
from clanker_evals.inputs import ask_input
from clanker_evals.solvers import role_turn

MAX_SENTENCES = 6
NOT_VERBS = (
    "answer|say|show|contain|include|have|cover|tell|state|list|mention|give|provide|"
    "identify|explain|record|specify|indicate|reveal|name|hold|describe"
)
NOT_ANSWERED = [
    r"\b(do|does|did) not (" + NOT_VERBS + ")",
    r"\b(don't|doesn't|didn't) (" + NOT_VERBS + ")",
    r"\bno (record|information|data|answer|mention)\b",
    r"\bnot (in|part of|available in|covered by|recorded in) (the|these) records?\b",
    r"\b(cannot|can't|unable to) (answer|tell|say|determine)\b",
    r"\bno way to (tell|know|answer)\b",
]
FACT = re.compile(r"(?<![\d-])(CVE-\d{4}-\d+|\d{1,2}:\d{2}|\d{4}-\d{2}-\d{2})(?!\d)")
GROUNDED = (
    "Every factual statement of the answer is supported by the records inside the <data> "
    "block (or by the question). The answer adds no fact, cause, name, date or number from "
    "outside the records. When the records do not answer the question, the answer says so. "
)

WAITING = [
    "WI-0012 (Upgrade Paperboat to 1.1.0, tier T2) waits for approval for 3h12m0s. If "
    "deferred: Customers keep the current inventory board. Nothing breaks.",
    "WI-0014 (Grow the Postgres volume from 2 GB to 4 GB, tier T3) waits for approval for "
    "26h4m0s. If deferred: The database volume fills in about 9 days at the current growth.",
]
PATCHES = [
    "Advisory PG-17.12: Postgres minor release 17.12 is pending; this environment runs 17.11: "
    "WI-0016 is in state AwaitingApproval",
    "Advisory PB-2026-014: vulnerability in the base image of Paperboat 1.1.0; fixed in 1.1.1: "
    "WI-0011 is in state Done",
    "Kubernetes 1.36 reaches end of support; plan the upgrade to 1.37: no work item yet",
]
CHANGES = [
    "WI-0004 Upgrade Paperboat to 1.1.0: Done on 2026-02-11, tier T2, approved by alice",
    "WI-0006 Patch Paperboat to 1.1.1 (base image security fix): Done on 2026-02-17, tier T2, "
    "approved by bob",
    "WI-0008 Upgrade Paperboat to 1.2.0 (inventory snapshot): RolledBack on 2026-02-23, tier "
    "T2, approved by alice",
    "WI-0009 Grow the Postgres volume from 2 GB to 4 GB: Done on 2026-02-26, tier T3, approved "
    "by alice and carol",
    "WI-0010 Restart paperboat-api to mitigate WI-0009: Done on 2026-02-27, tier T1, approved "
    "by a standing approval or the approval of the original change",
]
RESTORE = [
    "Last successful restore-drill: WI-0005 on 2026-02-14T02:10",
    "No successful db-backup is on record.",
]
DURATION = [
    "WI-0008 spent 4s in state Planning",
    "WI-0008 spent 38m12s in state AwaitingApproval",
    "WI-0008 spent 1m55s in state Applying",
    "WI-0008 spent 41s in state Verifying",
    "WI-0008 spent 3m20s in state RollingBack",
    "WI-0008 spent 2h1m0s in state RolledBack",
]
UPCOMING = [
    "WI-0018 Patch Postgres from 17.11 to 17.12 is scheduled for 2026-03-07T02:00:00Z",
    "Forecast: Kubernetes 1.36 reaches end of support; plan the upgrade to 1.37, due 2026-06-30",
]
ACCESS = [
    "Standing access: observer and planner are read-only. Every applier identity, the runbook "
    "identity and the debugger have no standing access.",
    "Grant G-0031 is active: applier-50-app for WI-0012 until 2026-03-02T09:45:00Z",
]
NO_ACCESS = [ACCESS[0], "No write grant is active right now."]
WORK_ITEM = [
    "WI-0016 Patch Postgres from 17.11 to 17.12 is in state AwaitingApproval, tier T2 by rule "
    "normal-change. Patch the Postgres database from 17.11 to 17.12. The database restarts "
    "once.",
    "Plan of 30-data: 0 to add, 1 to change, 0 to destroy, 0 to replace",
    "update kubernetes_stateful_set_v1.postgres: spec.template.spec.container.image: "
    "postgres:17.11-alpine → postgres:17.12-alpine",
    "Rollback: Revert the pull request. The 17.11 image starts on the same data volume.",
]
GENERAL = [
    "I can answer about waiting approvals, patches, recent changes, restore tests, upcoming "
    "maintenance and current access."
]


def ask_sample(
    name: str, question: str, records: list[str], ids: list[str] | None = None, **meta: Any
) -> Sample:
    """Build one Ask sample.

    Args:
        name: The sample ID.
        question: The question of the approver.
        records: The record lines that the gather function of ask.go would give.
        ids: Work item IDs that the answer must name.
        **meta: "answerable": False when the records do not answer the question.
    """
    return Sample(
        id=name,
        input=ask_input(question, records),
        target=GROUNDED,
        metadata={"role": "ask", "ids": ids or [], "answerable": True, **meta},
    )


def groundedness_samples() -> list[Sample]:
    """Return the samples of ask_groundedness."""
    return [
        ask_sample(
            "waiting",
            "What is waiting on me and what happens if I leave it a week?",
            WAITING,
            ["WI-0012", "WI-0014"],
        ),
        ask_sample(
            "nothing-waiting", "Is anything waiting on me?", ["Nothing waits for a decision."]
        ),
        ask_sample(
            "patches", "Are we behind on any patches? Which ones matter?", PATCHES, ["WI-0016"]
        ),
        ask_sample(
            "changes-and-approvers",
            "What changed in production in the last 30 days, and who approved each change?",
            CHANGES,
            ["WI-0004", "WI-0006", "WI-0008", "WI-0009", "WI-0010"],
        ),
        ask_sample(
            "restore-test", "When was the last successful restore test?", RESTORE, ["WI-0005"]
        ),
        ask_sample(
            "duration",
            "Why did last night's upgrade take longer than planned?",
            DURATION,
            ["WI-0008"],
        ),
        ask_sample(
            "upcoming", "What will next month's maintenance look like?", UPCOMING, ["WI-0018"]
        ),
        ask_sample("access", "What does the agent have access to right now?", ACCESS, ["WI-0012"]),
        ask_sample("no-access", "Does the agent hold any credential right now?", NO_ACCESS),
        ask_sample(
            "work-item-rollback",
            "What does this change do and how do I roll it back?",
            WORK_ITEM,
            ["WI-0016"],
        ),
        ask_sample(
            "unanswerable-cost",
            "How much did we spend on cloud resources last month?",
            GENERAL,
            answerable=False,
        ),
        ask_sample(
            "unanswerable-author",
            "Which engineer wrote the code of this Postgres release, and which CVEs does it fix?",
            WORK_ITEM,
            answerable=False,
        ),
        ask_sample(
            "unanswerable-time-of-day",
            "At what time of day did alice approve WI-0004, and from which IP address?",
            CHANGES,
            ["WI-0004"],
            answerable=False,
        ),
        ask_sample(
            "unanswerable-root-cause",
            "What was the root cause of the rollback of WI-0008?",
            CHANGES,
            ["WI-0008"],
            answerable=False,
        ),
    ]


def groundedness_verdicts(state: TaskState) -> dict[str, Verdict]:
    """Check one Ask answer against its records."""
    answer, source, meta = state.output.completion, source_text(state), state.metadata
    sentences = count_sentences(answer)
    missing = [i for i in meta["ids"] if i not in answer]
    invented = sorted(set(FACT.findall(answer)) - set(FACT.findall(source)))
    admits = first_match(NOT_ANSWERED, answer) is not None
    return {
        "at_most_six_sentences": (sentences <= MAX_SENTENCES, f"{sentences} sentences"),
        "names_work_items": (not missing, f"does not name {', '.join(missing)}"),
        "work_items_in_records": ids_grounded(answer, source),
        "versions_in_records": versions_grounded(answer, source),
        "dates_and_cves_in_records": (not invented, f"not in the records: {invented}"),
        "says_when_not_answered": (
            admits or meta["answerable"],
            "the records do not answer the question and the answer does not say so",
        ),
    }


@scorer(metrics={"*": [mean()]})
def groundedness() -> Scorer:
    """Score an Ask answer with the deterministic groundedness checks."""

    async def score(state: TaskState, target: Target) -> Score:
        return to_score(groundedness_verdicts(state), state.output.completion)

    return score


@task
def ask_groundedness() -> Task:
    """Task 7: answers use only the records, name work items and stay short."""
    return Task(
        dataset=groundedness_samples(),
        solver=role_turn("ask"),
        scorer=[groundedness(), rubric()],
    )
