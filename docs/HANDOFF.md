# Handoff

State of the work on 2026-11-17. A new session can continue from this file and from the
issues with the label `needs-owner-review`.

## Start here (written at the end of the session of 2026-11-17)

### The goal now

The control plane must run on AWS as a Nanos unikernel in an auto scaling group, set up by
the wizard (`clanker setup`). The decisions of the owner are in section 12 of
`docs/PLAN.md` and in issue #11. In short: the core starts no program and keeps no file; jobs
run in short-lived runners; all state is in an external PostgreSQL; the dashboard is behind
a load balancer with OIDC sign-in; the wizard builds the machine image with `ops`.

### The core and runner split (verified 2026-11-17)

The split has two steps:

1. The core holds no file path (plans, plan JSON, job logs and layer outputs are rows in
   PostgreSQL; policy rules and runbook scripts come from the forge).
2. Each job runs in a `clanker runner` process that gets its one job from the core over
   HTTP with a token.

What ran on the build of commit `376617b`, the last change to the Go code:

- The full run from an empty data directory, with the live model: thirteen work items,
  twelve in `Done` and `WI-0005` in `RolledBack`. The audit chain verified at 310 events.
  The log of the control plane had no error line. The run used the script that became
  `scripts/verify/fresh-run.sh`; the committed file itself was not run.
- `scripts/verify/sweep.py`: only `GET /topology` without `env` answered other than 2xx,
  which is correct.
- `scripts/verify/stale_plan.py`: the stale plan was refused, planned again and approved
  again. Both changes are on `main` of the forge and the objects pass `git fsck`.
- The data directory had no `plans`, `creds` or `repos` directory after the run.

### Next steps, in order

1. **Local-only code behind interfaces.** Done. The code that starts `kubectl` is in
   `internal/localtarget`, which only `cmd/clanker/up.go` wires in, and only for the local
   target. The broker takes a `broker.Issuer` and the simulator takes a `ManualChange`
   function. On a cloud both are absent: the broker exchanges no grant and the drift scene
   reports an error. `scripts/lint-core.sh` follows the imports of the core packages inside
   this module. The committed `scripts/verify/fresh-run.sh`, `sweep.py` and `stale_plan.py`
   ran on the build of commit `e5d1997` with the same results as before: thirteen work
   items, the chain at 310 events, no error line. The path without an issuer has unit
   tests only.
2. **A core entry point with no file.** Done for the start of the core; the secret store is
   open (issue #40). `clanker up --cloud <not local>` makes no directory and refuses
   `--data`. A released binary reads its source files from memory (`bundle.FS`). The forge,
   the simulator and the dashboard take a file system, and the broker takes its key. The
   core reads `CLANKER_DATABASE_URL` and `CLANKER_SIGNING_KEY` (base64, 64 bytes) from the
   environment and stops when one is absent. **These two variables are for a laptop only.**
   `ops` writes `Env` into the image, so the image for a cloud account must not carry them;
   there they must come from a secret store at start.
   - Ran as a host process (`scripts/verify/core-check.sh`, on commit `4c26150`): a binary
     with the tag `bundle`, started outside a checkout with a home directory that does not
     exist, against its own PostgreSQL. The dashboard answered. The repositories of the
     forge had the same trees as the checkout, with the same executable files. The working
     directory and the temporary directory stayed empty and the home directory was not
     made. The check does not watch the rest of the disk.
   - Ran on the local target, on the same commit: `fresh-run.sh` (thirteen work items, the
     chain at 310 events, no error line), `sweep.py` and `stale_plan.py`.
   - Did not run: the core under Nanos (step 3), and the walk through the wizard with a
     released binary. From a released binary on the local path, only `clanker preflight`
     ran, which unpacks the files.
   - The local target keeps its data directory (`broker.key`, `database.password`, the
     unpacked bundle, the files of the runner).
   - Not done: the template `internal/setup/controlplane.yaml` (the ECS design) still
     passes `--data /data`, which the core now refuses. Step 7 replaces that template.
3. **Boot the real core under Nanos** with `ops/run-local.sh`, in the standby mode
   (`clanker up --cloud aws --demo=false`), against PostgreSQL. Only a throwaway probe ran
   under Nanos so far.
4. **A guard for one writer.** The pool has one connection, but that does not stop two
   instances. Put the guard in PostgreSQL. Reconcile jobs that were in flight when the core
   stopped.
5. **OIDC sign-in**, and no trust in the `X-Clanker-User` header. This blocks every public
   deployment.
6. **Cloud runner and broker:** a launcher that starts a Fargate task, an S3 state backend
   in the runner (the job specification has the field `backend`), a broker that uses STS.
   The ECS task definition in `internal/setup/controlplane.yaml` is the starting point.
7. **The wizard for AWS:** build the image with `ops`, create the stack (auto scaling group,
   load balancer, RDS), switch `setup.Config.EnableAWS` on.
8. **A run in a real account.**

### What the owner must give

- A decision on issue #40: where the core gets its secrets on AWS, and whether the core
  uses the AWS SDK for Go. Steps 6 and 7 need it.
- For step 8: the AWS profile, the region and a spending limit; a domain name for the
  certificate; an OIDC identity provider. The owner chose "apply with a budget cap" but
  named no profile, region or amount. Do not pick one of the local profiles.
- The estimate for the home of the control plane is 47 to 50 USD each month. It is from
  list prices and is not checked against the pricing pages.

### Rules that cost time when they are forgotten

- `CLAUDE.md` has the standing rule: merge a pull request when its checks pass **and the
  change was run**. A unit test is not a run for a change to the control plane; use
  `scripts/verify/`.
- A push over SSH needs the owner at the keyboard (Secretive). On 2026-11-17 the owner
  allowed one push over HTTPS with the `gh` token; SSH worked later that day.
- A hook blocks a command line that contains the push command together with the name of
  the default branch, also inside quoted text. Run the push as its own command, and put
  such text in a file.
- The shell is bash 3.2. Put Python and long text in a file, not in a heredoc inside a
  compound command. A safety check refuses compound commands that remove things; run a
  removal as its own simple command.
- Do not script an approval through the dashboard. Approvals in the checks go through the
  API as the demo users.
- The schema has no migration. A change to a table needs `scripts/verify/fresh-run.sh`,
  which starts from an empty database.
- `docs/PLAN.md` sections 5 and 5.4 still describe the first design in places. Section 12
  is the record of the decisions.
- The site and the video were made from an earlier run with 323 audit records. The demo
  now has 310. To refresh them: `node export-data.mjs`, `node build.mjs`,
  `./build-video.sh` in `site/`, then upload the video to the release `site-video`.

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
  objects in the database (done); the split of core and runners; a lease, so that two instances
  never write; OIDC sign-in; the image build and the stack in the wizard; a run in a real
  account.
- **Forge (2026-11-17):** the forge starts no program. It uses go-git 5.19.3 with its
  objects and references in PostgreSQL, and its own three-way merge. The line merge gave the
  same result as `git merge-file` in 400 random cases: 320 clean and equal, 80 refused by
  both. The git client cloned, pushed and fetched over HTTP, and `git fsck --strict` found
  no fault in the objects that the forge wrote. Two changes to lines next to each other
  are a conflict, as in git.
- **Core and runners (2026-11-17):** the control plane has two parts in one program.
  - The core is the packages in `CORE_PACKAGES` of the Makefile and every package of this
    module that they import. `make lint-core` and CI refuse an import of `os/exec` in them.
    The check does not look inside other modules: go-git has a file transport that imports
    `os/exec`, which the forge does not use. The core holds no file path: plans, plan JSON,
    job logs and layer outputs are rows in PostgreSQL, and policy rules and runbook scripts
    come from the forge.
  - A job is a value (`internal/jobspec`). The dispatcher (`internal/dispatch`) gives it to a
    runner and waits. A launcher starts the runner. On the local target the launcher starts
    `clanker runner` as a process on the same machine (`internal/launch`).
  - The runner asks the core for the one job of its token over HTTP (`/runner/v1/`), sends
    the log while the job runs, sends a heartbeat, and reports the result. The token opens
    only that job, and nothing after the result. The user header means nothing there.
  - The core hashes the plan bytes that it stores, and an apply job gets exactly those
    bytes. The runner compares the hash again and never makes a new plan in an apply job.
  - A plan file can hold secret values in clear text. It is now a row in the database, where
    before it was a file in the data directory. No route serves it.
- **Still not ready for the unikernel:**
  - A cloud needs the signing key and the database address from a secret store (issue
    #40), and a `broker.Issuer` that uses the API of the cloud.
  - There is no launcher for a cloud (a Fargate task), and the runner has no remote state
    backend. The job specification has a field for the backend settings.
  - A job in flight is lost when the core stops. The work item must be reconciled at start.
- **Nanos boot test (2026-11-17, a throwaway probe, on a laptop):** a Go program as a Nanos
  image read its environment from the image configuration, made an outbound HTTPS request,
  connected to PostgreSQL 18 and inserted a row, and answered HTTP from the host. A
  subprocess was refused (`function not implemented`). With hardware acceleration the
  report came about one second after QEMU started. A file write failed with `no space left
  on device`, because the image has no free space unless the configuration asks for it.
- **PostgreSQL (2026-11-17):** from an empty data directory, the first deployment and the ten
  scenes of the scripted month ran on PostgreSQL with the live model. The thirteen work
  items ended in the expected states and the audit chain verified at 310 events.
- On a cloud, `clanker up` starts in a standby mode: it serves the dashboard and proposes
  no deployment.

## Nanos on a laptop

`ops/run-local.sh <binary> [port]` runs a static `linux/arm64` binary as a Nanos unikernel
under QEMU on an Apple Silicon Mac. Set `NANOS_CONFIG` to an `ops` configuration file to give
the program its environment.

The script starts QEMU directly, because `ops run` fails on this machine (macOS on Apple
Silicon, QEMU 11.1.3). Three separate defects are the cause. The notes come from the owner's
earlier unikernel work in another repository.

| # | Defect | Workaround |
|---|---|---|
| 1 | `ops` passes `gic-version=2`. From QEMU 11.1, HVF on arm64 uses the vGIC of the kernel, which is GICv3 only. QEMU stops with "HVF does not support GICv2 emulation" | `-accel hvf,kernel-irqchip=off` |
| 2 | `ops run --accel=false` passes `-cpu host`, which is valid only with acceleration. QEMU stops with "Invalid CPU model: host" | `-accel tcg -cpu max` |
| 3 | Nanos 0.1.56 for arm64 crashes in early boot on a real Apple CPU. Emulation does not show the fault | The nightly kernel (`ops build -n`), which has the fix |

Limits:

- The nightly kernel changes each day. Use it on a laptop only. An image for EC2 must use a
  release.
- The result is for arm64 on a laptop. It says nothing about x86 or EC2.
- The arm64 kernel does not boot with GICv3 when QEMU starts it with `-kernel`.
- With QEMU's user networking, the host is `10.0.2.2` from inside the unikernel.

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
| `stale_plan.py` failed once at `git fsck`, and the same command passed on the same clone a minute later | Not proved. The clone had one pack and no loose object afterward, so git probably repacked in the background after the fetch. The script now switches that off and prints the output of a failed fsck |
| `xcodebuild test` hangs | The first-launch packages of Xcode are probably not installed. `ios/build.sh` runs the test bundle with `xcrun simctl spawn` |
