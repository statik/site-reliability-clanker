"""Live check of approval in Slack on the local target, with a stand-in for Slack.

Run it from the root of the checkout after scripts/verify/fresh-run.sh:

    python3 scripts/verify/chat_check.py <empty work directory>

It restarts the control plane with the sign-in (the fake identity provider) and the Slack
app, whose Web API address is a listener in this process. It pushes a hand-written tier T2
change, reads the request for approval that the app posts, and clicks as a person:
with no signature, with no linked account, and after the link through the link page. At
the end it restarts the control plane as the demo. No request goes to the real Slack.
"""
import hashlib
import hmac
import http.cookiejar
import http.server
import json
import os
import pathlib
import re
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:7070"
IDP = "http://127.0.0.1:7190"
SLACK_PORT = 7191
SLACK = f"http://127.0.0.1:{SLACK_PORT}"
SECRET = "chat-check-signing-secret"
VARS = "platform/stacks/local/50-app/terraform.tfvars"
SIGN_IN = {
    "CLANKER_OIDC_ISSUER": IDP, "CLANKER_OIDC_CLIENT_ID": "clanker",
    "CLANKER_OIDC_CLIENT_SECRET": "check", "CLANKER_PUBLIC_URL": BASE,
    "CLANKER_ADMIN_EMAIL": "carol@example.com"}
CHAT = {
    "CLANKER_SLACK_SIGNING_SECRET": SECRET, "CLANKER_SLACK_BOT_TOKEN": "xoxb-chat-check",
    "CLANKER_SLACK_CHANNEL": "C0CHECK", "CLANKER_SLACK_API_URL": SLACK + "/api"}

received = []
failures = []


class StandIn(http.server.BaseHTTPRequestHandler):
    """Answers the two Web API methods of the app and takes the private answers."""

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        received.append({"path": self.path, "body": json.loads(raw),
                         "token": self.headers.get("Authorization", "")})
        answer = json.dumps({"ok": True, "channel": "C0CHECK", "ts": f"{len(received)}.0001"})
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(answer.encode())

    def log_message(self, *_):
        return


def to(path):
    return [call for call in list(received) if call["path"] == path]


def check(ok, text):
    print(("ok      " if ok else "FAILED  ") + text, flush=True)
    if not ok:
        failures.append(text)


def eventually(what, condition, limit=420):
    deadline = time.time() + limit
    while time.time() < deadline:
        if condition():
            return
        time.sleep(2)
    raise SystemExit(f"FAILED: {what} did not happen in {limit} s")


class Client:
    """A browser with its own cookies, or a program with a token."""

    def __init__(self, token=""):
        self.token = token
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def send(self, method, address, form=None, body=None, headers=None):
        headers = dict(headers or {})
        data = None
        if form is not None:
            data = urllib.parse.urlencode(form).encode()
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        request = urllib.request.Request(address, method=method, data=data, headers=headers)
        try:
            with self.opener.open(request, timeout=120) as response:
                return response.status, response.read().decode()
        except urllib.error.HTTPError as error:
            return error.code, error.read().decode()

    def api(self, method, path, body=None):
        status, text = self.send(method, BASE + "/api/v1" + path, body=body)
        if status >= 300:
            raise SystemExit(f"FAILED: {method} {path} answered {status}: {text}")
        return json.loads(text)

    def sign_in(self, email):
        self.send("POST", f"{IDP}/person?email={email}&verified=true", form={})
        status, _ = self.send("GET", BASE + "/auth/login")
        if status != 200:
            raise SystemExit(f"FAILED: the sign-in of {email} answered {status}")
        return self


def start_control_plane(extra):
    subprocess.run(["pkill", "-f", "clanker up"], check=False)
    time.sleep(2)
    log = open(".clanker/up.log", "a")
    subprocess.Popen(["./bin/clanker", "up"], stdout=log, stderr=log, start_new_session=True,
                     env={**os.environ, **extra})
    eventually("the control plane answers", lambda: Client().send("GET", BASE + "/healthz")[0] == 200,
               limit=90)


def program_token(person):
    """Run the device flow as a program, approved by a signed-in person."""
    program = Client()
    code = json.loads(program.send("POST", BASE + "/auth/device/code", form={})[1])
    status, _ = person.send("POST", BASE + "/auth/device",
                            form={"user_code": code["user_code"], "decision": "approve"})
    if status != 200:
        raise SystemExit(f"FAILED: the approval of the device code answered {status}")
    answer = program.send("POST", BASE + "/auth/device/token",
                          form={"device_code": code["device_code"]})[1]
    return json.loads(answer)["access_token"]


def push_change(work, token):
    """Push a branch that changes the number of api replicas, and return the branch."""
    def git(*args, cwd):
        subprocess.run(["git", "-c", "user.name=carol", "-c", "user.email=carol@example.com",
                        "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c",
                        f"http.extraHeader=Authorization: Bearer {token}", *args],
                       cwd=cwd, check=True, capture_output=True)

    git("clone", "-q", BASE + "/git/platform-live.git", str(work), cwd=work.parent)
    git("checkout", "-q", "-b", "human/chat-check", cwd=work)
    path = work / VARS
    text = path.read_text()
    found = re.search(r"api_replicas(\s*)= (\d+)", text)
    replicas = 1 if found.group(2) == "2" else 2
    path.write_text(text.replace(found.group(0), f"api_replicas{found.group(1)}= {replicas}"))
    git("commit", "-q", "-am", f"Run {replicas} api replicas", cwd=work)
    git("push", "-q", "origin", "human/chat-check", cwd=work)
    return "human/chat-check", f"Run {replicas} api replicas"


def buttons(message):
    values = {}
    for block in message.get("blocks", []):
        for element in block.get("elements", []):
            values[element.get("action_id")] = element.get("value")
    return values


def click(person, value, signed=True):
    """Send a click on Approve as Slack sends it, and return the HTTP status."""
    payload = {"type": "block_actions", "user": {"id": "U0" + person.upper(), "username": person},
               "team": {"id": "T0CHECK", "domain": "chat-check"},
               "response_url": SLACK + "/response",
               "actions": [{"type": "button", "action_id": "clanker_approve", "value": value}]}
    body = "payload=" + urllib.parse.quote(json.dumps(payload))
    stamp = str(int(time.time()))
    mac = hmac.new(SECRET.encode(), f"v0:{stamp}:{body}".encode(), hashlib.sha256).hexdigest()
    headers = {"X-Slack-Request-Timestamp": stamp, "Content-Type": "application/x-www-form-urlencoded"}
    if signed:
        headers["X-Slack-Signature"] = "v0=" + mac
    request = urllib.request.Request(BASE + "/chat/slack/interactions", method="POST",
                                     data=body.encode(), headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


def answer_to_click(person, value):
    before = len(to("/response"))
    check(click(person, value) == 200, f"a signed click of {person} answers 200")
    eventually("the private answer", lambda: len(to("/response")) > before, limit=60)
    return to("/response")[before]["body"]["text"]


def run(work):
    carol = Client().sign_in("carol@example.com")
    program = Client(program_token(carol))
    branch, title = push_change(work, program.token)
    item = program.api("POST", "/forge/pulls", {"branch": branch, "title": title,
                                                "body": "Check of approval in chat", "env": "local"})["work_item"]
    print(f"opened {item}: {title}", flush=True)

    def state():
        return program.api("GET", f"/work-items/{item}")["work_item"]

    eventually(f"{item} waits for approval", lambda: state()["state"] == "AwaitingApproval")
    check(state()["tier"] == "T2", f"{item} has tier T2 (it has {state()['tier']})")

    def request():
        return [c for c in to("/api/chat.postMessage")
                if buttons(c["body"]).get("clanker_open") == item]

    eventually("the app posts the request for approval", lambda: len(request()) > 0, limit=60)
    posted = request()[-1]
    value = buttons(posted["body"]).get("clanker_approve", "")
    check(posted["token"] == "Bearer xoxb-chat-check" and posted["body"]["channel"] == "C0CHECK",
          "the request went to the channel with the bot token")
    check(value.startswith(item + " ") and "clanker_reject" in buttons(posted["body"]),
          "the request has Approve and Reject with the work item and a binding")

    check(click("alice", value, signed=False) == 401, "a click with no signature answers 401")
    text = answer_to_click("alice", value)
    link = re.search(re.escape(BASE) + r"/auth/chat-link\?code=[A-Za-z0-9_-]+", text)
    check("Nothing was decided" in text and link is not None,
          "a click of an account with no link decides nothing and gets a link")
    check(state()["state"] == "AwaitingApproval", f"{item} still waits for approval")

    alice = Client().sign_in("alice@example.com")
    status, page = alice.send("GET", link.group(0))
    check(status == 200 and "U0ALICE" in page and "alice@example.com" in page,
          "the link page shows the chat account and the email address")
    code = urllib.parse.parse_qs(urllib.parse.urlparse(link.group(0)).query)["code"][0]
    status, page = alice.send("POST", BASE + "/auth/chat-link", form={"code": code},
                              headers={"Origin": BASE})
    check(status == 200 and "The account is linked" in page, "the person links the account")
    status, _ = alice.send("POST", BASE + "/auth/chat-link", form={"code": code},
                           headers={"Origin": BASE})
    check(status == 404, "the link code works once")

    text = answer_to_click("alice", item + " " + "0" * 64)
    check("changed after this request" in text, "a click with another binding is refused")
    text = answer_to_click("alice", value)
    check("is recorded: approve" in text, "the linked click is recorded: " + text)
    eventually(f"{item} ends", lambda: state()["state"] in {"Done", "Failed", "RolledBack"})
    check(state()["state"] == "Done", f"{item} ended in {state()['state']}")
    ts = f"{received.index(posted) + 1}.0001"

    def closed():
        return [c for c in to("/api/chat.update")
                if c["body"].get("ts") == ts and "clanker_approve" not in buttons(c["body"])]

    eventually("the app closes the request", lambda: len(closed()) > 0, limit=60)
    check("clanker_open" in buttons(closed()[-1]["body"]),
          "the request in the channel was replaced: no decision button, the link stays")

    events = program.api("GET", f"/events?work_item={item}&order=asc&limit=200")
    events = events["events"] if isinstance(events, dict) else events
    recorded = [e for e in events if e["type"] == "approval.recorded"]
    origin = (recorded[0].get("data") or {}).get("origin", {}) if recorded else {}
    check(len(recorded) == 1 and recorded[0]["actor"] == "alice" and origin.get("platform") == "slack"
          and origin.get("account") == "U0ALICE", f"one approval by alice with the Slack account: {origin}")
    check(program.api("GET", "/audit/verify").get("ok") is True, "the audit chain verifies")


def main():
    work = pathlib.Path(sys.argv[1]).resolve()
    work.mkdir(parents=True, exist_ok=True)
    subprocess.run(["go", "build", "-o", str(work / "fakeidp"), "./scripts/verify/fakeidp"], check=True)
    provider = subprocess.Popen([str(work / "fakeidp"), "--addr", "127.0.0.1:7190"])
    server = http.server.ThreadingHTTPServer(("127.0.0.1", SLACK_PORT), StandIn)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        start_control_plane({**SIGN_IN, **CHAT})
        run(work / "clone")
    finally:
        provider.kill()
        start_control_plane({})
    print("RESULT:", "FAILED: " + "; ".join(failures) if failures else
          "a linked person approved a tier T2 change in the stand-in for Slack")
    sys.exit(1 if failures else 0)


main()
