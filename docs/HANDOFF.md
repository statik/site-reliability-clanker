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
| Live model | OpenRouter with `claude-haiku-5.5` and `claude-sonnet-5.5`. Every run used the live model |
| Dashboard | Carbon dashboard served by the control plane at http://127.0.0.1:7070. Build, lint, types and 30 tests pass |
| Evals | Inspect AI suite in `evals/`. Three live rounds; see `evals/RESULTS.md` |
| Website and video | `site/`. The video renders from one command with real dashboard footage |
| iOS app | `ios/`. Builds, 71 tests pass, runs in the simulator against the control plane |
| Fresh clone | A fresh clone built and ran the blank-slate deployment and the scripted month |

## The setup wizard (added 2026-11-17)

- `clanker setup` serves a wizard on `127.0.0.1:7069`. `make release` builds self-contained
  binaries (tag `bundle`) for macOS and Linux in `dist/`.
- **Ran:** the macOS binary, started outside a checkout, walked through the local target in
  a real browser and started the control plane (`web/scripts/setup-wizard-check.mjs`), on
  the first-run path and on the restart path. It used the data directory of the running
  demo, so a first deployment from an empty machine through the wizard was not run.
- The wizard offers only the local target. The AWS target is switched off
  (`setup.Config.EnableAWS`).
- A binary that a browser downloads is unsigned, so macOS can refuse to start it. The
  `curl` command in the README avoids this.

## Direction for the cloud control plane (decided 2026-11-17)

The owner changed the home of the control plane on AWS twice on this day: first a Linux
virtual machine, then a container on Amazon ECS, then a Nanos unikernel in an auto scaling
group. Section 12 of the plan records the five decisions that follow from the last one.

- **What exists:** the code in `internal/setup` for the AWS CLI sign-in, the CloudFormation
  change-set review, the key in Parameter Store, and a template and tunnel for the ECS
  design. It passes its tests against a stand-in for the AWS CLI. It never ran in an AWS
  account. The ECS template is the starting point for the runner jobs.
- **What the unikernel needs, in build order:** policy on the OPA library (done); the
  database outside the process (done: PostgreSQL); the forge on a git library with its
  objects in the database; the split of core and runners; a lease, so that two instances
  never write; OIDC sign-in; the image build and the stack in the wizard; a run in a real
  account.
- **Open risks:** the forge needs its own three-way merge, because the git library merges
  only by fast-forward. The control plane still keeps plans, logs, OpenTofu state and its
  signing key as files in the data directory; these must move before the unikernel can
  hold no state.
- On a cloud, `clanker up` starts in a standby mode: it serves the dashboard and proposes
  no deployment.

## What is validated only

| Item | State |
|---|---|
| AWS, Google Cloud, Azure stacks | 18 root modules pass `tofu validate`. Nothing was planned or applied. No cloud account was used |
| Nix flake | Written. Not evaluated, because Nix is not installed on the build machine |

## What exists but was not run

- Replay mode in the control plane. The recordings pass the eval scorers with the mock model.
  No scripted month ran with `CLANKER_AGENT_MODE=replay`.
- An approval from the dashboard or from the iOS app against the real server. All approvals
  in the runs came through the API. The approval buttons were tested against a refusal only.
- CI for the dashboard, the evals, the site and the iOS app. The branch `integration` is
  not on GitHub yet.

## What is not built

- The access broker paths for the three clouds (STS, service account impersonation, Entra).
- Swamp integration and the Nanos unikernel track. Both spikes need input from the owner.
- Use cases 7, 8, 12 and 23 on a real target. Use case 7 is a forecast on the timeline.
- Real authentication. The dashboard and the iOS app send a user name in a header.
- A "defer" decision, and enforcement of a maintenance window for tier T3.
- Quiet hours, escalation and threading for notifications.
- The move of the control plane into a cloud (bootstrap and pivot).

## Decisions made in this build

| Topic | Decision | Issue |
|---|---|---|
| Workload | Paperboat is a fish rental app: inventory, aging, delivery, customer support | #17 |
| Forge | Built into the control plane | #6 |
| Control-plane database | PostgreSQL, outside the process (changed 2026-11-17) | #7 |
| First deployment | One approval. Later layers must stay inside a create-only envelope | #8 |
| Inference | OpenRouter, confirmed by the owner on 2026-11-16 | #15 |
| Merges | Merge a pull request when its checks pass and the change was run. See `CLAUDE.md` | — |
| Dashboard, evals, site and video, iOS app | Review issues | #22, #23, #24, #25, #26 |

## How to continue

1. Read the open issues with the label `needs-owner-review`. Each one states the default, what
   was run, what was not, and the question for the owner.
2. Do not apply anything in a cloud account before the owner approves the cost (issue #10).
3. To run the demo again from nothing: `make destroy`, `make clean-data`, `make demo`.
4. To try the scripted month without a model: set `CLANKER_AGENT_MODE=replay`. This mode
   was not run end to end.

## Things that cost time, so nobody repeats them

| Problem | Cause and fix |
|---|---|
| `tofu init` hangs for minutes | Several large provider downloads at once. Set `TF_PLUGIN_CACHE_DIR`. The runner does this |
| A cloud root fails `tofu init` in CI | The lock file has hashes for one platform. Run `tofu providers lock` for `linux_amd64`, `linux_arm64` and `darwin_arm64` |
| kind cluster cannot pull Paperboat images | The registry of the local target is created in layer `10-network`. The demo pushes the images after that layer |
| A tfvars change to the Postgres version replaced a helper job | The job used the Postgres image. It now uses a fixed `busybox` image |
| The Investigator blamed an old release | It had no change history. The prompt now includes it |
| An incident opened again every 20 seconds | The finding key used the sample time. It now uses the environment and the hour |
| The Investigator saw no errors in a failing system | The debug transcript was cut from the front, and the application log is the last command. Each command now keeps its newest output |
| A log line closed the `<data>` frame of the Investigator | The evals found it. Untrusted input is now JSON-encoded |
| `git push` over SSH fails with "agent refused operation" | The SSH key is in Secretive and needs the owner present. Do not work around it |
| `xcodebuild test` hangs | The first-launch packages of Xcode are probably not installed. `ios/build.sh` runs the test bundle with `xcrun simctl spawn` |
