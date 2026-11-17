"""Call every read route of the control plane and report each answer that is not 2xx."""
import json
import urllib.error
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:7070/api/v1"
bad = []


def call(method, path, body=None):
    request = urllib.request.Request(
        BASE + path, method=method,
        headers={"X-Clanker-User": "carol", "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            raw = response.read()
            status = response.status
    except urllib.error.HTTPError as error:
        raw, status = error.read(), error.code
    if status >= 300:
        bad.append(f"{method} {path}: {status} {raw[:200]!r}")
    try:
        return json.loads(raw)
    except ValueError:
        return raw


def size(value):
    if isinstance(value, list):
        return len(value)
    if isinstance(value, dict):
        for key in ("events", "items", "calls", "messages", "deliveries", "findings", "grants"):
            if isinstance(value.get(key), list):
                return len(value[key])
    return "-"


items = call("GET", "/work-items")
first = items[0]["id"]
print("work items:", len(items))
for item in items:
    detail = call("GET", f"/work-items/{item['id']}")
    for plan in detail.get("plans") or []:
        call("GET", f"/work-items/{item['id']}/plans/{plan['layer']}")
    for job in detail.get("jobs") or []:
        call("GET", f"/jobs/{job['id']}/log")

as_of = urllib.parse.quote(items[len(items) // 2]["updated_at"])
routes = ["/me", "/users", "/overview", f"/overview?as_of={as_of}", "/events", "/audit/verify",
          "/audit/evidence", f"/audit/evidence?work_item={first}", "/timeline", "/topology",
          f"/topology?as_of={as_of}", "/posture", f"/posture?as_of={as_of}", "/health", "/access",
          "/findings", "/notifications", "/forge/repos", "/forge/pulls", "/chat",
          f"/chat?work_item={first}", "/agent/calls", f"/agent/calls?work_item={first}", "/sim"]
for route in routes:
    print(f"{route}: {size(call('GET', route))}")

for repo in call("GET", "/forge/repos"):
    name = repo["name"] if isinstance(repo, dict) else repo
    call("GET", f"/forge/repos/{name}/tree")
    call("GET", f"/forge/repos/{name}/log")
for pull in call("GET", "/forge/pulls"):
    call("GET", f"/forge/pulls/{pull['id']}")
    call("GET", f"/forge/pulls?repo={pull['repo']}")

# The filters of the event list, which the audit package builds by hand.
events = call("GET", "/events")
events = events["events"] if isinstance(events, dict) else events
print("events:", len(events))
for query in ["search=PAPERBOAT", "search=paperboat", "type=grant.issued", "types=grant.issued,grant.revoked",
              "type_prefix=grant.", f"work_item={first}", "env=local", "after=100", "limit=5&order=desc",
              f"search=Paperboat&type_prefix=work_item.&work_item={first}&env=local"]:
    answer = call("GET", "/events?" + query)
    answer = answer["events"] if isinstance(answer, dict) else answer
    print(f"events?{query}: {len(answer) if isinstance(answer, list) else answer}")

answer = call("POST", "/chat", {"work_item": first, "question": "Who approved this change?"})
print("ask:", str(answer)[:160].replace("\n", " "))
print("chat after ask:", size(call("GET", f"/chat?work_item={first}")))
call("POST", "/me/seen", {})

print(f"\n{len(bad)} answers were not 2xx")
for line in bad:
    print(" ", line)
