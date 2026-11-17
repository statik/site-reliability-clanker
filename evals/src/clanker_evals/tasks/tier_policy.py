"""Task 9: risk tiers come from policy code. This task calls no model.

The solver runs `opa eval` on runbooks/policy with a plan fixture, as internal/policy does,
and the scorer compares the decision with the expected tier and rule.
"""

import asyncio
import json
import tempfile
from pathlib import Path
from typing import Any

from inspect_ai import Task, task
from inspect_ai.dataset import Sample
from inspect_ai.model import ModelOutput
from inspect_ai.scorer import Score, Scorer, Target, mean, scorer
from inspect_ai.solver import Generate, Solver, TaskState, solver

from clanker_evals.checks import to_score
from clanker_evals.repo import POLICY_DIR

QUERY = '{"decision": data.clanker.tier.decision, "create_only": data.clanker.tier.create_only}'


def change(kind: str, actions: list[str], **values: Any) -> dict[str, Any]:
    """Build one resource change of `tofu show -json` for a resource type."""
    return {"address": f"{kind}.x", "type": kind, "change": {"actions": actions, **values}}


def image(tag: str) -> dict[str, Any]:
    """Build the part of a stateful set that holds the container image."""
    return {"spec": [{"template": [{"spec": [{"container": [{"image": f"postgres:{tag}"}]}]}]}]}


def stateful_update(old: str, new: str) -> dict[str, Any]:
    """Build an in-place update of the Postgres stateful set from one image tag to another."""
    return change("kubernetes_stateful_set_v1", ["update"], before=image(old), after=image(new))


# name, kind, layer, resource changes, tier, rule, create_only
FIXTURES: list[tuple[str, str, str, list[dict[str, Any]], str, str, bool]] = [
    ("no-changes", "drift", "50-app", [], "T0", "no-changes", True),
    (
        "read-and-no-op",
        "drift",
        "50-app",
        [change("helm_release", ["no-op"]), change("kubernetes_secret_v1", ["read"])],
        "T0",
        "no-changes",
        True,
    ),
    (
        "app-release",
        "app-upgrade",
        "50-app",
        [change("helm_release", ["update"])],
        "T2",
        "normal-change",
        False,
    ),
    (
        "create-only",
        "change",
        "40-platform",
        [change("kubernetes_namespace_v1", ["create"])],
        "T2",
        "normal-change",
        True,
    ),
    (
        "stateful-replace",
        "change",
        "30-data",
        [change("kubernetes_stateful_set_v1", ["delete", "create"])],
        "T3",
        "stateful-destroy",
        False,
    ),
    (
        "volume-claim-destroy",
        "change",
        "30-data",
        [change("kubernetes_persistent_volume_claim_v1", ["delete"])],
        "T3",
        "stateful-destroy",
        False,
    ),
    (
        "cloud-database-destroy",
        "change",
        "30-data",
        [change("aws_db_instance", ["delete"])],
        "T3",
        "stateful-destroy",
        False,
    ),
    (
        "stateful-create",
        "change",
        "30-data",
        [change("kubernetes_stateful_set_v1", ["create"])],
        "T2",
        "normal-change",
        True,
    ),
    (
        "config-update-in-data-layer",
        "drift",
        "30-data",
        [change("kubernetes_config_map_v1", ["update"])],
        "T2",
        "normal-change",
        False,
    ),
    (
        "network-layer",
        "change",
        "10-network",
        [change("docker_container", ["update"])],
        "T3",
        "high-risk-layer",
        False,
    ),
    (
        "bootstrap-layer",
        "change",
        "00-bootstrap",
        [change("local_file", ["create"])],
        "T3",
        "high-risk-layer",
        True,
    ),
    (
        "identity-aws",
        "change",
        "20-cluster",
        [change("aws_iam_role_policy", ["update"])],
        "T3",
        "identity-change",
        False,
    ),
    (
        "identity-gcp",
        "change",
        "20-cluster",
        [change("google_service_account", ["create"])],
        "T3",
        "identity-change",
        True,
    ),
    (
        "identity-azure",
        "change",
        "20-cluster",
        [change("azurerm_role_assignment", ["delete"])],
        "T3",
        "identity-change",
        False,
    ),
    (
        "blank-slate",
        "blank-slate",
        "20-cluster",
        [change("kind_cluster", ["create"])],
        "T3",
        "whole-environment",
        True,
    ),
    (
        "teardown",
        "teardown",
        "50-app",
        [change("helm_release", ["delete"])],
        "T3",
        "whole-environment",
        False,
    ),
    (
        "database-minor-version",
        "postgres-patch",
        "30-data",
        [stateful_update("17.11-alpine", "17.12-alpine")],
        "T2",
        "normal-change",
        False,
    ),
    (
        "database-major-version",
        "change",
        "30-data",
        [stateful_update("17.11-alpine", "18.1-alpine")],
        "T3",
        "database-major-version",
        False,
    ),
    (
        "several-rules-first-by-name",
        "teardown",
        "10-network",
        [change("docker_network", ["delete"])],
        "T3",
        "high-risk-layer",
        False,
    ),
]


def policy_samples() -> list[Sample]:
    """Return one sample for each plan fixture."""
    samples = []
    for name, kind, layer, changes, tier, rule, create_only in FIXTURES:
        policy_input = {"kind": kind, "layer": layer, "plan": {"resource_changes": changes}}
        expected = {"tier": tier, "rule": rule, "create_only": create_only}
        samples.append(Sample(id=name, input=json.dumps(policy_input), target=json.dumps(expected)))
    return samples


async def decide(policy_input: str) -> dict[str, Any]:
    """Run `opa eval` on the policy directory and return tier, rule and create_only."""
    with tempfile.NamedTemporaryFile("w", suffix=".json") as handle:
        handle.write(policy_input)
        handle.flush()
        process = await asyncio.create_subprocess_exec(
            "opa", "eval", "--format", "json", "--data", str(Path(POLICY_DIR)),
            "--input", handle.name, QUERY,
            stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
        )  # fmt: skip
        stdout, stderr = await process.communicate()
    if process.returncode != 0:
        raise RuntimeError(f"opa eval failed for policies in runbooks/policy: {stderr.decode()}")
    value = json.loads(stdout)["result"][0]["expressions"][0]["value"]
    decision = value["decision"]
    return {"tier": decision["tier"], "rule": decision["rule"], "create_only": value["create_only"]}


@solver
def opa_decision() -> Solver:
    """Put the decision of the policy code in the output. No model is called."""

    async def solve(state: TaskState, generate: Generate) -> TaskState:
        decision = await decide(state.input_text)
        state.output = ModelOutput.from_content("opa", json.dumps(decision))
        return state

    return solve


@scorer(metrics={"*": [mean()]})
def decision_matches() -> Scorer:
    """Score the policy decision against the expected tier, rule and create-only flag."""

    async def score(state: TaskState, target: Target) -> Score:
        got, want = json.loads(state.output.completion), json.loads(target.text)
        verdicts = {
            key: (got[key] == want[key], f"got {got[key]!r}, expected {want[key]!r}")
            for key in ("tier", "rule", "create_only")
        }
        return to_score(verdicts, state.output.completion)

    return score


@task
def tier_policy_regression() -> Task:
    """Task 9: the tier of each plan fixture, decided by runbooks/policy/tier.rego."""
    return Task(dataset=policy_samples(), solver=opa_decision(), scorer=decision_matches())
