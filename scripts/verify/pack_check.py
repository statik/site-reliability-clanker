"""Check an application pack on the running local control plane. It needs no model key.

Usage: pack_check.py <pack> [--upgrade-to V] [--bad V --expect Failed|RolledBack]

<pack> is the directory of the pack that the control plane runs, for example apps/paperboat.
The script waits for the blank-slate work item, approves it and checks that the verification
reports the first release of the pack. Then it runs the runbook restart-component for each
component that the definition of the runbook permits. With --upgrade-to it pushes a branch
that sets the version line of the application layer with the git client, opens the pull
request through the API, approves it and checks the verification. With --bad it does the same
with a version whose verification must fail and checks the end state. At the end it verifies
the audit chain. The approvals go through the API as the demo users, as in month.py.

In replay mode no step needs a model. The script opens its own pull requests, so the Author
does not run. The Explainer writes the template text when the pack has no recording, and the
Reviewer gives no review.
"""

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

# CLANKER_URL names a control plane on another address, as in month.py.
ROOT = os.environ.get("CLANKER_URL", "http://127.0.0.1:7070")
BASE = ROOT + "/api/v1"
ENV = "local"
TERMINAL = frozenset({"Done", "Failed", "RolledBack", "Rejected"})
RESTART = "restart-component"
RESTART_INPUT = "component"
RUNNING = "running version is "
BLANK_SLATE_SECONDS = 1800
CHANGE_SECONDS = 900
RUNBOOK_SECONDS = 600
POLL_SECONDS = 5


class CheckError(Exception):
    """A check of the pack did not pass."""


@dataclass(frozen=True)
class Pack:
    """The values of the manifest of a pack that the checks use."""

    name: str
    first: str
    variable: str
    tfvars: str


def load_pack(directory: Path) -> Pack:
    """Read the manifest of the pack in directory."""
    path = directory / "pack.json"
    try:
        manifest = json.loads(path.read_text(encoding="utf-8"))
        return Pack(
            name=manifest["name"],
            first=manifest["releases"]["first"],
            variable=manifest["version_variable"],
            tfvars=f"platform/stacks/local/{manifest['layer']}/terraform.tfvars",
        )
    except (OSError, ValueError, KeyError, TypeError) as error:
        raise CheckError(f"read the manifest {path}: {error!r}") from error


def set_version(text: str, variable: str, version: str) -> tuple[str, str]:
    """Set the version line of a tfvars file.

    Args:
        text: The content of the tfvars file.
        variable: The name of the version variable of the pack.
        version: The new version.

    Returns:
        The new content and the version that the line held before.

    Raises:
        CheckError: The file does not have exactly one line that sets the variable.
    """
    line = re.compile(rf'^(\s*{re.escape(variable)}\s*=\s*)"([^"]*)"', re.MULTILINE)
    found = line.findall(text)
    if len(found) != 1:
        raise CheckError(f'the tfvars must set {variable} = "..." on one line; found {len(found)}')
    return line.sub(lambda match: f'{match.group(1)}"{version}"', text), found[0][1]


def running_versions(events: list[dict[str, Any]]) -> list[str]:
    """Return the versions that the passed verifications among events report."""
    versions = []
    for event in events:
        if event.get("type") != "verify.passed":
            continue
        for check in (event.get("data") or {}).get("checks") or []:
            if check.startswith(RUNNING):
                versions.append(check.removeprefix(RUNNING))
    return versions


def restart_components(runbooks: list[dict[str, Any]]) -> list[str]:
    """Return the components that the restart runbook permits.

    Raises:
        CheckError: The runbook is missing, has no standing approval or lists no component.
    """
    for definition in runbooks:
        if definition.get("name") != RESTART:
            continue
        if not definition.get("standing_approver"):
            raise CheckError(f"the runbook {RESTART} has no standing approval")
        field = (definition.get("inputs") or {}).get(RESTART_INPUT) or {}
        components = list(field.get("values") or [])
        if not components:
            raise CheckError(f"the input {RESTART_INPUT} of {RESTART} lists no value")
        return components
    raise CheckError(f"the pack has no runbook {RESTART}")


def call(method: str, path: str, body: Any = None, user: str = "carol") -> Any:
    """Send one request to the API and return the decoded answer."""
    request = urllib.request.Request(
        BASE + path,
        method=method,
        headers={"X-Clanker-User": user, "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None,
    )
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        detail = error.read().decode()
        raise CheckError(f"{method} {path} answered {error.code}: {detail}") from error


def git(*args: str, cwd: Path) -> None:
    """Run the git client as the demo user carol, as stale_plan.py does."""
    command = ["git", "-c", "user.name=carol", "-c", "user.email=carol@example.com"]
    command += ["-c", "gc.auto=0", "-c", "maintenance.auto=false", *args]
    done = subprocess.run(command, cwd=cwd, capture_output=True, text=True, check=False)
    if done.returncode != 0:
        raise CheckError(f"git {' '.join(args)} failed: {done.stderr.strip()}")


def first_approver() -> str:
    """Return the user who approves first: the person from the wizard, or the demo user."""
    return call("GET", "/auth").get("default_user") or "alice"


def approve(detail: dict[str, Any]) -> None:
    """Approve a waiting work item as the demo approvers, as many as its tier needs."""
    item = detail["work_item"]
    approved = set(detail.get("valid_approvers") or [])
    needed = detail["approvers_required"] - len(approved)
    for user in (first_approver(), "bob"):
        if needed <= 0:
            return
        if user in approved:
            continue
        body = {"decision": "approve", "comment": "pack check"}
        answer = call("POST", f"/work-items/{item['id']}/decision", body, user=user)
        print(
            f"  approve {item['id']} ({item['tier']}) as {user}: {answer.get('state')}", flush=True
        )
        needed -= 1
    if needed > 0:
        raise CheckError(f"{item['id']} needs {needed} more approvers than the demo has")


def drive(work_item: str, timeout: int) -> dict[str, Any]:
    """Approve the work item whenever it waits, and return its detail at an end state."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        detail = call("GET", f"/work-items/{work_item}")
        state = detail["work_item"]["state"]
        if state in TERMINAL:
            item = detail["work_item"]
            print(
                f"  {work_item} [{item['kind']}] {item['tier']} -> {state}: {item['title']}",
                flush=True,
            )
            return detail
        if state == "AwaitingApproval":
            approve(detail)
        time.sleep(POLL_SECONDS)
    raise CheckError(f"{work_item} is not at an end state after {timeout} s")


def expect_state(detail: dict[str, Any], wanted: str) -> None:
    """Fail unless the work item of detail is in the wanted state."""
    item = detail["work_item"]
    if item["state"] != wanted:
        raise CheckError(f"{item['id']} ended {item['state']}, wanted {wanted}")


def expect_running(detail: dict[str, Any], version: str) -> None:
    """Fail unless a passed verification of the work item reports the running version."""
    item = detail["work_item"]["id"]
    found = running_versions(detail["events"])
    if version not in found:
        raise CheckError(
            f"no passed verification of {item} says '{RUNNING}{version}'; they report {found}"
        )
    print(f"  {item}: {RUNNING}{version}", flush=True)


def wait_for_new(known: set[str], kind: str, timeout: int) -> str:
    """Return the first work item of the kind that is not in known."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        for item in call("GET", "/work-items"):
            if item["kind"] == kind and item["id"] not in known:
                return item["id"]
        time.sleep(POLL_SECONDS)
    raise CheckError(f"no new work item of kind {kind} after {timeout} s")


def check_pack_runs(pack: Pack) -> None:
    """Fail unless the control plane loaded this pack the last time it started."""
    events = call("GET", "/events?types=pack.loaded&limit=1")
    loaded = (events[0].get("data") or {}).get("name") if events else None
    if loaded != pack.name:
        raise CheckError(f"the control plane runs the pack {loaded}, not {pack.name}")


def check_blank_slate(pack: Pack) -> None:
    """Approve the blank slate and check that it runs the first release."""
    print(f"== blank slate of {pack.name}", flush=True)
    work_item = wait_for_new(set(), "blank-slate", BLANK_SLATE_SECONDS)
    detail = drive(work_item, BLANK_SLATE_SECONDS)
    expect_state(detail, "Done")
    expect_running(detail, pack.first)
    print(f"  explanation: {detail['work_item']['explanation'].get('source')}", flush=True)


def check_restarts() -> None:
    """Run the restart runbook for each component that it permits, one after the other."""
    components = restart_components(call("GET", "/runbooks"))
    print(f"== {RESTART} for {', '.join(components)}", flush=True)
    for component in components:
        known = {item["id"] for item in call("GET", "/work-items")}
        body = {"env": ENV, "params": {RESTART_INPUT: component}}
        call("POST", f"/runbooks/{RESTART}", body, user=first_approver())
        work_item = wait_for_new(known, f"runbook:{RESTART}", RUNBOOK_SECONDS)
        expect_state(drive(work_item, RUNBOOK_SECONDS), "Done")


def push_version(pack: Pack, version: str, purpose: str) -> tuple[str, str]:
    """Push a branch that sets the version of the application and open its pull request.

    Returns:
        The work item of the pull request and the version that main held before.
    """
    branch = f"pack-check/{purpose}-{version}"
    title = f"Set {pack.variable} to {version} ({purpose})"
    with tempfile.TemporaryDirectory(prefix="pack-check-") as work:
        clone = Path(work) / "platform-live"
        git("clone", "-q", f"{ROOT}/git/platform-live.git", str(clone), cwd=Path(work))
        git("checkout", "-q", "-b", branch, cwd=clone)
        path = clone / pack.tfvars
        text, previous = set_version(path.read_text(encoding="utf-8"), pack.variable, version)
        path.write_text(text, encoding="utf-8")
        # S7: a bad version of a pack with release records also needs its test record here.
        git("commit", "-q", "-am", title, cwd=clone)
        git("push", "-q", "origin", branch, cwd=clone)
    body = {"branch": branch, "title": title, "body": "Pack check", "env": ENV}
    answer = call("POST", "/forge/pulls", body)
    print(
        f"  pushed {branch}, pull request {answer['pull']['id']} -> {answer['work_item']}",
        flush=True,
    )
    return answer["work_item"], previous


def check_upgrade(pack: Pack, version: str) -> None:
    """Upgrade the application to version and check the verification."""
    print(f"== upgrade to {version}", flush=True)
    work_item, _ = push_version(pack, version, "upgrade")
    detail = drive(work_item, CHANGE_SECONDS)
    expect_state(detail, "Done")
    expect_running(detail, version)


def check_bad(pack: Pack, version: str, expect: str) -> str:
    """Deploy a version whose verification fails and check the end state.

    Returns:
        The work item of the bad version.
    """
    print(f"== bad version {version}, expect {expect}", flush=True)
    work_item, previous = push_version(pack, version, "bad")
    detail = drive(work_item, CHANGE_SECONDS)
    expect_state(detail, expect)
    failed = [e["summary"] for e in detail["events"] if e["type"] == "verify.failed"]
    if not failed:
        raise CheckError(f"{work_item} ended {expect} without a failed verification")
    print(f"  {failed[0]}", flush=True)
    if expect == "RolledBack":
        rollbacks = [w["id"] for w in call("GET", "/work-items") if w["rollback_of"] == work_item]
        if not rollbacks:
            raise CheckError(f"{work_item} ended RolledBack and no rollback work item exists")
        rollback = drive(rollbacks[0], CHANGE_SECONDS)
        expect_state(rollback, "Done")
        expect_running(rollback, previous)
    return work_item


def check_end(expected_failures: set[str]) -> None:
    """Fail when a work item is in flight or failed unexpectedly, or the chain is broken."""
    items = call("GET", "/work-items")
    in_flight = [w["id"] for w in items if w["state"] not in TERMINAL]
    failed = [w["id"] for w in items if w["state"] == "Failed" and w["id"] not in expected_failures]
    chain = call("GET", "/audit/verify")
    print("chain:", chain, flush=True)
    if in_flight or failed or not chain.get("ok"):
        raise CheckError(f"in flight: {in_flight}; failed: {failed}; chain ok: {chain.get('ok')}")
    print(f"  {len(items)} work items are at an end state", flush=True)


def parse(argv: list[str]) -> argparse.Namespace:
    """Read the command line."""
    parser = argparse.ArgumentParser(description="Check an application pack on the local target.")
    parser.add_argument("pack", type=Path, help="directory of the pack, e.g. apps/paperboat")
    parser.add_argument("--upgrade-to", metavar="V", help="a release to upgrade to")
    parser.add_argument("--bad", metavar="V", help="a version whose verification fails")
    parser.add_argument("--expect", choices=["Failed", "RolledBack"], help="end state of --bad")
    args = parser.parse_args(argv)
    if (args.bad is None) != (args.expect is None):
        parser.error("--bad and --expect go together")
    return args


def main(argv: list[str]) -> int:
    """Run the checks and print the result."""
    args = parse(argv)
    try:
        pack = load_pack(args.pack)
        check_pack_runs(pack)
        check_blank_slate(pack)
        check_restarts()
        if args.upgrade_to:
            check_upgrade(pack, args.upgrade_to)
        expected_failures = set()
        if args.bad:
            bad = check_bad(pack, args.bad, args.expect)
            if args.expect == "Failed":
                expected_failures.add(bad)
        check_end(expected_failures)
    except CheckError as error:
        print(f"RESULT: failed: {error}", flush=True)
        return 1
    print(f"RESULT: every check of the pack {pack.name} passed and the chain verifies", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
