# Control-plane API and event schema

The dashboard uses only this API. All paths start with `/api/v1`. All bodies are JSON. An
error has the form `{"error": "message"}` with a 4xx or 5xx status.

The prototype has no authentication. The header `X-Clanker-User` names the user. The server
enforces the role of that user for decisions, runbooks and debug sessions.

Read endpoints that show state accept `as_of=<timestamp>`. They then answer from the audit
log and the snapshots as of that time.

## Endpoints

| Method and path | Purpose |
|---|---|
| `GET /me`, `POST /me/seen`, `GET /users` | Current user; mark the visit for the catch-up panel; demo users |
| `GET /overview` | Environments with health and versions, counts, waiting items, catch-up, chain status |
| `GET /work-items`, `GET /work-items/{id}` | List; detail with plans, approvals, jobs, grants, pull request, diff and events |
| `POST /work-items/{id}/decision` | Body `{"decision": "approve" \| "reject" \| "request_changes", "comment": "", "scheduled_for": ""}` |
| `GET /work-items/{id}/plans/{layer}` | Plan JSON for one layer, without state values |
| `GET /jobs/{id}/log` | Log of a plan, apply, runbook or debug job |
| `GET /events` | Audit events. Filters: `from`, `to`, `env`, `work_item`, `types`, `type_prefix`, `q`, `after`, `limit`, `order` |
| `GET /events/stream` | Server-sent events, event name `audit` |
| `GET /audit/verify`, `GET /audit/evidence` | Chain verification; evidence pack for a period |
| `GET /timeline` | Markers, future items and health buckets for `from` to `to` |
| `GET /topology?env=` | Nodes and edges of one environment |
| `GET /posture` | Version posture matrix |
| `GET /health?env=` | Health samples |
| `GET /access` | Identities, active grants, grant history |
| `GET /findings` | Detector findings |
| `GET /notifications`, `POST /notifications/digest` | Channels, routing rules, delivery history; send pending digests |
| `GET /forge/repos`, `/forge/repos/{repo}/tree`, `/blob`, `/log` | Code browsing. `ref` and `path` are query parameters |
| `GET /forge/pulls`, `GET /forge/pulls/{id}`, `POST /forge/pulls` | Pull requests. `POST` body `{"branch", "title", "body", "env"}` opens one for a branch and starts the pipeline |
| `GET /chat`, `POST /chat` | Ask. Body `{"work_item": "", "question": ""}` |
| `GET /agent/calls` | Model calls with model, tokens, cost and transcript |
| `GET /runbooks` | The runbooks of the `runbooks` repository: description, tier, inputs, outputs and who signed the standing approval |
| `POST /runbooks/{name}` | Run a tier T1 runbook. Body `{"env", "params"}`. The parameters must be exactly the inputs of the definition; a request that does not fit answers 400 with the reason. Approver or admin |
| `POST /debug-sessions` | Open a recorded, read-only debug session. Approver or admin |
| `POST /environments/{env}/blank-slate` | Open the blank-slate work item. Admin |
| `GET /sim`, `POST /sim/advance`, `POST /sim/scenes/{name}` | Simulator: virtual clock and scenes of the scripted month |

Git over HTTP is at `/git/<repository>.git`.

## Work item states

`Detected`, `Drafting`, `Planned`, `AwaitingApproval`, `ChangesRequested`, `Approved`,
`Scheduled`, `Applying`, `Verifying`, `Done`, `Failed`, `RolledBack`, `Rejected`.

## Audit event

```json
{
  "seq": 82, "ts": "2026-11-18T05:42:14.781Z", "wall_ts": "2026-11-17T03:42:20.101Z",
  "type": "apply.succeeded", "actor": "runner", "env": "local", "layer": "50-app",
  "work_item": "WI-0002", "summary": "Applied 50-app",
  "data": {"job": 9, "grant": "G-e66ca7056c8f", "plan_sha256": "…", "commit": "…"},
  "prev_hash": "…", "hash": "…"
}
```

`ts` is virtual time. `wall_ts` is real time. `hash` covers every field and `prev_hash`.

## Event types

| Group | Types |
|---|---|
| Work items | `workitem.created`, `workitem.state_changed` |
| Pull requests | `pr.opened`, `pr.updated`, `pr.merged` |
| Plans and policy | `plan.created`, `plan.failed`, `plan.within_envelope`, `policy.evaluated` |
| Approvals | `approval.requested`, `approval.recorded`, `standing_approval.signed` |
| Access | `grant.issued`, `grant.debug_issued`, `grant.revoked`, `grant.expired`, `grant.revoke_failed` |
| Applies | `apply.started`, `apply.succeeded`, `apply.failed`, `apply.refused`, `apply.error` |
| Verification | `verify.passed`, `verify.failed`, `rollback.authorized`, `rollback.failed` |
| Runbooks | `runbook.succeeded`, `runbook.failed` |
| Detectors | `finding.opened`, `detector.failed` |
| Incidents | `incident.opened`, `incident.diagnosed`, `incident.escalated`, `incident.resolved` |
| Agent | `agent.drafted`, `agent.explained`, `agent.answered`, `agent.failed`, `agent.budget_reached` |
| Notifications | `notify.sent`, `notify.failed`, `notify.skipped` |
| Simulator | `sim.scene_started`, `sim.clock_advanced`, `sim.release_published`, `sim.advisory_published`, `sim.manual_change`, `sim.fault_injected`, `sim.images_published` |
