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

### Work in flight (written 2026-11-17, at night)

Read this section first.

**Merged on 2026-11-17, after the list in the section below:** #53 (evals for the Reviewer and
the strict form of its answer), #54 to #57, #59, #64, #65, #67 and #72 (the site and its
pictures), #58 (approval in Slack and Teams), #60 (the number of T3 approvers is a
setting), #62 (the device flow in the iOS app), #68 (the video on one idea), #66, #69 and
#70 (the colours and the icon of S.R.C. in the iOS app, the setup wizard and the
dashboard), #71 (the iOS app is named S.R.C.). Step 5 of the next steps is done.

**No branch waits and no pull request is open.** After the list above, #74 (the dashboard
fits a phone: below 1056 px the side navigation opens from a menu button, and wide content
scrolls in its own region; `pnpm phone-check` in `web/` checks every page at 390 px and
768 px against a running dashboard) and #75 (a test waits for the applies in the
background) are merged.

Later on 2026-11-17 the owner answered the first two points of the order of the work, and
these are merged: #78 (the emails and the chat messages name the product "S.R.C."; the
full run gave thirteen work items, 316 events and no error line, and an email in Mailpit
had the new name) and #77 (unit tests for the navigation drawer of the dashboard, with
`happy-dom` 21.0.4 as the test environment of that one file; CI runs them).

**#79 is merged too.** The owner asked for the figure of the architecture page behind the
title card of the video. The video built from it (99.0 s) and a copy is in
`~/Movies/src-explainer/explainer-title-figure.mp4`. The owner has not heard it, and it is
not uploaded to the release `site-video`, so the public site still has the video without
the figure. Pushes now go over HTTPS with the `gh` token, as the owner said.

**Step 6 (the cloud runner and broker) is started only as questions.** The owner named an
AWS profile, wants everything removable at the end of the week of 2026-11-17, wants no
public load balancer (access is through Tailscale), and asked which VPCs exist. The
spending limit is 200 USD for that week. Only read calls ran in the account, and nothing
was made.

- This repository is public. Do not write the profile, the account, or the names of its
  networks and services here, in an issue or in a pull request. The session keeps them in
  its memory, and the owner knows them.
- Credentials: the owner runs `assume --export <profile>` in their own terminal. It fails
  in a shell that is not interactive. The profile has no region: pass `--region`.
- The role can create, pass and delete IAM roles and policies, and has no permissions
  boundary.
- The account has VPCs with private subnets behind NAT gateways and a running Tailscale
  subnet router. No new VPC and no new NAT gateway are necessary.
- Open: which VPC, and how a Fargate runner reaches `/runner/v1/` of the core. A subnet
  router carries traffic from the tailnet into the VPC, not from the VPC to a laptop.

For the removal: one OpenTofu state, one tag on every resource, one teardown command, and a
tag query that returns nothing as the proof. Make no change to a VPC that exists.

Worktrees that can be removed, because their work is on the main branch:
`src-wt-domtests` (branch `dashboard-nav-tests`), `src-wt-titlecard` (branch `title-card-figure`),
`agent-aa1afd77bc263132f` (branch `chat-approval`, locked), `agent-ae36a6a2b9fad684f` (the
site and the video), `colours-wizard`, `colours-ios`, `colours-dashboard`, `copy-iac`. Many local branches and
a few remote ones are merged too. Nothing was removed with force.

The logs of every live eval round are in `evals/logs/` of the main checkout, which git does
not track. Keep them: `report.py` needs those logs to write the whole of `RESULTS.md` (the
command is in `evals/README.md`, "Compare two runs").

**The site** (https://statik.github.io/site-reliability-clanker/, built from the main branch
by the Pages workflow; a merge that touches `site/` deploys it). What the owner decided:

- The one idea of the site and the video: "Ship an AI site reliability engineer with your
  product instead of an installer." The overview page follows the order of the video
  script: the idea, the problem and its cost, the answer, the setup and three steps for
  every change, the video, the proof, one call to action. The detail sections follow.
- The copy standard of the owner, from the rejections of this day:
  - A section that introduces a concept has no result of a run in it.
  - A heading says what the page or the section is about. No slogan.
  - A text beside a figure describes the concepts, not the drawing or its animation.
  - No analogy. The owner rejected one as strained. For permissions the words are:
    narrowly scoped, issued only for a specific approved change, revoked as soon as the job
    ends.
  - A grid of boxes breaks into rows of the same length (4 and 4, not 7 and 1), and the
    parts of the boxes are at the same height.
- No fish and no paper boat as a mark or a metaphor on the site or in the video. The demo
  application keeps its name Paperboat and its real screenshots. The copy must not name
  Swamp.
- The product is "S.R.C." in the copy. Commands keep the name `clanker`.
- Dark theme only, with the palette in `site/css/site.css`. The icon is a person who
  directs aircraft with two wands.
- The architecture page has an isometric figure of the system (`site/lib/machine.mjs`, made
  with the `anatomy` skill). It is not in the video any more. It is not published to a
  gallery; ask the owner first.
- The pictures of the page "Live screens" come from `site/capture.mjs`, which reads the
  running demo with GET requests only. The picture of the approval page needs a change
  that waits for approval (the same hand-written change as for the data, below).
- The pages are built from `site/data/month.json`. To refresh it: run
  `scripts/verify/fresh-run.sh`, then push one hand-written change that stays in
  `AwaitingApproval` (title "Run two api replicas for the holiday rush", `api_replicas` from
  1 to 2 in `platform/stacks/local/50-app/terraform.tfvars`, as `stale_plan.py` pushes its
  first branch), then `node export-data.mjs` in `site/`. The pages need that waiting
  change: without it the approval example shows the incident. No script for this step is
  committed.

**The video.** Rules of the owner:

- Never build the video with the local voice of macOS, not as a draft and not at all. The
  branch removes that voice from the build. The keys `ELEVEN_LABS_API_KEY` and
  `ELEVEN_LABS_VOICE_ID` are in `.env` of the main checkout; a worktree has no `.env`.
  Each sentence is synthesized once and kept in `site/out/cache/speech/`.
- Do not upload a video to the release `site-video` before the owner approves it by ear.
- The video on the public site is the build on one idea: 99.2 s, the script in
  `site/narration.md`, which the owner approved line by line and by ear. Its scenes are
  drawn pages that read `site/data/month.json`, so the build needs no running demo. A copy
  of each build that the owner heard is in `~/Movies/src-explainer/`.
- The script describes the product as designed. The owner decided that the site and the
  video do not say "in progress": the line about the setup says that it walks you through
  logging in to your cloud provider, and the wizard sets up only the local target today.
- The crackling that the owner heard in one build is gone, and the owner confirmed it by
  ear. The cause was in the mix, not in the voice clips: the music had a layer of filtered
  noise and rose in each pause (a sidechain compressor), and the voice was raised and
  limited. Now the music has no noise layer and one low level, and the voice gets one
  gain to a peak of -3 dB with no limiting. Keep this mix.

**Decisions of the owner in this session that are not in an issue**

- The model can raise a risk tier and never lower one (the adversarial review, #50).
- No Swamp. Typed runbooks in this repository (section 5.6 of the plan, #52).
- Two approvers for tier T3 is a setting: `runbooks/policy/approvals.json`, the value 1 or
  2, two when the file is absent. An invalid file is an error.
- The wizard, the iOS app and the dashboard have the colours and the icon of the website,
  dark only. Each has one palette file with the values of `site/css/site.css`:
  `internal/setup/ui/style.css`, `ios/App/Views/Components/Palette.swift`,
  `web/src/styles/_palette.scss`. The iOS app and the dashboard have tests that calculate
  the contrast of every pair.
- The iOS app is named S.R.C.: target `SRC`, bundle identifier `dev.clanker.src`. A person
  with an earlier build removes it and signs in again.
- The site and the video describe the product as designed. They do not say "in progress".
  Reports, pull requests and this file still say what ran and what did not.
- The owner has a free Slack workspace as its admin. A test with the real Slack is possible
  now: the owner creates the app from `ops/chat/slack/manifest.yaml` and puts
  `CLANKER_SLACK_SIGNING_SECRET`, `CLANKER_SLACK_BOT_TOKEN` and `CLANKER_SLACK_CHANNEL` in
  `.env`. It needs a public address (`ngrok` is installed; ask the owner before traffic
  goes through it) and the sign-in switched on. Teams is put off: the free developer
  tenant of Microsoft is only for some accounts.

**Questions that wait for the owner**

- How is a pull request on the `runbooks` repository tiered and approved? Until that
  exists, a runbook and the policy rules cannot change after the first seed.
- A change to `policy/approvals.json` must not be approvable under the rule that it
  lowers: a change of `t3_approvers` from 2 to 1 must itself need two approvers. The pull
  request path for `runbooks` must enforce this. Until that path exists, the file can be
  set only before the first start, because the forge copies `runbooks/` only once.
- Does the owner want a pass over the functions that are over a cyclomatic complexity of
  8, and a check in CI? Only the functions of this session were brought under it.

**Known gaps from this session**

- The strict parser of the Reviewer refused one live answer in one of the four full runs whose reviews were read ("the
  answer does not start with a JSON object", `WI-0011`). The tier of the policy engine
  stood and an `agent.failed` event recorded it. The answer is lost, because the database
  of that run is gone. Every model call is in the table `agent_calls` with its answer:
  when `agent.failed` appears again after a full run, read the answer there before the
  next `fresh-run.sh`, and then decide between a change of the prompt and one retry.
  `fresh-run.sh` counts only error lines of the log, so it does not show this failure.
- Approval in chat ran against a stand-in for Slack only (`scripts/verify/chat_check.py`).
  Teams ran in unit tests only.
- The iOS app: not run on a real phone, and the button that opens the browser was not
  pressed. CI does not build the app.
- The work item detail of the API gives an empty list `valid_approvers` for the first
  deployment after it is done. This was so before this session. It was not examined.
- `docs/API.md` still says that the prototype has no authentication.
- On the audit page of the dashboard, the text "Server search / Pick a range on the
  timeline" overlaps the heading of the column "Work item".
- The demo application Paperboat keeps its own look and its boat mark. The owner asked
  only that the product has no fish or boat as a mark.

**The order of the work now**

1. Let the owner hear the video with the figure behind the title card, then upload it.
2. Step 6, after the owner answers the questions above. Issue #40 has the decision for
   its secrets (Parameter Store with the AWS SDK for Go v2).
3. The pull request path for runbooks, when the owner decides how.
4. Issue #51: scrub the input of the model roles before the JSON frame.

**Things that cost time in this session**

- The hook that guards pushes refuses any command line that has the push command and the
  word for the default branch, also inside a branch name, beside `--base` and in
  `origin/<default branch>` later on the same line. Give a branch a name without that
  word, and run the push as its own command.
- A force push and `git reset --hard` are refused. When a pull request conflicts after
  another merge, rebase, push the result under a new branch name, open a new pull request
  and close the old one.
- A merge to the main branch of one pull request can make the next one conflict in
  `docs/HANDOFF.md`. Merge them one after the other.
- A safety check can misread a long Python heredoc with backslashes as a removal and
  refuse it. Write the script to a file in the scratchpad and run the file.
- `cd` into a worktree moves the session. Use `cd` back or `git -C`.
- Do not switch the branch of the main checkout while `fresh-run.sh`, `core-check.sh` or
  `nanos-check.sh` runs. The two core checks need a checkout with no change that is not
  committed. The demo is one shared thing: run one verify script at a time.
- The full run ends at 316 or 317 audit events (see `scripts/verify/README.md`).
- `git fsck` right after a fetch can fail once because of background maintenance of git.
- Measure complexity with
  `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 8 <files>`.
- A subagent in a worktree cannot use the demo. It can start its own control plane in
  standby mode on its own ports with its own PostgreSQL container, as the iOS work did
  (7270 and 7290). Each branch that changes the control plane needs the full run in the
  main checkout before its pull request.
- CI does not run the tests of `evals/`, of the site or of the iOS app. Run
  `uv run pytest -q` in `evals/` when Go code that the evals mirror changes.
- A test that ends while an apply still runs in the background can close its database
  under the apply, and the Go job then fails on a pull request that changed no Go code. A
  test that approves a change must end with `Orchestrator.WaitForApplies` (#75).
- The dashboard at phone width was checked in Chromium only, not on a phone, in Safari or
  with a screen reader. Each region that scrolls sideways is a tab stop at desktop width too.
- The change that waits for approval in the demo ("holiday rush") was applied in a live
  check after the pictures of the site were taken. Push it again before the next capture.

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
     audit event. A tier T3 change needs two approvers unless `policy/approvals.json`
     says one, and a control plane on a cloud has only the first admin until somebody
     adds a row to the users table. The request for a code is open to everybody and is
     not slowed down.
   - **Done: the device flow in the iOS app.** When the control plane answers 401, the
     app shows a code and the address of the page, asks for its token at the interval of
     the server, and keeps the token in the Keychain, one for each server address. Every
     request to the control plane, also the event stream, sends it as a bearer token. A
     later 401 removes the token and shows the sign-in again with a message. Settings
     shows the person from `GET /api/v1/me` and has a sign-out, which only forgets the
     token on the phone. A control plane without a sign-in gets the user header as before.
     `GET /api/v1/auth` is behind the gate: a control plane with a sign-in answers it with
     401 to a caller it does not know, so the 401 is how the app learns that people sign in.
   - Ran: `ios/build.sh` (105 tests pass). In the simulator against a control plane in
     standby mode with the fake identity provider: the app showed a code, the admin
     approved it with a browser session (curl), the app showed the admin and loaded the
     data, and the token was still there after a restart of the app. After the removal of
     the token row in the database and a restart of the app, the app showed the sign-in
     with the message.
   - Not run for the iOS app: a real phone, the button that opens the browser, the wait
     while the browser is in front, a 401 in a session that runs, a refusal or the end of
     a code against a real server, and the demo path against a live demo. Unit tests
     cover all of these but the button. The Keychain needs a signed app: the
     simulator build is now signed "to run locally", and without that signature the
     Keychain answered with status -34018.
   - Not built: there is no page to add a user or change a role: a
     user other than the first admin needs a row in the users table. There is no list of
     tokens and no way to end one token before its twelve hours. The core does not slow
     down a program that asks for its token too often.
   - **Done, with fakes only: approval inside Slack and Microsoft Teams** (issue #49,
     `internal/chatops`, setup in `ops/chat/README.md`). **Nothing ran against the real
     Slack or the real Teams, and the change was not run end to end in a control plane.**
     The owner deferred the real test until a Slack app and a Teams bot registration exist.
     - A person links a chat account once at `/auth/chat-link`, with a browser session of
       the sign-in. A program token cannot link. A link code works once and for ten
       minutes. The same page lists the links of the person and removes one.
     - A click goes to `POST /chat/slack/interactions` or `POST /chat/teams/messages`. Both
       are open in the gate and accept only a request that the platform signed: Slack with
       HMAC-SHA256 over the raw body and a timestamp of at most five minutes, Teams with a
       token of the Bot Connector (signature, issuer, audience, validity, endorsement of
       the key for the channel, `serviceUrl`, and the tenant of the bot).
     - The decision goes through `Orchestrator.Decide` as the user with the email of the
       link. The message carries a binding of the commit and the plan hashes, and
       `Decide` refuses a click on a message of an older plan. `Decide` now runs one
       decision on a work item at a time, also for the dashboard.
     - Chat decides tier T2 and lower. A tier T3 request has only a link.
     - After a decision, and when the work item leaves `AwaitingApproval` for another
       reason or its plan changes, the posted message shows the outcome and has no action.
     - Audit: `chat.linked`, `chat.unlinked` and `chat.refused` are new. `approval.recorded`
       and `approval.refused` carry `origin` (platform, workspace or tenant, account) for a
       decision from chat.
     - Settings: `CLANKER_SLACK_SIGNING_SECRET`, `CLANKER_SLACK_BOT_TOKEN`,
       `CLANKER_SLACK_CHANNEL`; `CLANKER_TEAMS_APP_ID`, `CLANKER_TEAMS_APP_SECRET`,
       `CLANKER_TEAMS_TENANT_ID`. The secrets come from the environment, which is for a
       laptop only (issue #40). With the settings of a platform, the app takes the place of
       the webhook of that platform. Without them nothing changes, and no new audit event
       is emitted. A chat app with no sign-in stops the start.
     - Ran: `go test ./...` with a fake Slack Web API, a fake token endpoint and Bot
       Connector, a test key set and the fake identity provider.
     - Not run: `fresh-run.sh`, `core-check.sh`, `nanos-check.sh` and `restart_check.py` on
       this change. The schema has three new tables (`chat_links`, `chat_link_codes`,
       `chat_messages`), so `fresh-run.sh` must run before a merge. The expected result is
       the same thirteen work items and 310 events.
     - To check first against a real tenant: the request that starts a thread in a Teams
       channel (its form comes from an archived page of the Microsoft documentation), and
       that an invoke activity carries `from.aadObjectId` and the tenant ID.
     - Not built: a comment for a rejection in chat (the comment is fixed), threading,
       more than one Slack workspace or Teams channel, an installation for a whole Slack
       Enterprise Grid organization, a page in the dashboard for the links (the page at
       `/auth/chat-link` does this).
     - To know: the links in the other notifications still use the listen address and not
       `CLANKER_PUBLIC_URL` (`cmd/clanker/up.go`). The chat requests use `CLANKER_PUBLIC_URL`.
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
- The site and the video: see "The site" and "The video" in "Work in flight" above.

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
| iOS app | `ios/`. Builds, 105 tests pass, runs in the simulator against the control plane, signs in with the device flow |
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
- A page to manage users, and a way to end one token.
- A "defer" decision, and enforcement of a maintenance window for tier T3.
- Quiet hours, escalation and threading for notifications.
- A test of the approval in chat against the real Slack and the real Teams (issue #49).
- The move of the control plane into a cloud (bootstrap and pivot).

## Decisions made in this build

| Topic | Decision | Issue |
|---|---|---|
| Workload | Paperboat is a fish rental app: inventory, aging, delivery, customer support | #17 |
| Forge | Built into the control plane | #6 |
| Control-plane database | PostgreSQL, outside the process (changed 2026-11-17) | #7 |
| First deployment | One approval. Later layers must stay inside a create-only envelope | #8 |
| Inference | OpenRouter, confirmed by the owner on 2026-11-16 | #15 |
| Approval in chat | In Slack and Teams, with a chat account that is linked once through the sign-in. Tier T2 in chat, tier T3 on the dashboard | #49 |
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
