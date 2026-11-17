# Handoff

State of the work on 2026-11-17. A new session can continue from this file and from the
issues with the label `needs-owner-review`.

## Start here (written at the end of the session of 2026-11-17)

### The goal now

The control plane must run on AWS as a Nanos unikernel in an auto scaling group, set up by
the wizard (`clanker setup`). The decisions of the owner are in section 12 of
`docs/PLAN.md` and in issue #11. In short: the core starts no program and keeps no file; jobs
run in short-lived runners; all state is in an external PostgreSQL; people sign in with
OIDC in the core itself (changed by the owner on 2026-11-17, issue #45); the wizard builds
the machine image with `ops`.

### Work in flight (written 2026-11-17, late in the day, before a new session)

Read this section first. It names work that is on this machine only.

**Merged today:** #38, #39, #42, #43, #44, #46, #47, #48, #50, #52. Next steps 1 to 4 are
done. Of step 5, the sign-in for a browser and the device flow for git and scripts are done.
The adversarial review and typed runbooks are also merged.

**Branches that are not merged.** Each is local unless the text says that it is pushed.

| Branch | Where | State | To finish |
|---|---|---|---|
| `reviewer-evals-checked` | the main checkout | The evals for the Reviewer, a strict parser for its answer, `deferred_layers` in its input, and the fix that keeps secrets out of its concerns. The live eval round ran (see `evals/RESULTS.md` on the branch). The full run on commit `6634911` passed: thirteen work items, the chain at 316 events, no error line, `sweep.py` and `stale_plan.py` as usual. All eight reviews completed and none failed. The Reviewer kept `WI-0005` at T2 with three concerns in this run; in two earlier runs of the same day it raised it to T3. A mirror test of the evals still looked for the mitigation list that typed runbooks removed from the Go code; it now reads the runbook definitions | Push, open a pull request, merge. This text is on this branch |
| `reviewer-evals` | worktree `.claude/worktrees/agent-ad37772b47ed8d947` | The same commits before the move onto the main branch | Remove the worktree after the merge |
| `site-video-intro` | worktree `.claude/worktrees/agent-ae36a6a2b9fad684f` | The new site and video. A subagent was at work on it when this was written. See "The site and the video" below | See below |
| `chat-approval` | worktree `.claude/worktrees/agent-aa1afd77bc263132f` | Approval inside Slack and Teams (issue #49). A subagent was at work on it and had not reported. Nothing of it ran end to end | Read its commits and its tests. Move it onto the main branch. Run `fresh-run.sh`, `core-check.sh` and `nanos-check.sh`. The real Slack and Teams are not tested: the owner put that off |

The worktrees of earlier sessions are removed. Only the three worktrees in the table are
left in `.claude/worktrees`. The local branches of the removed worktrees still exist; all
their work is on the main branch.

The logs of every live eval round are in `evals/logs/` of the main checkout (`before`,
`round2`, `round3`, `round4`, `live`, `repeats`, `repro`, `repro2`), which git does not
track. Keep them: `report.py` needs those logs to write the whole of `RESULTS.md` (the
command is in `evals/README.md`, "Compare two runs").

**The site and the video** (branch `site-video-intro`). The owner asked for all of this:

- No fish and no paper boat as a mark or a metaphor, anywhere on the site or in the video.
  The demo application keeps its name Paperboat and its real screenshots.
- The product is called "S.R.C." in the copy. Commands keep the name `clanker`.
- Tagline: "Don't build a legacy product installer. Build an AI site reliability engineer
  that installs, upgrades, patches, and monitors your product."
- More lines of the owner: "The convenience of managed services without needing a 3rd party
  contract." "Inference is now one of the core ingredients alongside storage, memory, and
  compute." "Your product should get better and easier to manage automatically when new
  models come out." "S.R.C. uses AI to understand and propose the change in deterministic
  code, then uses adversarial review to classify the risk level and a strict policy engine
  to determine which changes need to be escalated for human review and approval."
- The problem heading: "Most of operations is maintenance, and most maintenance is
  repetitive and tedious." (not "is late").
- The video shows the flow: download the setup app, sign in to your cloud, start the
  control plane, deploy the product, maintain it. The cloud sign-in is a drawing with the
  label "AWS: in progress". The owner wants the label kept.
- Sections on Slack and Teams, on cost control and runbooks, and on the evals. The copy
  must not name Swamp. It must not say "approve in Slack" before chat approval is merged.
- The colours of the owner's other project (the SuperComm palette): honey `#F0B53F`,
  honeyDeep `#D98E2B`, cream `#F7ECD7`, sky `#8AA8C8`, seaMid `#3A6E8F`, seaDeep `#1B354A`,
  dark `#1C1917`, darkWarm `#292524`, terracotta `#E07856`, sage `#8FBC8F`, red `#C65545`.
  Dark theme only.
- The icon: a person who directs aircraft with two handheld wands.
- An architecture figure made with the `anatomy` skill, for the architecture page and the
  video. The owner wants to see it when it is ready.
- The full-screen video was cropped (`object-fit: cover`); the fix is on the branch.

To finish the site and the video:

1. Look at what the subagent left on the branch, and at the stills, before anything else.
   The owner saw one complete draft (176 s, with the local voice) and wants to see the
   anatomy figure and the next draft.
2. The final build uses the ElevenLabs voice of the owner (approved by the owner; the keys
   are `ELEVEN_LABS_API_KEY` and `ELEVEN_LABS_VOICE_ID` in `.env`). With the demo up and
   clean: `cd site`, `node export-data.mjs`, `./build-video.sh` with `VIDEO_FOOTAGE` not
   set. A new export gives the pipeline scene the record of the adversarial review.
3. Do not upload the video to the release `site-video`, and do not publish the anatomy
   figure to a gallery, before the owner approves the result.

**Decisions of the owner in this session that are not in an issue**

- The model can raise a risk tier and never lower one (the adversarial review, #50).
- No Swamp. Typed runbooks in this repository (section 5.6 of the plan, #52).
- The owner has a free Slack workspace as its admin. A test with the real Slack is possible
  when `chat-approval` is ready: the owner creates the app from the manifest on that branch
  and puts `CLANKER_SLACK_SIGNING_SECRET`, `CLANKER_SLACK_BOT_TOKEN` and
  `CLANKER_SLACK_CHANNEL` in `.env`. It needs a public address (`ngrok` is installed; ask
  the owner before traffic goes through it) and the sign-in switched on. Teams is put off:
  the free developer tenant of Microsoft is only for some accounts.

**Questions that wait for the owner**

- Do the setup wizard, the iOS app and the dashboard also get the new colours and the new
  icon? The recommendation was yes, the dashboard last, because it needs a Carbon theme
  and new footage.
- How is a pull request on the `runbooks` repository tiered and approved? Until that
  exists, a runbook and the policy rules cannot change after the first seed.
- Does the owner want a pass over the 101 functions that are over a cyclomatic complexity
  of 8, and a check in CI? Only the functions of this session were brought under it.

**The order of the work after the branches above are merged**

1. The device flow in the iOS app (the last part of step 5).
2. The new colours and icon for the wizard, the iOS app and the dashboard, if the owner
   says yes.
3. The pull request path for runbooks, when the owner decides how.
4. Step 6: the cloud runner and broker. Issue #40 has the decision for the secrets
   (Parameter Store with the AWS SDK for Go v2).
5. Issue #51: scrub the input of the model roles before the JSON frame.

**Things that cost time in this session**

- The hook that guards pushes refuses any command line that has the push command and the
  word for the default branch, also inside a branch name and beside `--base`. Give a
  branch a name without that word, and run the push as its own command.
- A safety check can misread a long Python heredoc with backslashes as a removal and
  refuse it. Write the script to a file in the scratchpad and run the file.
- `cd` into a worktree moves the session. Use `cd` back or `git -C`.
- Do not switch the branch of the main checkout while `fresh-run.sh`, `core-check.sh` or
  `nanos-check.sh` runs. The two core checks need a checkout with no change that is not
  committed.
- The full run ends at 316 or 317 audit events now, not 310 (see
  `scripts/verify/README.md`).
- `git fsck` right after a fetch can fail once because of background maintenance of git.
- Measure complexity with
  `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 8 <files>`.
- A subagent in a worktree cannot use the demo. Each branch of a subagent needs the full
  run in the main checkout before its pull request.

### State of the next steps

Also built in that session, on the request of the owner: the adversarial review. After
the policy engine sets the tier of a planned change, the Reviewer role (a second model call
with its own prompt, `internal/agent/reviewer.go`) reads the plans, the policy decision and
the diff and looks for reasons for a higher tier. The orchestrator takes the higher tier
and never a lower one. A failed review leaves the tier of the policy engine and an audit
event. The approval page shows the review and its concerns.

- Ran on commit `955ffca`: `fresh-run.sh` with the live model. The review agreed with the
  policy engine on five changes and raised the faulty release `WI-0005` from T2 to T3,
  because of the tag `1.2.0-bad`. The chain verified at 317 events with no error line.
  `sweep.py` and `stale_plan.py` gave the usual results.
- Not run: the approval page with the review in a browser (only the lint, the type check
  and the tests of the dashboard ran). A work item that goes back to approval after a stale
  plan keeps its tier and is not reviewed again. Evals for the Reviewer are written on the
  branch `reviewer-evals` and no live round ran.
- The full run no longer ends at exactly 310 events: see `scripts/verify/README.md`.

Also built in that session: typed runbooks (section 5.6 of the plan). The owner decided not
to use Swamp, because of its licence and its account dependency, and to build the same
ideas in this repository. Each runbook has a definition file beside its script
(`runbooks/jobs/<name>.json`) with its tier, its inputs with the values that each one
permits, and its typed outputs. The control plane refuses a request that does not fit the
definition before a work item or a grant exists, and records the outputs of each run.

- Ran on commit `c953da1`: `fresh-run.sh` (thirteen work items, 317 events, no error line;
  the backup runbook reported its backup file and 92 objects), `sweep.py`, `stale_plan.py`,
  `core-check.sh` and `nanos-check.sh`.
- A database from before this change refuses every runbook, because its `runbooks`
  repository has no definition files and the forge does not seed a repository twice. Start
  from an empty database (`fresh-run.sh`).
- Not built: a pull request for a change to a runbook, and a standing approval that names
  the commit of the runbook. The prompt of the Investigator and the evals still hold their
  own list of runbooks; a Go test fails when the prompt and the definitions differ.

The owner decided these things in that session:

- Issue #40: on AWS the core reads its secrets from Parameter Store with the AWS SDK for
  Go v2. This is work for steps 6 and 7.
- Issue #41: after a restart, plan again and ask for approval again. Built.
- Issue #45, for step 5: the sign-in is in the core and not at a load balancer, so that
  Okta, Google or another provider works on every cloud. A role comes from the users table
  by email. Clients that are not a browser use a device grant flow that the core runs
  itself.

Not done in that session: the iOS app was not built after the change that shows a lost job
as an error (`ios/App/Views/ActivityView.swift`).

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
   - The walk through the wizard (`web/scripts/setup-wizard-check.mjs`) ran later on the
     same day with a released binary from the main branch after #46, started outside a
     checkout on the data directory of the demo: the control plane started and the
     hand-over link answered.
   - The local target keeps its data directory (`broker.key`, `database.password`, the
     unpacked bundle, the files of the runner).
   - Not done: the template `internal/setup/controlplane.yaml` (the ECS design) still
     passes `--data /data`, which the core now refuses. Step 7 replaces that template.
3. **Boot the real core under Nanos.** Done on a laptop (`scripts/verify/nanos-check.sh`,
   arm64, nightly kernel, HVF). The core in the standby mode
   (`clanker up --cloud aws --demo=false`) answered about one second after the start,
   served the dashboard and seeded the forge from memory with the trees of the checkout.
   A `git push` over HTTP was accepted, and a second unikernel on the same database had
   the pushed branch. No job ran in it: the core proposes no deployment on a cloud yet.
   Nothing ran on x86 or on EC2.
4. **A guard for one writer.** Done.
   - The control plane holds an advisory lock in PostgreSQL on a connection of its own
     (`store.HoldWriter`). A second control plane waits and serves nothing. A holder whose
     connection breaks stops. The lock is not a fence around each write: after a break the
     old holder can still write for a few seconds.
   - At start, `Orchestrator.Reconcile` ends each running job as `lost`, revokes each grant
     that did not end, and fails each work item that was in `Applying` or `Verifying`. It
     does not continue an apply.
   - Ran (`scripts/verify/restart_check.py`, commit `0e6572d`): a second control plane
     waited. After `kill -9` in the middle of an apply, the restarted control plane failed
     the work item, ended its job as `lost`, revoked its grant, and the audit chain
     verified. `fresh-run.sh`, `sweep.py` and `stale_plan.py` gave the usual results on
     the same commit.
   - A work item that was in the plan, or approved and not yet in the apply (`Detected`,
     `Drafting`, `Planned` or `Approved`), is planned again at start and ends in a new
     request for approval. The owner decided this in issue #41. One with no layer to plan
     (a runbook, an incident, a debug session, a change with no pull request yet) fails.
     A work item in `Scheduled` keeps its window and its approval.
     Ran on commit `9209191` (`restart_check.py`): after `kill -9` in the middle of a
     plan, the work item was planned again and waited for approval. A stop of an
     `Approved` work item has a unit test only, because that state lasts a moment.
   - Limit: the kill in the test closed the connection at once. An instance that
     disappears and does not close its connection keeps the lock until PostgreSQL notices,
     which the keepalive settings decide. Until then the next control plane waits and
     serves nothing.
   - Not examined: the runner process of the killed apply. It was gone after the restart,
     but nothing shows whether it finished its `tofu apply` first. A break of the lock
     connection was tested only by the unit tests of the wait, not by a cut connection.
5. **OIDC sign-in**, and no trust in the `X-Clanker-User` header. Issue #45 has the
   decisions of the owner.
   - **Done: the sign-in for a browser.** The core runs the authorization code flow with
     PKCE (`internal/signin`, with `coreos/go-oidc` and `golang.org/x/oauth2`) against a
     provider with a discovery document. A session is a row in PostgreSQL; the browser
     holds a random token in a cookie and the database holds its hash. A session lasts
     twelve hours.
   - With the sign-in on, every route needs a known user except `/healthz`, `/auth/` and
     `/runner/v1/`. The user header means nothing. A person whose email is not in the users
     table is refused. Actions that the demo leaves open (scenes, the clock, the digest, a
     pull request) need a role.
   - Settings: `CLANKER_PUBLIC_URL`, `CLANKER_OIDC_ISSUER`, `CLANKER_OIDC_CLIENT_ID`,
     `CLANKER_OIDC_CLIENT_SECRET` (a secret: not in `Env` of an image for a cloud account)
     and `CLANKER_ADMIN_EMAIL`, the first admin. On a cloud all five are required. The
     demo users are not made when the control plane starts with `--demo=false`; `--demo`
     is on by default. On the local target the sign-in is on when the issuer is
     set; without it the demo is as before.
   - Ran: `core-check.sh` and `nanos-check.sh` with a fake identity provider
     (`scripts/verify/fakeidp`), and `web/scripts/signin-check.mjs` in a real browser on
     the local target. The results are in the pull request.
   - Not run: a real provider (Okta, Google, Entra). The sign-in needs the claim
     `email_verified`, which some providers do not send; that will show with a real one.
   - **Done: the device flow for a program** (`internal/signin/device.go`). It follows the
     OAuth 2.0 device authorization grant, and the core is its own authorization server.
     A program asks `POST /auth/device/code` for a code. A person who is signed in in a
     browser approves the code at `/auth/device`. The program then gets a token from
     `POST /auth/device/token`. A token lasts twelve hours and the database holds only its
     hash. A program sends it as a bearer token, or as the password of basic
     authentication, which is what git does. A program with a token cannot approve a code.
   - `clanker login --server <address>` runs the flow and keeps the token in the
     configuration directory of the user. `clanker git-credential` is a credential helper
     that gives the token to git.
   - `clanker token --server <address>` prints the token for a script.
   - Nobody pushes to the main branch of a repository of the forge over HTTP, with or
     without the sign-in. It changes only when the forge merges a pull request.
   - Ran: `core-check.sh` runs `clanker login`, approves the code as the admin, calls the
     API with the token, clones two repositories with the credential helper, pushes a
     branch, and is refused a push to the main branch. `web/scripts/signin-check.mjs`
     approves a code in a real browser.
   - Limits to know before a run in a real account: a sign-in, a code and a token leave no
     audit event. A tier T3 change needs two approvers, and a control plane on a cloud
     has only the first admin until somebody adds a row to the users table. The request
     for a code is open to everybody and is not slowed down.
   - Not built: the iOS app does not use the device flow yet, and still sends the user
     header, which only the demo reads. There is no page to add a user or change a role: a
     user other than the first admin needs a row in the users table. There is no list of
     tokens and no way to end one token before its twelve hours. The core does not slow
     down a program that asks for its token too often.
6. **Cloud runner and broker:** a launcher that starts a Fargate task, an S3 state backend
   in the runner (the job specification has the field `backend`), a broker that uses STS.
   The ECS task definition in `internal/setup/controlplane.yaml` is the starting point.
7. **The wizard for AWS:** build the image with `ops`, create the stack (auto scaling group,
   load balancer, RDS), switch `setup.Config.EnableAWS` on.
8. **A run in a real account.**

### What the owner must give

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
- The video on the branch `site-video-intro` is a draft. It has the local `say` voice, and
  its four recordings of the dashboard and of Paperboat are cuts of earlier recordings
  (`VIDEO_FOOTAGE`, see `site/README.md`), because the demo was in use. The final build
  needs the ElevenLabs key in `.env` and the running demo. The setup step "Sign in to your
  cloud" in the video is a drawing: the wizard does not set up a cloud yet.

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
- A pull request for a change to a runbook: the main branch of the `runbooks` repository
  cannot change after it is seeded. Section 5.6 of the plan has the open decision.
- Use cases 7, 8, 12 and 23 on a real target. Use case 7 is a forecast on the timeline.
- The device flow in the iOS app, a page to manage users, and a way to end one token.
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
