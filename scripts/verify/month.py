"""Drive the scripted month through the HTTP API: run a scene, approve what waits, wait for the end."""
import json
import sys
import time
import urllib.request

BASE = "http://127.0.0.1:7070/api/v1"
TERMINAL = {"Done", "Failed", "RolledBack", "Rejected"}

def call(method, path, body=None, user="carol"):
    req = urllib.request.Request(BASE + path, method=method, headers={"X-Clanker-User": user, "Content-Type": "application/json"},
                                 data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as err:
        return {"error": err.read().decode()}

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
                approvers = ["alice", "bob"] if w["tier"] == "T3" else ["alice"]
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
print("chain:", call("GET", "/audit/verify"))
