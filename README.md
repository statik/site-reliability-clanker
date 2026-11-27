# site-reliability-clanker

Prototype of an AI site reliability engineer. A dashboard and an agent deploy and maintain a
multi-service application. Each change is OpenTofu code in a pull request. The agent explains
the change in plain language, a human approves it, and a runner applies it with a credential
that exists only for that job.

The managed application is **Paperboat**, a fish rental business: live inventory, fish aging,
deliveries and customer support.

- [Prototype plan](docs/PLAN.md): architecture, use cases, permissions and build order.
- [Handoff](docs/HANDOFF.md): what works now, what is validated only, and what is open.
- [Platform contract](platform/CONTRACT.md): the OpenTofu interface for each cloud.

## Install with the setup wizard

A released `clanker` binary carries the dashboard and the platform code, so it needs no
checkout. Start it with no command, or with `clanker setup`. It opens a wizard in your
browser, on the loopback address only.

```
curl -fsSL -o clanker https://github.com/statik/site-reliability-clanker/releases/latest/download/clanker-darwin-arm64
chmod +x clanker
./clanker
```

The wizard has two targets:

| Target | What the wizard does | Status |
|---|---|---|
| This machine | Checks the tools, takes the model key, starts the control plane. The control plane then plans the first deployment and waits for approval | Runs end to end |
| Your AWS account | Not available yet. See "Direction for the cloud control plane" in `docs/HANDOFF.md` | Not built |

The wizard never approves a change. To build the binaries yourself, run `make release`.

## Run the local demo

You need Docker (running), `tofu`, `kubectl`, `helm`, `kind`, Go, Node 22 and `pnpm`.
The tests also need `opa` and `git`.

The control plane keeps its data in PostgreSQL. On the local target `clanker up` starts a
PostgreSQL container for the data directory. To use another database, set
`CLANKER_DATABASE_URL`. The Go tests start their own PostgreSQL container, or use
`CLANKER_TEST_DATABASE_URL`. `nix develop` provides all of them except Docker.

```
make demo
```

This command builds the control plane and the dashboard, then starts them. The first start
takes about ten minutes, because it downloads the cluster image.

| Address | What it is |
|---|---|
| http://127.0.0.1:7070 | S.R.C. dashboard |
| http://localhost:8088 | Paperboat, after the first deployment is approved and applied |
| http://127.0.0.1:8025 | Mailpit: the local inbox for notification email |
| http://127.0.0.1:7071 | Webhook sink: the local stand-in for Slack, Teams and Discord |

1. Open the dashboard. One work item waits: the blank-slate deployment.
2. Select the user `alice` in the header and approve it. Then select `bob` and approve it.
   The deployment is tier T3, so it needs two approvers.
3. Open the **Demo** page and run the scenes of the scripted month in order.

To remove the environment, its history and its saved setup:

```
./bin/clanker reset
```

## The agent and the model

The agent uses OpenRouter. Put the key in a file named `.env` in the repository root:

```
OPENROUTER_API_KEY=sk-or-...
```

Without a key, the agent runs in replay mode with recorded responses. To force replay mode
with a key present, set `CLANKER_AGENT_MODE=replay`. The recorded responses pass the eval
scorers, but nobody ran the full demo in replay mode yet: every run so far used the live
model. The runtime stops model calls for the day after 5 USD.

The agent can also use an OpenAI-compatible chat endpoint, for example a model server on the
same host. These settings are environment variables of `clanker up`:

| Setting | Meaning |
|---|---|
| `CLANKER_LLM_BASE_URL` | The base address of the endpoint, for example `http://127.0.0.1:11434/v1`. The agent sends requests to `<base>/chat/completions`. With this setting, the agent does not use OpenRouter and needs no `OPENROUTER_API_KEY`. |
| `CLANKER_LLM_API_KEY` | Optional. The bearer key for the endpoint. |
| `CLANKER_LLM_MODEL` | The model name for every role. |
| `CLANKER_LLM_MODEL_<ROLE>` | The model name for one role. `<ROLE>` is `TRIAGE`, `EXPLAINER`, `AUTHOR`, `INVESTIGATOR` or `REVIEWER`. |

If `CLANKER_LLM_BASE_URL` is set and a role has no model name, `clanker up` stops at the
start with an error. `CLANKER_AGENT_MODE=replay` still forces replay mode. The dashboard and
the audit log show this mode as `local`. The agent waits 10 minutes for one answer of this
endpoint and asks for an answer of at most 8192 tokens.

A call to such an endpoint reports no cost, so the limit of 5 USD does not stop it. The
runtime also stops model calls for the day after 5 million tokens in calls that report no
cost.

The agent can also use Amazon Bedrock in an AWS account, through the Converse API. It needs
no API key: AWS credentials sign each call.

| Setting | Meaning |
|---|---|
| `CLANKER_LLM_PROVIDER` | `bedrock` uses Amazon Bedrock. Without this setting the agent uses `CLANKER_LLM_BASE_URL` or OpenRouter as above. Another value stops `clanker up` at the start. |
| `CLANKER_LLM_REGION` | The region of Bedrock. Without it the agent uses `AWS_REGION`. One of the two is necessary. |
| `CLANKER_LLM_MODEL` | The ID of an inference profile for every role, for example `us.anthropic.claude-sonnet-5-5`. |
| `CLANKER_LLM_MODEL_<ROLE>` | The ID of an inference profile for one role. |

Without a model setting, the Triage and Explainer roles use `us.anthropic.claude-haiku-5-5`,
and the Author, Investigator and Reviewer roles use `us.anthropic.claude-sonnet-5-5`. The
`us.` profiles serve a region in the United States. In another region, set the models to the
profiles of that region, for example `eu.` or `global.`.

The credentials depend on where the control plane runs:

- On AWS, the control plane uses the role of its instance. The home stack gives that role
  `bedrock:InvokeModel` on the chosen inference profiles and on the foundation models that
  they route to. The installer writes the settings to Parameter Store. In the wizard, the
  step Language model of the AWS target offers Amazon Bedrock first: it lists the inference
  profiles of the region and calls each chosen model once with your profile. The command
  line has the same choice: `clanker install --cloud aws --model-provider bedrock`, with
  `--bedrock-model ROLE=ID` for a role that needs another profile.
- On this machine, `clanker up` uses the AWS credentials of the environment: the keys in
  `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`, or the AWS CLI profile in `AWS_PROFILE`.
  It reads them once at the start, and stops with an error when it finds none or the
  session of the profile has expired.

The dashboard and the audit log show this mode as `bedrock`. The agent asks for an answer of
at most 16000 tokens, because the thinking of the model counts against the same limit, and
waits 5 minutes for one answer. The cost of each call comes from the on-demand list prices
of the default profiles and of their `global.` profiles. A call to another model records no
cost, so the limit of 5 million tokens applies to it.

Stop a running control plane before you start `make demo` again: `pkill -f "clanker up"`.

## What is in the repository

| Path | Content |
|---|---|
| `cmd/clanker` | The CLI: `preflight`, `bootstrap`, `up`, `destroy`, `reset`, `sink` |
| `internal/` | The control plane: orchestrator, runner, policy, access broker, audit log, forge, notifier, agent, detectors, simulator, HTTP API |
| `web/` | The dashboard (React, Carbon Design System) |
| `platform/` | OpenTofu modules and stacks for `local`, `aws`, `gcp`, `azure` |
| `apps/paperboat` | The managed application, its Helm chart and its releases |
| `runbooks/` | Runbook jobs, the risk tier policy (Rego) and the notification routes |
| `evals/` | Inspect AI evaluations for the agent roles. Results: `evals/RESULTS.md` |
| `site/` | The concept website and the build of the explainer video |
| `ios/` | S.R.C. for iOS, an app to review and approve work from a phone |

## Website, video, evals and iOS app

| Task | Command |
|---|---|
| Preview the website | `cd site && python3 -m http.server 4173`, then open http://127.0.0.1:4173 |
| Export the data and capture the screens | `cd site && node export-data.mjs && node capture.mjs` |
| Render the video | `cd site && ./build-video.sh` |
| Run the evals without a model | `cd evals && ./run.sh mock` |
| Run the evals with the live model (costs about 2 USD) | `cd evals && ./run.sh live` |
| Build and test the iOS app | `ios/build.sh` |
| Run the iOS app in the simulator | `ios/run.sh` |

The video is written to `site/out/explainer.mp4`. The export and the capture need the demo to
run. The video needs only `site/data/month.json` and the ElevenLabs voice (see
`site/README.md`).

## Tests

```
make test
make lint
```

## Status of the cloud targets

Only the `local` target was applied and tested. The `aws`, `gcp` and `azure` stacks pass
`tofu validate`. Nobody applied them: that needs credentials and an approved budget.
