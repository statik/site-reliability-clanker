"""Drive the scripted month through the HTTP API: run a scene, approve what waits, wait for the end."""
import json
import os
import sys
import time
import urllib.request

# CLANKER_URL names a control plane on another address, for a run beside the demo.
BASE = os.environ.get("CLANKER_URL", "http://127.0.0.1:7070") + "/api/v1"
TERMINAL = {"Done", "Failed", "RolledBack", "Rejected"}

def call(method, path, body=None, user="carol"):
    req = urllib.request.Request(BASE + path, method=method, headers={"X-Clanker-User": user, "Content-Type": "application/json"},
                                 data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as err:
        return {"error": err.read().decode()}

def first_approver():
    """Return the user who approves first: the person from the wizard, or the demo user."""
    return call("GET", "/auth").get("default_user") or "alice"

def items():
    return {w["id"]: w for w in call("GET", "/work-items")}

def settle(known, timeout=1500):
    """Approve every waiting item and wait until no item is in flight."""
    deadline = time.time() + timeout
    quiet = 0
    while time.time() < deadline:
        current = items()
        busy = False
        for wid, w in current.items():
            if w["state"] == "AwaitingApproval":
                busy = True
                approvers = [first_approver(), "bob"] if w["tier"] == "T3" else [first_approver()]
                for a in approvers:
                    r = call("POST", f"/work-items/{wid}/decision", {"decision": "approve", "comment": "scripted month"}, user=a)
                    print(f"  approve {wid} ({w['tier']}) by {a}: {r.get('state', r)}", flush=True)
            elif w["state"] not in TERMINAL:
                busy = True
        quiet = 0 if busy else quiet + 1
        if quiet >= 6:
            break
        time.sleep(5)
    current = items()
    for wid, w in sorted(current.items()):
        if wid not in known or known[wid]["state"] != w["state"]:
            print(f"  {wid} [{w['kind']}] {w['tier']} -> {w['state']}: {w['title']}", flush=True)
    return current

known = items()
print("== first run", flush=True)
known = settle({}, timeout=1800)
for scene in sys.argv[1:]:
    print(f"== scene {scene}", flush=True)
    print(" ", call("POST", f"/sim/scenes/{scene}", {}), flush=True)
    time.sleep(8)
    known = settle(known)
chain = call("GET", "/audit/verify")
print("chain:", chain)
# A run with a failed item, an item in flight or a broken chain is not a pass. The scene
# bad-release ends in RolledBack on purpose, so that state is not a failure.
bad = sorted(
    wid for wid, w in known.items() if w["state"] == "Failed" or w["state"] not in TERMINAL
)
if bad or not chain.get("ok"):
    print(f"RESULT: failed. Items not done: {', '.join(bad) or 'none'}; chain ok: {chain.get('ok')}")
    sys.exit(1)
print(f"RESULT: {len(known)} work items reached an end state and the chain verifies")
