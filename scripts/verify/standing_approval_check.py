"""Live check of the standing approval of a runbook on a control plane that is not a demo.

Run it from the root of the checkout after scripts/verify/fresh-run.sh:

    python3 scripts/verify/standing_approval_check.py <empty work directory>

It restarts the control plane on the local target with --demo=false and the sign-in (the
fake identity provider). The approver alice signs in and revokes the standing approval of
db-backup, and a run of db-backup is refused. A viewer cannot sign. Then alice signs the
standing approval through the API, and db-backup runs to its end under her signature. At
the end it restarts the control plane as the demo. It stops only the control plane that
listens on the port of the demo.
"""
import http.cookiejar
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

PORT = 7070
BASE = f"http://127.0.0.1:{PORT}"
IDP = "http://127.0.0.1:7190"
LOG = pathlib.Path(".clanker/up.log")
SIGN_IN = {
    "CLANKER_OIDC_ISSUER": IDP, "CLANKER_OIDC_CLIENT_ID": "clanker",
    "CLANKER_OIDC_CLIENT_SECRET": "check", "CLANKER_PUBLIC_URL": BASE,
    "CLANKER_ADMIN_EMAIL": "carol@example.com"}
RUNBOOK = "db-backup"
APPROVAL = f"/runbooks/{RUNBOOK}/standing-approval"

failures = []


def check(ok, text):
    print(("ok      " if ok else "FAILED  ") + text, flush=True)
    if not ok:
        failures.append(text)


def eventually(what, condition, limit=600):
    deadline = time.time() + limit
    while time.time() < deadline:
        if condition():
            return
        time.sleep(2)
    raise SystemExit(f"FAILED: {what} did not happen in {limit} s")


class Client:
    """A browser with its own cookies."""

    def __init__(self):
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def send(self, method, address, body=None):
        data, headers = None, {}
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(address, method=method, data=data, headers=headers)
        try:
            with self.opener.open(request, timeout=120) as response:
                return response.status, response.read().decode()
        except urllib.error.HTTPError as error:
            return error.code, error.read().decode()
        except urllib.error.URLError as error:
            # The control plane does not listen yet, or not any more.
            return 0, str(error.reason)

    def call(self, method, path, body=None):
        """Send a request to the API and return the status and the decoded answer."""
        status, text = self.send(method, BASE + "/api/v1" + path, body)
        try:
            return status, json.loads(text)
        except json.JSONDecodeError:
            return status, text

    def api(self, method, path, body=None):
        status, answer = self.call(method, path, body)
        if status >= 300:
            raise SystemExit(f"FAILED: {method} {path} answered {status}: {answer}")
        return answer

    def sign_in(self, email):
        self.send("POST", f"{IDP}/person?email={urllib.parse.quote(email)}&verified=true")
        status, _ = self.send("GET", BASE + "/auth/login")
        if status != 200:
            raise SystemExit(f"FAILED: the sign-in of {email} answered {status}")
        return self


def stop_control_plane():
    """Stop the program that listens on the port of the demo, and nothing else."""
    found = subprocess.run(["lsof", "-nP", f"-iTCP:{PORT}", "-sTCP:LISTEN", "-t"],
                           capture_output=True, text=True, check=False)
    for pid in found.stdout.split():
        subprocess.run(["kill", pid], check=False)
    eventually("the control plane stops", lambda: Client().send("GET", BASE + "/healthz")[0] == 0,
               limit=60)


def start_control_plane(args, extra):
    stop_control_plane()
    log = LOG.open("a")
    subprocess.Popen(["./bin/clanker", "up", *args], stdout=log, stderr=log,
                     start_new_session=True, env={**os.environ, **extra})
    eventually("the control plane answers",
               lambda: Client().send("GET", BASE + "/healthz")[0] == 200, limit=90)


def runbook_row(client):
    return next(row for row in client.api("GET", "/runbooks") if row["name"] == RUNBOOK)


def refusals(alice, dave):
    """A viewer cannot sign, and a runbook with no standing approval does not run."""
    status, answer = dave.call("POST", APPROVAL)
    check(status == 403, f"a viewer cannot sign: {status} {answer}")
    before = runbook_row(alice)["standing_approver"]
    if before != "":
        answer = alice.api("DELETE", APPROVAL)
        check(answer["signer"] == "", f"alice revoked the signature of {before}")
    check(runbook_row(alice)["standing_approver"] == "", "the list shows no standing approval")
    status, answer = alice.call("POST", f"/runbooks/{RUNBOOK}", {"env": "local"})
    check(status == 409 and "has no standing approval" in str(answer),
          f"a run with no standing approval is refused: {status} {answer}")
    return before


def sign_and_run(alice):
    """alice signs the standing approval, and the runbook runs under it."""
    signed = alice.api("POST", APPROVAL)
    check(signed["signer"] == "alice" and signed["signed_at"] != "",
          f"alice signed the standing approval: {signed}")
    row = runbook_row(alice)
    check(row["standing_approver"] == "alice" and row["standing_signed_at"] == signed["signed_at"],
          f"the list shows the signer and the time: {row['standing_approver']} "
          f"{row['standing_signed_at']}")
    known = {item["id"] for item in alice.api("GET", "/work-items")}
    alice.api("POST", f"/runbooks/{RUNBOOK}", {"env": "local"})

    def new_run():
        return [item for item in alice.api("GET", "/work-items")
                if item["id"] not in known and item["kind"] == f"runbook:{RUNBOOK}"]

    eventually("the run of the runbook starts", lambda: len(new_run()) == 1, limit=60)
    item = new_run()[0]["id"]
    eventually(f"{item} ends", lambda: alice.api("GET", f"/work-items/{item}")["work_item"][
        "state"] in {"Done", "Failed"})
    detail = alice.api("GET", f"/work-items/{item}")
    check(detail["work_item"]["state"] == "Done",
          f"{item} ended in {detail['work_item']['state']}")
    # The grant of a runbook names the standing approval that covers it.
    approvers = [grant["approvers"] for grant in detail["grants"]]
    check(approvers == [["standing:alice"]],
          f"the grant of the run names the standing approval of alice: {approvers}")
    check("alice signed" in detail["work_item"]["summary"],
          f"the work item names the signature: {detail['work_item']['summary']}")
    return item


def audit(alice, before):
    events = alice.api("GET", "/events?type_prefix=standing_approval.&limit=1&order=desc")
    check(len(events) == 1 and events[0]["type"] == "standing_approval.signed"
          and events[0]["actor"] == "alice", f"the last signature is by alice: {events}")
    if before != "":
        revoked = alice.api("GET", "/events?types=standing_approval.revoked&limit=1")
        check(len(revoked) == 1 and revoked[0]["actor"] == "alice"
              and revoked[0]["data"]["signer"] == before,
              f"the revocation names alice and the signer before: {revoked}")
    check(alice.api("GET", "/audit/verify").get("ok") is True, "the audit chain verifies")


def run():
    alice = Client().sign_in("alice@example.com")
    dave = Client().sign_in("dave@example.com")
    check(alice.api("GET", "/me")["role"] == "approver", "alice is an approver")
    check(alice.api("GET", "/auth")["sign_in"] is True, "people sign in")
    before = refusals(alice, dave)
    item = sign_and_run(alice)
    print(f"{item} ran {RUNBOOK} under the standing approval of alice", flush=True)
    audit(alice, before)


def main():
    work = pathlib.Path(sys.argv[1]).resolve()
    work.mkdir(parents=True, exist_ok=True)
    subprocess.run(["go", "build", "-o", str(work / "fakeidp"), "./scripts/verify/fakeidp"],
                   check=True)
    provider = subprocess.Popen([str(work / "fakeidp"), "--addr", "127.0.0.1:7190"])
    start = LOG.stat().st_size if LOG.exists() else 0
    try:
        start_control_plane(["--demo=false"], SIGN_IN)
        run()
        with LOG.open() as log:
            log.seek(start)
            errors = [line for line in log if "level=ERROR" in line]
        check(not errors, f"no error lines in the log of the control plane: {errors[:3]}")
    finally:
        provider.kill()
        start_control_plane([], {})
    print("RESULT:", "FAILED: " + "; ".join(failures) if failures else
          "an approver signed a standing approval through the API and the runbook ran under it")
    sys.exit(1 if failures else 0)


main()
