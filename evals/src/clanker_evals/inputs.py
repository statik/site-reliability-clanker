"""Builders for the user messages that the Go runtime sends to each role.

Each builder writes the same text as the Go code: the JSON field order of the Go structs,
sorted keys for Go maps, escaped <, > and &, the same truncation and the same <data> frame.
Every role frames its input with frame(), as internal/agent/agent.go does.
"""

from typing import Any

from clanker_evals.gotext import go_json, sorted_map, truncate

RELEASE_NOTE_LIMIT = 4000
TRANSCRIPT_LIMIT = 14000
REVIEW_DIFF_LIMIT = 12000
TIERS = ["T0", "T1", "T2", "T3"]
# The history that recentChanges gives for an environment that has run 1.0.0 for nine days.
QUIET_HISTORY = ["WI-0001 Create environment local: Done, 216h0m0s ago"]


def frame(value: Any) -> str:
    """Wrap untrusted input as frame does in Go: <data>, the value as JSON, </data>.

    The JSON encoding escapes < and >, so no text inside the value can close the element.
    """
    return "<data>" + go_json(value) + "</data>"


def finding(detector: str, key: str, title: str, detail: dict[str, Any]) -> dict[str, Any]:
    """Build a finding with the JSON shape of detect.Finding.

    Args:
        detector: The detector that opened it: release, advisory, drift or health.
        key: The unique key, for example "release:local:1.1.0".
        title: The title that a person reads.
        detail: The detail map. Go writes its keys in sorted order.

    Returns:
        The finding as a dictionary in the field order of the Go struct.
    """
    return {
        "id": 7,
        "detector": detector,
        "env": "local",
        "key": key,
        "title": title,
        "detail": sorted_map(detail),
        "state": "open",
        "work_item": "",
        "at": "2026-03-02T09:00:00Z",
    }


def author_input(found: dict[str, Any], cloud: str = "local") -> str:
    """Return the user message of Agent.Draft: the finding, the cloud and the tfvars path."""
    tfvars_path = f"platform/stacks/{cloud}/<layer>/terraform.tfvars"
    return frame({"cloud": cloud, "finding": found, "tfvars_path": tfvars_path})


def resource(address: str, action: str, details: list[str] | None = None) -> dict[str, Any]:
    """Build one runner.ResourceChange. The type and name come from the address."""
    parts = address.split(".")
    return {
        "address": address,
        "type": parts[-2],
        "name": parts[-1],
        "action": action,
        "details": details or [],
    }


def plan(
    layer: str,
    resources: list[dict[str, Any]],
    policy: tuple[str, str, list[str]],
    outputs: list[str] | None = None,
) -> dict[str, Any]:
    """Build one orchestrator.Plan with its facts and its policy decision.

    Args:
        layer: The layer that the plan belongs to.
        resources: The changed resources, from resource().
        policy: The tier, the rule that set it and the reasons, as the policy code gives them.
        outputs: The names of changed outputs.

    Returns:
        The plan in the field order of the Go struct. The counts come from the resources.
    """
    actions = [r["action"] for r in resources]
    tier, rule, reasons = policy
    return {
        "id": 3,
        "work_item": "WI-0007",
        "layer": layer,
        "commit": "4f0c2d9a7b1e",
        "plan_sha256": "9b1d6c0e5a3f",
        "facts": {
            "add": actions.count("create"),
            "change": actions.count("update"),
            "destroy": actions.count("destroy"),
            "replace": actions.count("replace"),
            "resources": resources,
            "outputs": outputs or [],
        },
        "policy": {
            "tier": tier,
            "rule": rule,
            "reasons": reasons,
            "create_only": bool(actions) and set(actions) == {"create"},
        },
        "superseded": False,
        "created_at": "2026-03-02T09:05:00Z",
    }


def explain_input(
    work_item: dict[str, Any], plans: list[dict[str, Any]], release_note: str | None = None
) -> str:
    """Return the user message of Agent.Explain.

    Args:
        work_item: The keys title, kind, layers and trigger. The keys tier and tier_rule are
            optional; without them the highest plan tier and its rule are used.
        plans: The plans of the layers that could be planned. The other layers are deferred.
        release_note: The release note, when the change is an application upgrade.

    Returns:
        The facts as JSON inside the <data> frame.
    """
    top = max(plans, key=lambda p: TIERS.index(p["policy"]["tier"]))["policy"] if plans else {}
    planned = {p["layer"] for p in plans}
    facts: dict[str, Any] = {
        "title": work_item["title"],
        "kind": work_item["kind"],
        "env": "local",
        "layers": work_item["layers"],
        "tier": work_item.get("tier", top.get("tier", "")),
        "tier_rule": work_item.get("tier_rule", top.get("rule", "")),
        "trigger": work_item["trigger"],
        "plans": plans,
        "deferred_layers": [layer for layer in work_item["layers"] if layer not in planned],
    }
    if release_note is not None:
        facts["release_note"] = truncate(release_note, RELEASE_NOTE_LIMIT)
    return frame(sorted_map(facts))


def review_input(
    work_item: dict[str, Any],
    plans: list[dict[str, Any]],
    policy_tier: str | None = None,
    diff: str | None = None,
) -> str:
    """Return the user message of Agent.Review, as reviewInput and frame write it.

    Args:
        work_item: The keys title, kind, layers and trigger. The key env is optional; without
            it the environment is "local".
        plans: The plans with the decision of the policy engine for each. A layer of the
            work item without a plan is a deferred layer.
        policy_tier: The tier that the policy engine gave the work item. None takes the
            highest tier of the plans. Give T3 when a layer is deferred and no plan is T3.
        diff: The diff of the pull request. None is a work item without a pull request, so
            the input has no diff field.

    Returns:
        The input of the review as JSON inside the <data> frame.
    """
    highest = max((p["policy"]["tier"] for p in plans), default=TIERS[0])
    planned = {p["layer"] for p in plans}
    facts: dict[str, Any] = {
        "title": work_item["title"],
        "kind": work_item["kind"],
        "env": work_item.get("env", "local"),
        "layers": work_item["layers"],
        "trigger": work_item["trigger"],
        "policy_tier": policy_tier or highest,
        "plans": plans,
        "deferred_layers": [layer for layer in work_item["layers"] if layer not in planned],
    }
    if diff is not None:
        facts["diff"] = truncate(diff, REVIEW_DIFF_LIMIT)
    return frame(sorted_map(facts))


def debug_transcript(pods: str, deployments: str, events: str, logs: str) -> str:
    """Build the transcript of a debug session from the output of its four fixed commands."""
    commands = [
        ("get pods -n paperboat -o wide", pods),
        ("get deployments -n paperboat", deployments),
        ("get events -n paperboat --sort-by=.lastTimestamp", events),
        ("logs -n paperboat deployment/paperboat-api --tail=40", logs),
    ]
    return "".join(f"$ kubectl {command}\n{output.strip()}\n" for command, output in commands)


def investigator_input(
    alert: dict[str, Any], transcript: str, history: list[str] | None = None
) -> str:
    """Return the user message of Agent.diagnose.

    Args:
        alert: The incident finding.
        transcript: The transcript of the debug session.
        history: The lines of recentChanges, newest first. None gives QUIET_HISTORY.

    Returns:
        The alert, the change history and the transcript as one framed JSON object.
    """
    return frame(
        {
            "alert": alert,
            "change_history": QUIET_HISTORY if history is None else history,
            "debug_session": truncate(transcript, TRANSCRIPT_LIMIT),
        }
    )


def ask_input(question: str, records: list[str]) -> str:
    """Return the user message of Agent.Ask. The question is outside the <data> frame."""
    return "Question: " + question + "\n" + frame(records)
