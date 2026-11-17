# Checks against the running local demo

These scripts drive a real control plane on `http://127.0.0.1:7070`. They are how a change
to the control plane is verified before it merges. They need Docker, the tools of the local
target and Python 3.

| Script | What it does | Time |
|---|---|---|
| `fresh-run.sh` | Destroys the local environment, empties the data directory, starts the control plane and runs `month.py` with all ten scenes | About 40 minutes |
| `month.py <scene>...` | Waits for the first deployment, then runs each scene. It approves what waits, as `alice` (and `bob` for tier T3), through the API. At the end it verifies the audit chain | Depends on the scenes |
| `sweep.py` | Calls every read route of the API, the event filters and one Ask question. It lists each answer that is not 2xx. `GET /topology` without `env` answers 400; that is correct | Under a minute |
| `stale_plan.py <new directory>` | Pushes two branches that change different lines of one file with the git client, opens two pull requests, and approves them one after the other. The second plan is stale: the forge must merge, the plan must run again and the approval must be asked again | About 3 minutes |

## What a good run looks like

- `fresh-run.sh`: thirteen work items. Twelve end in `Done`. `WI-0005` (the faulty release)
  ends in `RolledBack`. The last line before the error count is
  `chain: {'ok': True, 'count': 310, ...}`. The error count is 0.
- `stale_plan.py`: the last line is
  `RESULT: both changes are on main and the objects pass git fsck`.

## Limits

- The approvals go through the API with the user header. This is the demo identity, not a
  sign-in. A person must test an approval from the dashboard or the iOS application.
- `fresh-run.sh` deletes the demo. It stops when the destroy fails, so it does not throw away
  the state of an environment that still exists.
