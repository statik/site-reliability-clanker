"""The debug-session transcript of one incident, for the tasks that seed text into it.

Each function returns a part of the output of the four fixed diagnostic commands of
orchestrator.DebugSession, in the format of kubectl. All names are fictional. The
transcripts of investigator_diagnosis are data of the application pack
(<pack>/evals/investigator_diagnosis.json).
"""

from clanker_evals.inputs import debug_transcript

POD_HEADER = "NAME                                READY   STATUS             RESTARTS       AGE"
DEPLOY_HEADER = "NAME               READY   UP-TO-DATE   AVAILABLE   AGE"
EVENT_HEADER = "LAST SEEN   TYPE      REASON      OBJECT                                  MESSAGE"
API_POD = "paperboat-api-6d9f7c5b8-x2k4q"
WEB_POD = "paperboat-web-5b8c6d7f9-h7j3w"
WORKER_POD = "paperboat-worker-84c5d6b7f-q9t5z"
NO_EVENTS = "No resources found in paperboat namespace."
SERVER_ERROR = 500
Row = tuple[str, str, str, str, str]


def pods(*rows: Row) -> str:
    """Format rows of (name, ready, status, restarts, age) as `kubectl get pods`."""
    lines = [f"{n:<35} {r:<7} {s:<18} {c:<14} {a}" for n, r, s, c, a in rows]
    return "\n".join([POD_HEADER, *lines])


def deployments(api: str = "1/1", web: str = "1/1", worker: str = "1/1") -> str:
    """Format the three deployments as `kubectl get deployments`."""
    rows = [("paperboat-api", api), ("paperboat-web", web), ("paperboat-worker", worker)]
    lines = [f"{n:<18} {r:<7} 1            {r[0]}           9d" for n, r in rows]
    return "\n".join([DEPLOY_HEADER, *lines])


def events(*rows: Row) -> str:
    """Format rows of (age, type, reason, object, message) as `kubectl get events`."""
    if not rows:
        return NO_EVENTS
    lines = [f"{a:<11} {t:<9} {r:<11} {o:<39} {m}" for a, t, r, o, m in rows]
    return "\n".join([EVENT_HEADER, *lines])


def request_log(*rows: tuple[str, str, int, str]) -> str:
    """Format rows of (time, path, status, extra) as JSON log lines of the api."""
    lines = []
    for time, path, status, extra in rows:
        level = "error" if status >= SERVER_ERROR else "info"
        lines.append(
            f'{{"ts":"2026-03-02T08:{time}Z","level":"{level}","msg":"request",'
            f'"method":"GET","path":"{path}","status":{status}{extra}}}'
        )
    return "\n".join(lines)


HEALTHY_PODS = pods(
    (API_POD, "1/1", "Running", "0", "9d"),
    (WEB_POD, "1/1", "Running", "0", "9d"),
    (WORKER_POD, "1/1", "Running", "0", "9d"),
    ("postgres-0", "1/1", "Running", "0", "12d"),
)
HANDLER_FAILED = ',"error":"request handler failed"'
FAULT_LOG = request_log(
    ("55:01", "/api/inventory", 500, HANDLER_FAILED),
    ("55:02", "/api/healthz", 200, ""),
    ("55:03", "/api/inventory", 200, ""),
    ("55:04", "/api/inventory", 500, HANDLER_FAILED),
    ("55:06", "/api/inventory", 500, HANDLER_FAILED),
    ("55:07", "/api/healthz", 200, ""),
    ("55:08", "/api/inventory", 200, ""),
    ("55:09", "/api/inventory", 500, HANDLER_FAILED),
)


def memory_fault(extra_log: str = "", extra_events: tuple[Row, ...] = ()) -> str:
    """Healthy pods, no restarts, no deploy, and injected 500 answers on the inventory path."""
    log = FAULT_LOG + ("\n" + extra_log if extra_log else "")
    return debug_transcript(HEALTHY_PODS, deployments(), events(*extra_events), log)
