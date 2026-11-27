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
| `POST /work-items/{id}/decision` | Body `{"decision": "approve" \| "reject" \| "request_changes", "comment": "", "scheduled_for": ""}`. A work item in the state `Scheduled` accepts only `reject`, which cancels the apply. The field `maintenance_window` of a work item is the start of the maintenance window that it waits for, or `""` |
| `GET /work-items/{id}/plans/{layer}` | Plan JSON for one layer, without state values |
| `GET /jobs/{id}/log` | Log of a plan, apply, runbook or debug job |
| `GET /events` | Audit events. Filters: `from`, `to`, `env`, `work_item`, `types`, `type_prefix`, `q`, `after`, `limit`, `order` |
| `GET /events/stream` | Server-sent events, event name `audit` |
| `GET /audit/verify`, `GET /audit/evidence` | Chain verification; evidence pack for a period |
| `GET /timeline` | Markers, future items and health buckets for `from` to `to` |
| `GET /topology?env=` | Nodes and edges of one environment. With an observation that is not stale and no `as_of`, the node of a component has the `status` `ok`, `degraded`, `down` or `unknown` and the `detail` "1 of 1 ready" |
| `GET /posture` | Version posture matrix |
| `GET /health?env=` | Health samples |
| `GET /observation?env=` | The newest observation of one environment (`docs/PACKS.md` section B.6) with two more fields: `age_seconds`, the seconds since the data arrived, and `stale`, true after 60 seconds. A stale record has each state `unknown`. With no data yet, each component is `unknown`, `stale` is true and `age_seconds` is -1 |
| `GET /access` | Identities, active grants, grant history |
| `GET /findings` | Detector findings |
| `GET /notifications`, `POST /notifications/digest` | Channels, routing rules, delivery history; send pending digests |
| `GET /forge/repos`, `/forge/repos/{repo}/tree`, `/blob`, `/log` | Code browsing. `ref` and `path` are query parameters |
| `GET /forge/pulls`, `GET /forge/pulls/{id}`, `POST /forge/pulls` | Pull requests. `POST` body `{"branch", "title", "body", "env"}` opens one for a branch and starts the pipeline |
| `GET /chat`, `POST /chat` | Ask. Body `{"work_item": "", "question": ""}` |
| `GET /agent/calls` | Model calls with model, mode (`openrouter`, `local`, `bedrock` or `replay`), tokens, cost and transcript |
| `GET /runbooks` | The runbooks of the `runbooks` repository: description, tier, inputs, outputs, who signed the standing approval (`standing_approver`, empty when nobody did) and when (`standing_signed_at`, empty for a signature from before this field) |
| `POST /runbooks/{name}` | Run a tier T1 runbook. Body `{"env", "params"}`. The parameters must be exactly the inputs of the definition; a request that does not fit answers 400 with the reason. A runbook with no standing approval answers 409. Approver or admin |
| `POST /runbooks/{name}/standing-approval` | Sign the standing approval of a runbook as the person behind the request. It covers every later run until somebody revokes it, and a new signature replaces the one before. No body. The answer is `{"runbook", "signer", "signed_at"}`. A runbook with no definition, or whose tier is not T1, answers 400 with the reason. Approver or admin; another role gets 403 |
| `DELETE /runbooks/{name}/standing-approval` | Revoke the standing approval of a runbook, also one that another person signed. A run that started goes on. The answer is the approval with an empty `signer` and `signed_at`. A runbook with no standing approval answers 409. Approver or admin |
| `POST /debug-sessions` | Open a recorded, read-only debug session. Body `{"env", "reason"}`. The session runs in the background, and the answer is its work item (kind `debug-session`) at once. The work item ends `Done` or `Failed`; the transcript is the log of its job of kind `debug` (`GET /jobs/{id}/log`), and a failure is the reason of the change to `Failed`. Approver or admin |
| `POST /environments/{env}/blank-slate` | Open the blank-slate work item. Admin |
| `GET /sim`, `POST /sim/advance`, `POST /sim/scenes/{name}` | Simulator: virtual clock and scenes of the scripted month |
| `GET /settings/model` | The language model of the control plane, for every role of a user. `mode` is `openrouter`, `local`, `bedrock` or `replay`. `provider` is the name for a person. `models` is the model of each role (empty in the mode `replay`); in the mode `bedrock` each model is the ID of an inference profile. `base_url` is the endpoint of the mode `local`, without a user, a password or a query. `region` is the AWS region of the mode `bedrock`. `key_set` says whether a key is set; the answer never holds a key or a part of one. `local` is the local inference: `available` (with a `reason` when false), `url`, `running`, `version`, `model`, `model_present`, `model_loaded` and `started_by_src` |
| `POST /settings/model/local/start` | Start the local inference: start the Ollama server when it does not answer, then load the model into memory. No body. The answer is the settings with `done`, a sentence that says what was done. Admin; another role gets 403. A control plane that cannot control a model server (`available` is false), and a server that does not hold the model, answer 409 with the reason |
| `POST /settings/model/local/stop` | Stop the local inference: unload the model, and stop the Ollama server only when this control plane started it. `done` says which of the two was done. No body. Admin; the same refusals as the start |

Git over HTTP is at `/git/<repository>.git`.

A collector sends its metrics to `POST /otlp/v1/metrics`. This path is not below `/api/v1`
and needs no signed-in user. The body is an OTLP request for metrics in JSON, at most
2 MiB, with no compression. The header `Authorization: Bearer <token>` carries the
collector token of one environment, and the core keeps the observation of that
environment. The answer is `{}` with the status 200. The errors: 401 with no valid token,
413 for a body over the limit, 400 for a body that is not such a request, and 503 while
the targets of the environment are not known, which is before the first apply of the layer
`40-platform`.

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
| Approvals | `approval.requested`, `approval.recorded`, `standing_approval.signed`, `standing_approval.revoked` |
| Access | `grant.issued`, `grant.debug_issued`, `grant.revoked`, `grant.expired`, `grant.revoke_failed` |
| Applies | `apply.started`, `apply.succeeded`, `apply.failed`, `apply.refused`, `apply.error` |
| Verification | `verify.passed`, `verify.failed`, `rollback.authorized`, `rollback.failed`, `rollback.refused` |
| Runbooks | `runbook.succeeded`, `runbook.failed` |
| Detectors | `finding.opened`, `detector.failed` |
| Incidents | `incident.opened`, `incident.diagnosed`, `incident.escalated`, `incident.resolved` |
| Agent | `agent.drafted`, `agent.explained`, `agent.answered`, `agent.failed`, `agent.budget_reached` |
| Notifications | `notify.sent`, `notify.failed`, `notify.skipped` |
| Settings | `model.local_started`, `model.local_stopped` |
| Simulator | `sim.scene_started`, `sim.clock_advanced`, `sim.release_published`, `sim.advisory_published`, `sim.manual_change`, `sim.fault_injected`, `sim.images_published` |
