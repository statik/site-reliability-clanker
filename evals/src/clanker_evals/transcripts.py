"""Debug-session transcripts for the Investigator: one for each distinct cause.

Each function returns the output of the four fixed diagnostic commands of
orchestrator.DebugSession, in the format of kubectl. All names are fictional.
"""

from clanker_evals.inputs import debug_transcript

POD_HEADER = "NAME                                READY   STATUS             RESTARTS       AGE"
DEPLOY_HEADER = "NAME               READY   UP-TO-DATE   AVAILABLE   AGE"
EVENT_HEADER = "LAST SEEN   TYPE      REASON      OBJECT                                  MESSAGE"
API_POD = "paperboat-api-6d9f7c5b8-x2k4q"
NEW_API_POD = "paperboat-api-7c4d9b6f5-m8n2p"
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


def bad_release() -> str:
    """Release 1.2.0-bad rolled out minutes ago; its pods are healthy but inventory fails."""
    pod_rows = pods(
        (NEW_API_POD, "1/1", "Running", "0", "6m"),
        (WEB_POD, "1/1", "Running", "0", "6m"),
        (WORKER_POD, "1/1", "Running", "0", "6m"),
        ("postgres-0", "1/1", "Running", "0", "12d"),
    )
    event_rows = events(
        (
            "6m",
            "Normal",
            "ScalingUp",
            "deployment/paperboat-api",
            "Scaled up replica set paperboat-api-7c4d9b6f5 to 1",
        ),
        (
            "6m",
            "Normal",
            "Pulled",
            f"pod/{NEW_API_POD}",
            'Container image "registry.local:5001/paperboat:1.2.0-bad" already present on machine',
        ),
        (
            "5m",
            "Normal",
            "ScalingDown",
            "deployment/paperboat-api",
            "Scaled down replica set paperboat-api-6d9f7c5b8 to 0",
        ),
    )
    snapshot = ',"error":"inventory: build tank snapshot: index out of range"'
    log = request_log(
        ("55:01", "/api/inventory", 500, snapshot),
        ("55:02", "/api/healthz", 200, ""),
        ("55:03", "/api/inventory", 200, ""),
        ("55:04", "/api/inventory", 500, snapshot),
        ("55:05", "/api/rentals", 200, ""),
        ("55:06", "/api/inventory", 500, snapshot),
        ("55:07", "/api/healthz", 200, ""),
    )
    return debug_transcript(pod_rows, deployments(), event_rows, log)


def crash_loop_after_deploy() -> str:
    """A new replica set of release 1.2.0-bad panics at start; the old pod still serves."""
    pod_rows = pods(
        (API_POD, "1/1", "Running", "0", "9d"),
        (NEW_API_POD, "0/1", "CrashLoopBackOff", "6 (40s ago)", "7m"),
        (WEB_POD, "1/1", "Running", "0", "9d"),
        (WORKER_POD, "1/1", "Running", "0", "9d"),
        ("postgres-0", "1/1", "Running", "0", "12d"),
    )
    event_rows = events(
        (
            "7m",
            "Normal",
            "ScalingUp",
            "deployment/paperboat-api",
            "Scaled up replica set paperboat-api-7c4d9b6f5 to 1",
        ),
        (
            "7m",
            "Normal",
            "Pulled",
            f"pod/{NEW_API_POD}",
            'Container image "registry.local:5001/paperboat:1.2.0-bad" already present on machine',
        ),
        (
            "40s",
            "Warning",
            "BackOff",
            f"pod/{NEW_API_POD}",
            "Back-off restarting failed container api",
        ),
    )
    log = (
        "Found 2 pods, using pod/" + NEW_API_POD + "\n"
        '{"ts":"2026-03-02T08:54:11Z","level":"info","msg":"starting","version":"1.2.0-bad"}\n'
        "panic: runtime error: invalid memory address or nil pointer dereference\n"
        "goroutine 1 [running]:\n"
        "main.buildTankSnapshots(...)\n"
        "\t/src/internal/inventory/snapshot.go:41 +0x1c\n"
        "main.main()\n\t/src/main.go:88 +0x2e4"
    )
    return debug_transcript(pod_rows, deployments(api="1/1"), event_rows, log)


def database_unreachable() -> str:
    """Postgres is down after its volume failed to attach; the api cannot connect."""
    pod_rows = pods(
        (API_POD, "0/1", "Running", "0", "9d"),
        (WEB_POD, "1/1", "Running", "0", "9d"),
        (WORKER_POD, "0/1", "Running", "0", "9d"),
        ("postgres-0", "0/1", "ContainerCreating", "0", "6m"),
    )
    event_rows = events(
        ("6m", "Normal", "Killing", "pod/postgres-0", "Stopping container postgres"),
        (
            "5m",
            "Warning",
            "FailedMount",
            "pod/postgres-0",
            'MountVolume.SetUp failed for volume "postgres-data": mount failed: exit status 32',
        ),
        (
            "4m",
            "Warning",
            "Unhealthy",
            f"pod/{API_POD}",
            "Readiness probe failed: HTTP probe failed with statuscode: 503",
        ),
    )
    refused = ',"error":"dial tcp 10.96.14.20:5432: connect: connection refused"'
    log = request_log(
        ("55:01", "/api/inventory", 500, refused),
        ("55:02", "/api/healthz", 503, refused),
        ("55:04", "/api/inventory", 500, refused),
        ("55:07", "/api/healthz", 503, refused),
    )
    return debug_transcript(pod_rows, deployments(api="0/1", worker="0/1"), event_rows, log)


def image_pull_failure() -> str:
    """The new api pod cannot pull its image; the old replica set was already scaled down."""
    pod_rows = pods(
        (NEW_API_POD, "0/1", "ImagePullBackOff", "0", "8m"),
        (WEB_POD, "1/1", "Running", "0", "9d"),
        (WORKER_POD, "1/1", "Running", "0", "9d"),
        ("postgres-0", "1/1", "Running", "0", "12d"),
    )
    event_rows = events(
        (
            "8m",
            "Normal",
            "ScalingUp",
            "deployment/paperboat-api",
            "Scaled up replica set paperboat-api-7c4d9b6f5 to 1",
        ),
        (
            "7m",
            "Warning",
            "Failed",
            f"pod/{NEW_API_POD}",
            'Failed to pull image "registry.local:5001/paperboat:1.1.2": manifest unknown',
        ),
        ("7m", "Warning", "Failed", f"pod/{NEW_API_POD}", "Error: ErrImagePull"),
        (
            "90s",
            "Normal",
            "BackOff",
            f"pod/{NEW_API_POD}",
            'Back-off pulling image "registry.local:5001/paperboat:1.1.2"',
        ),
    )
    log = (
        f'Error from server (BadRequest): container "api" in pod "{NEW_API_POD}" is waiting '
        "to start: trying and failing to pull image\n(command failed: exit status 1)"
    )
    return debug_transcript(pod_rows, deployments(api="0/1"), event_rows, log)


def healthy_system() -> str:
    """Everything is healthy and every request succeeds: the alert has no visible cause."""
    log = request_log(
        ("55:01", "/api/inventory", 200, ""),
        ("55:02", "/api/healthz", 200, ""),
        ("55:03", "/api/inventory", 200, ""),
        ("55:05", "/api/rentals", 200, ""),
        ("55:07", "/api/healthz", 200, ""),
        ("55:08", "/api/inventory", 200, ""),
    )
    return debug_transcript(HEALTHY_PODS, deployments(), events(), log)


def oom_kills() -> str:
    """The api container is killed for memory again and again."""
    pod_rows = pods(
        (API_POD, "0/1", "OOMKilled", "14 (55s ago)", "9d"),
        (WEB_POD, "1/1", "Running", "0", "9d"),
        (WORKER_POD, "1/1", "Running", "0", "9d"),
        ("postgres-0", "1/1", "Running", "0", "12d"),
    )
    event_rows = events(
        (
            "3m",
            "Warning",
            "OOMKilling",
            "node/local-control-plane",
            "Memory cgroup out of memory: Killed process 48211 (paperboat) total-vm:412004kB",
        ),
        ("55s", "Warning", "BackOff", f"pod/{API_POD}", "Back-off restarting failed container api"),
    )
    log = (
        '{"ts":"2026-03-02T08:54:01Z","level":"info","msg":"starting","version":"1.1.1"}\n'
        '{"ts":"2026-03-02T08:54:20Z","level":"info","msg":"inventory cache warm","fish":60}\n'
        '{"ts":"2026-03-02T08:54:58Z","level":"warn","msg":"heap in use","mb":251,'
        '"limit_mb":256}'
    )
    return debug_transcript(pod_rows, deployments(api="0/1"), event_rows, log)


def volume_full() -> str:
    """The shared volume is full; writes fail and a restart cannot free space."""
    full = ',"error":"write /data/manifests/2026-03-02.json: no space left on device"'
    log = request_log(
        ("55:01", "/api/deliveries", 500, full),
        ("55:02", "/api/healthz", 200, ""),
        ("55:03", "/api/inventory", 500, full),
        ("55:05", "/api/inventory", 500, full),
        ("55:07", "/api/healthz", 200, ""),
    )
    event_rows = events(
        (
            "12m",
            "Warning",
            "FreeDiskSpaceFailed",
            "node/local-control-plane",
            "shared volume paperboat-shared is 100% used",
        ),
    )
    return debug_transcript(HEALTHY_PODS, deployments(), event_rows, log)


def unschedulable() -> str:
    """After a node drain no node has room: the api pod stays Pending."""
    pod_rows = pods(
        (NEW_API_POD, "0/1", "Pending", "0", "11m"),
        (WEB_POD, "1/1", "Running", "0", "9d"),
        (WORKER_POD, "1/1", "Running", "0", "9d"),
        ("postgres-0", "1/1", "Running", "0", "12d"),
    )
    event_rows = events(
        (
            "11m",
            "Warning",
            "FailedScheduling",
            f"pod/{NEW_API_POD}",
            "0/1 nodes are available: 1 Insufficient cpu. preemption: 0/1 nodes are available",
        ),
    )
    log = (
        'Error from server (BadRequest): container "api" in pod "'
        + NEW_API_POD
        + ('" is waiting to start: ContainerCreating\n(command failed: exit status 1)')
    )
    return debug_transcript(pod_rows, deployments(api="0/1"), event_rows, log)


def web_not_ready() -> str:
    """The web pod fails its readiness probe; the api answers every request."""
    pod_rows = pods(
        (API_POD, "1/1", "Running", "0", "9d"),
        (WEB_POD, "0/1", "Running", "0", "9d"),
        (WORKER_POD, "1/1", "Running", "0", "9d"),
        ("postgres-0", "1/1", "Running", "0", "12d"),
    )
    event_rows = events(
        (
            "2m",
            "Warning",
            "Unhealthy",
            f"pod/{WEB_POD}",
            'Readiness probe failed: Get "http://10.244.0.17:8080/": context deadline exceeded',
        ),
    )
    log = request_log(
        ("55:01", "/api/inventory", 200, ""),
        ("55:02", "/api/healthz", 200, ""),
        ("55:04", "/api/inventory", 200, ""),
        ("55:07", "/api/healthz", 200, ""),
    )
    return debug_transcript(pod_rows, deployments(web="0/1"), event_rows, log)


def session_failed() -> str:
    """The debug session could not reach the cluster: there is no evidence."""
    failure = (
        "Unable to connect to the server: dial tcp 127.0.0.1:6443: connect: connection "
        "refused\n(command failed: exit status 1)"
    )
    return debug_transcript(failure, failure, failure, failure)
