# Checks against the running local demo

These scripts drive a real control plane on `http://127.0.0.1:7070`. They are how a change
to the control plane is verified before it merges. They need Docker, the tools of the local
target and Python 3.

| Script | What it does | Time |
|---|---|---|
| `fresh-run.sh` | Destroys the local environment, empties the data directory, starts the control plane and runs `month.py` with all ten scenes | About 10 minutes |
| `month.py <scene>...` | Waits for the first deployment, then runs each scene. It approves what waits, as `alice` (and `bob` for tier T3), through the API. At the end it verifies the audit chain | Depends on the scenes |
| `sweep.py` | Calls every read route of the API, the event filters and one Ask question. It lists each answer that is not 2xx. `GET /topology` without `env` answers 400; that is correct | Under a minute |
| `stale_plan.py <new directory>` | Pushes two branches that change different lines of one file with the git client, opens two pull requests, and approves them one after the other. The second plan is stale: the forge must merge, the plan must run again and the approval must be asked again | About 3 minutes |
| `core-check.sh` | Builds a binary that carries its source files and starts it as the core on a cloud (`clanker up --cloud aws --demo=false`), outside a checkout, with its own PostgreSQL container. It checks the refusals at start, that nobody gets in without a sign-in, the sign-in of the first admin through the fake identity provider, the device flow with `clanker login` and the git credential helper, the dashboard, the seeded repositories and that the core left no file. It does not touch the local demo. The checkout must have no change that is not committed | About 2 minutes |
| `nanos-check.sh` | Builds the same binary for `linux/arm64` and boots it as a Nanos unikernel under QEMU (`ops/run-local.sh`), with its own PostgreSQL container. It signs the first admin in through the fake identity provider, checks the dashboard and the seeded forge, pushes a branch with the git client, boots a second unikernel on the same database and looks for the branch. It needs `ops` and QEMU on an Apple Silicon Mac. It does not touch the local demo | About 1 minute |
| `restart_check.py <new directory>` | Starts a second control plane on the database of the demo, which must wait. Then it opens a pull request and kills the control plane in the middle of the plan: after the start the plan must run again and end in a request for approval. It approves, kills the control plane in the middle of the apply and starts it again. The work item must be `Failed`, its job `lost` and no grant open. It leaves a failed work item and possibly a partly applied change: run `fresh-run.sh` afterward | About 4 minutes |
| `fakeidp` | A fake identity provider (a Go program) for the two checks above and for `web/scripts/signin-check.mjs`. It signs in whoever a check names, with no password. Never use it for a real control plane | - |

## What a good run looks like

- `restart_check.py`: the last line is
  `RESULT: both stops were reconciled and the second control plane waited`.
- `nanos-check.sh`: the last line is
  `RESULT: the core ran as a unikernel, signed the admin in, took a push and kept it across a restart`.
- `core-check.sh`: the last line is
  `RESULT: the core signed in a person and a program, seeded the forge from memory and left no file`.
- `fresh-run.sh`: thirteen work items. Twelve end in `Done`. `WI-0005` (the faulty release)
  ends in `RolledBack`. The last line before the error count is
  `chain: {'ok': True, 'count': 310, ...}`. The error count is 0.
- `stale_plan.py`: the last line is
  `RESULT: both changes are on main and the objects pass git fsck`.

## Limits

- `nanos-check.sh` uses the nightly Nanos kernel on arm64 under QEMU. It says nothing
  about x86 or EC2. The core on a cloud proposes no deployment yet, so no job runs in it.
- `core-check.sh` runs the core as a process on this machine. It says nothing about Nanos.
  It looks for files only in the working directory, the home directory and the temporary
  directory of the core.

- The approvals go through the API with the user header. This is the demo identity, not a
  sign-in. A person must test an approval from the dashboard or the iOS application.
- `fresh-run.sh` deletes the demo. It stops when the destroy fails, so it does not throw away
  the state of an environment that still exists.
