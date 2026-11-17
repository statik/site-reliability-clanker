# Handoff

State of the work as of 2026-11-17, written so a fresh session on a better-equipped machine can
start building without re-deriving anything.

## Where things stand

- **The repository contains the plan and nothing else.** No code, no flake, no modules.
  [`docs/PLAN.md`](PLAN.md) is the source of truth for architecture, use cases, permissions and
  build order.
- A build was started on a cloud sandbox and stopped during environment setup, before any code
  was written, because that machine cannot run the local target (details below).
- Next action: milestone M0 in section 9 of the plan.

## Decisions already made

These came from the project owner and should not be reopened.

| Topic | Decision |
|---|---|
| Clouds | AWS, Google Cloud and Azure from the first milestone, plus a local target |
| Infrastructure as code | OpenTofu, explicitly (the `tofu` CLI), not Terraform |
| UI | IBM Carbon Design System for every UI element; Carbon Charts for charts |
| Orientation | A strong visualisation system with a historical time view (plan section 7.4) |
| Notifications | Email, Slack, Microsoft Teams and Discord |
| Inference | OpenRouter, using OpenRouter keys |
| Toolchain | Nix for the runner image and the developer/CI shell only (plan section 5.5) |
| Unikernels | Try packaging the services as Nanos unikernels (plan section 3.1) |
| Swamp | Fold [swamp-club.com](https://swamp-club.com) into the architecture (plan section 5.6) |
| Outreach | A full website with an animated walkthrough and an explainer video (plan section 7.6) |
| Pull requests | Merge them when ready, without asking |
| Working style | Build as much as possible autonomously |

## Decisions still open

Listed in plan section 12 with defaults. Three matter before code is written:

1. **Forge.** The plan's default is bundled Forgejo. During setup an alternative looked
   attractive: a small forge built into the control plane (bare repositories on disk, the `git`
   CLI for history, diff and merge, `git http-backend` for clone and push, pull requests stored
   in the control-plane database, browsing rendered in the Carbon UI). It removes a service from
   bootstrap and gives one consistent UI. Forgejo gives a complete, familiar forge for free.
   This was not decided.
2. **Control-plane database.** The plan says Postgres. Embedded SQLite would make
   `clanker up` a single binary with no dependencies. Not decided.
3. **Swamp dependency depth.** The plan keeps a plain subprocess executor next to the Swamp
   executor until the M0 spike answers the questions in section 5.6.

## What the sandbox could and could not do

Recorded so nobody repeats the same hour.

| Capability | Result on the sandbox | Consequence |
|---|---|---|
| Docker | Installed; daemon had to be started by hand (`dockerd &`); image pulls from Docker Hub worked | Fine |
| kind | **Failed.** Control-plane pods never start: `runc create failed: unable to start container process: can't get final child's PID from pipe: EOF` (nested containers on a Firecracker guest with cgroup v1) | No local Kubernetes, so the local target cannot be verified there |
| KVM | `/dev/kvm` absent | Nanos under QEMU would run in slow emulation only; not attempted |
| GitHub release downloads | Blocked for repositories other than this one | `tofu` could not be installed from releases |
| OpenTofu | Built from source via the Go module cache as a workaround (v1.10.7) | Works, but not how it should be installed; use the Nix flake |
| kubectl, Helm | Installed from `dl.k8s.io` and `get.helm.sh` | Fine |
| Go, Node, npm, Playwright with Chromium, ffmpeg | Present | Fine |
| Go module proxy, npm registry, OpenTofu registry, OpenRouter API | Reachable | Fine |
| Nix | Not installed; not attempted | Flake cannot be verified there |
| OpenRouter key | None in the environment | Live inference untested |
| Cloud credentials | Not provided for this project; nothing was provisioned in any cloud | AWS, Google Cloud and Azure modules can be validated but not applied |

## What the next machine needs

| Requirement | Why |
|---|---|
| Linux (or macOS) host with Docker and cgroup v2 | kind for the local target |
| Hardware virtualisation exposed (`/dev/kvm` on Linux) | Nanos images under QEMU |
| 8 or more cores, 32 GB memory, 100 GB disk | kind cluster, control plane, image builds and browser tests at once |
| Unrestricted access to GitHub releases, container registries, `swamp-club.com` | Tool installs, Swamp install and registry |
| Nix with flakes enabled | Dev shell and runner image |
| OpenRouter API key | Live agent; without it only replay mode can be exercised |
| Sandbox accounts with billing on AWS, Google Cloud and Azure, and an administrator credential for each | `clanker bootstrap` and milestone M1; without them, the three cloud targets stop at `tofu validate` |
| A swamp-club.com account and a collective token | Swamp spike and CI |
| Webhook URLs for a test Slack channel, Teams channel and Discord channel; an SMTP relay or Mailpit | Notification adapters end to end |

Before provisioning anything in a cloud account, confirm the spend with the owner and make sure
every environment has a TTL.

## Recommended way to run the build

M0 first, by one author, because it fixes the interfaces everything else depends on:

1. `flake.nix` with the dev shell (tofu, kubectl, helm, kind, Go, Node, policy tools) and the
   runner image.
2. `platform/CONTRACT.md`: the exact input and output names for each layer, taken from plan
   section 4.1, so four implementations can be written against it independently.
3. The control-plane HTTP API and event schema, written down before the dashboard is started.
4. Paperboat 1.0.0, its chart, and the local target bringing it up on kind.

After M0 the work splits into streams that touch separate directories and can run in parallel:

| Stream | Directory | Depends on | Verify with |
|---|---|---|---|
| Local platform | `platform/modules/local`, `platform/stacks/local` | Contract | `tofu apply` on kind, conformance suite |
| AWS platform | `platform/modules/aws`, `platform/stacks/aws` | Contract | `tofu validate`, then apply in the sandbox account |
| Google Cloud platform | `platform/modules/gcp`, `platform/stacks/gcp` | Contract | Same |
| Azure platform | `platform/modules/azure`, `platform/stacks/azure` | Contract | Same |
| Paperboat releases | `apps/paperboat` | Nothing | Unit tests, chart install on kind |
| Control-plane core | `internal/*`, `cmd/clanker` | API and event schema | Go tests; a hand-written change through plan, approve, apply on the local target |
| Dashboard | `web` | API and event schema | Playwright screenshots in both Carbon themes |
| Notifier adapters | `internal/notify` | Event schema | Recorded-payload tests, then real channels |
| Swamp spike | `runbooks`, `extensions` | Account and token | The questions in plan section 5.6 answered in writing |
| Nanos spike | `apps/paperboat/unikernel` | KVM | `api` boots under QEMU and serves a request |
| Website and video | `site` | A running demo to capture | Site builds; video renders from one command |

Keep the control-plane core with a single author; it is the part where coherence matters most.

Suggested implementation shortcuts that keep the prototype small without changing its shape:

- Shell out to `tofu`, `kubectl`, `helm` and `git` from the runner instead of linking their
  libraries. It keeps the Go dependency set small and matches what an operator would run.
- Keep the notifier and the OpenRouter client on the Go standard library.
- Per-cloud stacks are separate root modules under `platform/stacks/<cloud>/<layer>`; the
  cloud-agnostic layers (`40-platform`, `50-app`) are shared modules that only use the
  Kubernetes and Helm providers, configured by each cloud's root module.
- On the local target: official Postgres image in-cluster, a statically provisioned
  read-write-many volume, and a port mapping in place of a cloud load balancer are enough to
  satisfy the contract.

## Definition of done for the first working slice

The smallest thing worth showing, and the order to aim for:

1. `clanker bootstrap --cloud local` then `clanker up` on a clean machine.
2. The dashboard opens with one work item: blank-slate deployment.
3. Approving it runs OpenTofu layer by layer and Paperboat answers on localhost.
4. Publishing Paperboat 1.1.0 produces a new work item with an accurate approval page.
5. Approving that upgrades the app; both changes are on the timeline with logs, diff and plan.
6. An approver was notified on at least one channel for each approval.

Everything after that is breadth: the other three clouds, the remaining use cases, the agent,
and the access broker.

## Starting prompt for the next session

> Read `docs/PLAN.md` and `docs/HANDOFF.md` in this repository. Build milestone M0, then
> continue through the build sequence as far as you can, autonomously. Verify each milestone
> by running it, not by inspection. Merge pull requests when they are ready. Before creating
> anything in a cloud account, tell me the expected cost and wait for a go-ahead. Raise the
> three open decisions in the handoff only if one blocks you; otherwise choose the option that
> makes bootstrap simplest and record the choice in the plan.
