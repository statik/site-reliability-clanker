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

You need Docker (running), `git`, `tofu`, `kubectl`, `helm`, `kind`, `opa`, Go, Node 22 and
`pnpm`. `nix develop` provides all of them except Docker.

```
make demo
```

This command builds the control plane and the dashboard, then starts them. The first start
takes about ten minutes, because it downloads the cluster image.

| Address | What it is |
|---|---|
| http://127.0.0.1:7070 | Clanker dashboard |
| http://localhost:8088 | Paperboat, after the first deployment is approved and applied |
| http://127.0.0.1:8025 | Mailpit: the local inbox for notification email |
| http://127.0.0.1:7071 | Webhook sink: the local stand-in for Slack, Teams and Discord |

1. Open the dashboard. One work item waits: the blank-slate deployment.
2. Select the user `alice` in the header and approve it. Then select `bob` and approve it.
   The deployment is tier T3, so it needs two approvers.
3. Open the **Demo** page and run the scenes of the scripted month in order.

To remove everything:

```
make destroy
make clean-data
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

Stop a running control plane before you start `make demo` again: `pkill -f "clanker up"`.

## What is in the repository

| Path | Content |
|---|---|
| `cmd/clanker` | The CLI: `preflight`, `bootstrap`, `up`, `destroy`, `sink` |
| `internal/` | The control plane: orchestrator, runner, policy, access broker, audit log, forge, notifier, agent, detectors, simulator, HTTP API |
| `web/` | The dashboard (React, Carbon Design System) |
| `platform/` | OpenTofu modules and stacks for `local`, `aws`, `gcp`, `azure` |
| `apps/paperboat` | The managed application, its Helm chart and its releases |
| `runbooks/` | Runbook jobs, the risk tier policy (Rego) and the notification routes |
| `evals/` | Inspect AI evaluations for the agent roles. Results: `evals/RESULTS.md` |
| `site/` | The concept website and the build of the explainer video |
| `ios/` | Paperboat Manager, an iOS app to review and approve work from a phone |

## Website, video, evals and iOS app

| Task | Command |
|---|---|
| Preview the website | `cd site && python3 -m http.server 4173`, then open http://127.0.0.1:4173 |
| Export the data, capture the screens, render the video | `cd site && node export-data.mjs && node capture.mjs --video && ./build-video.sh` |
| Run the evals without a model | `cd evals && ./run.sh mock` |
| Run the evals with the live model (costs about 2 USD) | `cd evals && ./run.sh live` |
| Build and test the iOS app | `ios/build.sh` |
| Run the iOS app in the simulator | `ios/run.sh` |

The video is written to `site/out/explainer.mp4`. The capture and the video need the demo to
run.

## Tests

```
make test
make lint
```

## Status of the cloud targets

Only the `local` target was applied and tested. The `aws`, `gcp` and `azure` stacks pass
`tofu validate`. Nobody applied them: that needs credentials and an approved budget.
