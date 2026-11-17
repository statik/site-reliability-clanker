"""Live test of the forge: two hand-pushed pull requests that change the same file.

The git client pushes both branches over HTTP. The first is approved and applied. The plan
of the second is then stale: the control plane must merge main into the branch (a three-way
merge of one file), plan again and ask again.
"""
import json
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:7070/api/v1"
VARS = "platform/stacks/local/50-app/terraform.tfvars"
work = pathlib.Path(sys.argv[1])


def call(method, path, body=None, user="carol"):
    request = urllib.request.Request(
        BASE + path, method=method,
        headers={"X-Clanker-User": user, "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        return {"error": error.read().decode(), "status": error.code}


def git(*args, cwd):
    # After a fetch, git can repack the objects in the background. The fsck at the end then
    # fails now and then, because it reads the objects while they move.
    subprocess.run(["git", "-c", "user.name=carol", "-c", "user.email=carol@example.com",
                    "-c", "gc.auto=0", "-c", "maintenance.auto=false", *args],
                   cwd=cwd, check=True, capture_output=True)


def state(item):
    return call("GET", f"/work-items/{item}")["work_item"]["state"]


def wait(item, wanted, limit=420):
    deadline = time.time() + limit
    while time.time() < deadline:
        now = state(item)
        if now in wanted:
            return now
        time.sleep(4)
    raise SystemExit(f"{item} is {state(item)} after {limit} s, wanted one of {wanted}")


git("clone", "-q", "http://127.0.0.1:7070/git/platform-live.git", str(work), cwd=work.parent)
base = subprocess.run(["git", "rev-parse", "HEAD"], cwd=work, capture_output=True, text=True).stdout.strip()
changes = {"human/more-api": ("api_replicas      = 1", "api_replicas      = 2", "Run two api replicas"),
           "human/no-loadgen": ("loadgen_enabled   = true", "loadgen_enabled   = false", "Stop synthetic traffic")}
items = {}
for branch, (old, new, title) in changes.items():
    git("checkout", "-q", "-b", branch, base, cwd=work)
    path = work / VARS
    path.write_text(path.read_text().replace(old, new))
    git("commit", "-q", "-am", title, cwd=work)
    git("push", "-q", "origin", branch, cwd=work)
    answer = call("POST", "/forge/pulls", {"branch": branch, "title": title, "body": "Hand-written change", "env": "local"})
    items[branch] = answer["work_item"]
    print(f"pushed {branch} and opened pull request {answer['pull']['id']} -> {items[branch]}", flush=True)

first, second = items["human/more-api"], items["human/no-loadgen"]
for item in (first, second):
    wait(item, {"AwaitingApproval"})
print("both wait for approval", flush=True)

print("approve the first:", call("POST", f"/work-items/{first}/decision", {"decision": "approve", "comment": "stale test"}, user="alice").get("state"), flush=True)
print("first ended:", wait(first, {"Done", "Failed", "RolledBack"}), flush=True)

answer = call("POST", f"/work-items/{second}/decision", {"decision": "approve", "comment": "stale test"}, user="alice")
print("approve the second (its plan is stale now):", answer.get("state") or answer, flush=True)
after = wait(second, {"AwaitingApproval", "Done", "Failed"})
print("second after the stale approval:", after, flush=True)
if after == "AwaitingApproval":
    answer = call("POST", f"/work-items/{second}/decision", {"decision": "approve", "comment": "stale test, second time"}, user="alice")
    print("approve the second again:", answer.get("state") or answer, flush=True)
    after = wait(second, {"Done", "Failed", "RolledBack"})
print("second ended:", after, flush=True)

events = call("GET", f"/events?work_item={second}&order=asc&limit=200")
events = events["events"] if isinstance(events, dict) else events
for event in events:
    if any(word in event["type"] for word in ("refused", "stale", "superseded", "plan.created", "approval")):
        print("  ", event["type"], "|", event["summary"][:110], flush=True)

git("fetch", "-q", "origin", cwd=work)
content = subprocess.run(["git", "show", f"origin/main:{VARS}"], cwd=work, capture_output=True, text=True).stdout
print(content)
subprocess.run(["git", "fsck", "--strict", "--no-dangling"], cwd=work, check=True, capture_output=True)
ok = "api_replicas      = 2" in content and "loadgen_enabled   = false" in content and after == "Done"
print("RESULT:", "both changes are on main and the objects pass git fsck" if ok else "FAILED")
sys.exit(0 if ok else 1)
