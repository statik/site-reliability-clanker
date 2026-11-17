# Handoff

State of the work on 2026-11-17. A new session can continue from this file and from the
issues with the label `needs-owner-review`.

## What runs now

Everything in this section was run on the local target (kind on Docker, macOS arm64).

| Capability | Result |
|---|---|
| First start | `make demo` builds and starts the control plane. `clanker preflight` lists missing tools first |
| Blank-slate deployment | One tier T3 work item. Two approvers. Six layers applied one after the other, each under its own grant. Verification passed |
| Application upgrade with migration | Paperboat 1.0.0 to 1.1.0. The agent noticed the release, opened the pull request and explained it |
| Security patch | Advisory to 1.1.1 |
| Failed canary and rollback | 1.2.0 failed the canary check. A revert pull request was planned and applied automatically |
| Postgres minor patch | 17.11 to 17.12 |
| Runbooks under a standing approval | Restart, backup, restore drill, database maintenance |
| Drift | A manual `kubectl` change was found by the drift check and reconciled after approval |
| Incident | Alert, read-only debug session under a debug grant, diagnosis, mitigation or escalation |
| Stale plan | A saved plan that went stale was refused. The branch was updated, planned again, and the old approval did not count |
| Hand-written change | Clone and send a branch over HTTP, open a pull request through the API, same pipeline |
| Notifications | Email (Mailpit) and Slack, Teams, Discord payloads (local webhook sink) for each approval request |
| Audit | Hash-chained log. The chain verified after more than 450 events |
| Live model | OpenRouter with `claude-haiku-5.5` and `claude-sonnet-5.5`. Replay mode works without a key |

## What is validated only

| Item | State |
|---|---|
| AWS, Google Cloud, Azure stacks | 18 root modules pass `tofu validate`. Nothing was planned or applied. No cloud account was used |
| Nix flake | Written. Not evaluated, because Nix is not installed on the build machine |

## What is not built

- The access broker paths for the three clouds (STS, service account impersonation, Entra).
- Swamp integration and the Nanos unikernel track. Both spikes need input from the owner.
- Use cases 7, 8, 12 and 23 on a real target. Use case 7 is a forecast on the timeline.
- Real authentication. The dashboard sends a user name in a header.
- Quiet hours, escalation and threading for notifications.
- The move of the control plane into a cloud (bootstrap and pivot).

## Decisions made in this build

| Topic | Decision | Issue |
|---|---|---|
| Workload | Paperboat is a fish rental app: inventory, aging, delivery, customer support | #17 |
| Forge | Built into the control plane | #6 |
| Control-plane database | Embedded SQLite | #7 |
| First deployment | One approval. Later layers must stay inside a create-only envelope | #8 |
| Inference | OpenRouter, confirmed by the owner on 2026-11-16 | #15 |
| Merges | Merge a pull request when its checks pass and the change was run. See `CLAUDE.md` | — |

## How to continue

1. Read the open issues with the label `needs-owner-review`. Each one states the default, what
   was run, what was not, and the question for the owner.
2. Do not apply anything in a cloud account before the owner approves the cost (issue #10).
3. To run the demo again from nothing: `make destroy`, `make clean-data`, `make demo`.
4. To repeat the scripted month without a model: set `CLANKER_AGENT_MODE=replay`.

## Things that cost time, so nobody repeats them

| Problem | Cause and fix |
|---|---|
| `tofu init` hangs for minutes | Several large provider downloads at once. Set `TF_PLUGIN_CACHE_DIR`. The runner does this |
| A cloud root fails `tofu init` in CI | The lock file has hashes for one platform. Run `tofu providers lock` for `linux_amd64`, `linux_arm64` and `darwin_arm64` |
| kind cluster cannot pull Paperboat images | The registry of the local target is created in layer `10-network`. The demo pushes the images after that layer |
| A tfvars change to the Postgres version replaced a helper job | The job used the Postgres image. It now uses a fixed `busybox` image |
| The Investigator blamed an old release | It had no change history. The prompt now includes it |
| An incident opened again every 20 seconds | The finding key used the sample time. It now uses the environment and the hour |
