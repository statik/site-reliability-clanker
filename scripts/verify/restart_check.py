"""Live test of a stop in the middle of an apply, and of two control planes on one database.

Run it from the repository root against the local demo after a complete run. It starts a
second control plane, which must wait. It opens a pull request, approves it, kills the
control plane while the apply runs, and starts it again. The work item must then be Failed,
its job lost and no grant open.

It leaves a failed work item and possibly a partly applied change. Run
scripts/verify/fresh-run.sh for a clean demo afterward.
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
problems = []


def call(method, path, body=None, user="carol", base=BASE):
    request = urllib.request.Request(
        base + path, method=method,
        headers={"X-Clanker-User": user, "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        return {"error": error.read().decode(), "status": error.code}


def git(*args, cwd):
    subprocess.run(["git", "-c", "user.name=carol", "-c", "user.email=carol@example.com", *args],
                   cwd=cwd, check=True, capture_output=True)


def answers(base=BASE):
    try:
        urllib.request.urlopen(base + "/work-items", timeout=2).close()
        return True
    except OSError:
        return False


def state(item):
    return call("GET", f"/work-items/{item}")["work_item"]["state"]


def wait(item, wanted, limit=300):
    deadline = time.time() + limit
    while time.time() < deadline:
        now = state(item)
        if now in wanted:
            return now
        time.sleep(0.5)
    raise SystemExit(f"{item} is {state(item)} after {limit} s, wanted one of {wanted}")


def check(ok, text):
    print(("ok      " if ok else "FAILED  ") + text, flush=True)
    if not ok:
        problems.append(text)


print("== a second control plane on the same database must wait", flush=True)
work.mkdir()
second_log = work / "second.log"
with second_log.open("w") as log:
    second = subprocess.Popen(["./bin/clanker", "up", "--addr", "127.0.0.1:7172", "--sink-addr",
                               "127.0.0.1:7173"], stdout=log, stderr=subprocess.STDOUT)
time.sleep(15)
check(second.poll() is None, "the second control plane still runs")
check(not answers("http://127.0.0.1:7172/api/v1"), "the second control plane serves nothing")
check("another control plane writes" in second_log.read_text(), "its log says that it waits")
second.terminate()
second.wait(timeout=20)

print("== open a pull request and approve it", flush=True)
clone = work / "clone"
git("clone", "-q", "http://127.0.0.1:7070/git/platform-live.git", str(clone), cwd=work)
git("checkout", "-q", "-b", "human/three-web", cwd=clone)
path = clone / VARS
before = path.read_text()
after = before.replace("web_replicas      = 1", "web_replicas      = 3")
if after == before:
    raise SystemExit(f"{VARS} has no line 'web_replicas      = 1': run fresh-run.sh first")
path.write_text(after)
git("commit", "-q", "-am", "Run three web replicas", cwd=clone)
git("push", "-q", "origin", "human/three-web", cwd=clone)
answer = call("POST", "/forge/pulls", {"branch": "human/three-web", "title": "Run three web replicas",
                                       "body": "Restart check", "env": "local"})
item = answer["work_item"]
wait(item, {"AwaitingApproval"})
events_before = call("GET", "/audit/verify")["count"]
call("POST", f"/work-items/{item}/decision", {"decision": "approve", "comment": "restart check"}, user="alice")
wait(item, {"Applying"}, limit=120)

print(f"== kill the control plane while {item} is in the apply", flush=True)
subprocess.run(["pkill", "-9", "-f", "clanker up"], check=True)
time.sleep(2)
check(not answers(), "the control plane is down")
with pathlib.Path(".clanker/up.log").open("a") as log:
    subprocess.Popen(["./bin/clanker", "up"], stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
deadline = time.time() + 120
while time.time() < deadline and not answers():
    time.sleep(1)
check(answers(), "the control plane answers again")

detail = call("GET", f"/work-items/{item}")
check(detail["work_item"]["state"] == "Failed", f"{item} is Failed (it is {detail['work_item']['state']})")
statuses = [job["status"] for job in detail.get("jobs") or []]
check("lost" in statuses and "running" not in statuses, f"its jobs are {statuses}: one lost, none running")
active = call("GET", "/access")["active"]
check(not active, f"no grant is open ({len(active or [])} are)")
chain = call("GET", "/audit/verify")
check(chain["ok"] and chain["count"] > events_before, f"the audit chain verifies at {chain['count']} events")
events = call("GET", f"/events?work_item={item}&limit=200")
events = events["events"] if isinstance(events, dict) else events
reasons = [event["summary"] for event in events
           if "Failed" in event["summary"] or "revoked" in event["summary"]]
print("   ", *reasons, sep="\n    ", flush=True)
others = [w["id"] for w in call("GET", "/work-items") if w["state"] in ("Applying", "Verifying")]
check(not others, f"no work item is in an apply or a verification ({others})")
check("ended the work that the last stop interrupted" in pathlib.Path(".clanker/up.log").read_text(),
      "the log of the control plane names the reconcile")

print("RESULT:", "the stop in the apply was reconciled and the second control plane waited"
      if not problems else f"{len(problems)} checks FAILED")
sys.exit(1 if problems else 0)
