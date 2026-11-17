# Narration for the explainer video

This file is the script of `out/explainer.mp4`. The video build reads it and makes the
narration, the captions and the timing from it, so change the video by changing this file,
then run `./build-video.sh`.

Format:

- Each `##` heading starts a segment. The heading is the segment's name.
- `source:` is what the segment shows. `fallback:` is what it shows when the source is not
  available (the dashboard is not up).
  - `site:<page>` is a page of this site. The drawn scenes of the video are pages too:
    `site:video/opening.html`, `site:video/setup.html` and `site:video/pipeline.html`.
  - `tour:<name>` and `dashboard:<name>` are the scripted click-throughs in `lib/tours.mjs`.
- Each list item is one caption and one spoken sentence. The segment is as long as its
  sentences.
- A caption can start with a cue in square brackets, for a `site:` source. The recorder
  does it when the sentence starts: `[#id]` scrolls to that part of the page, `[flow:N]`
  moves the system diagram to stop N, `[beat:N]` shows part N of a drawn scene, and
  `[page:file.html]` opens another page.
- A caption can start with `(if event.type)` or `(unless event.type)`, before the cue. The
  sentence is then used only if the month has (or does not have) an event of that type, so
  the narration does not tell of something that a new run of the month did not do.
- `{days}`, `{grants}`, `{held}`, `{events}` and `{rollback_seconds}` are filled in from
  `data/month.json`, so the narration cannot disagree with the data.
- The voice says some words differently from how the caption spells them (AI, AWS, S.R.C.). That
  list is in `video/script.mjs`.

## opening

source: site:video/opening.html

- [beat:1] This is S.R.C.: Site Reliability Clanker.
- [beat:2] Don't build a legacy product installer. Build an AI site reliability engineer that installs, upgrades, patches, and monitors your product.
- [beat:3] Inference is now one of the core ingredients, alongside storage, memory, and compute.
- [beat:5] A Kubernetes version reaches its end of support. A base image gets a vulnerability. The database needs a patch.

## problem

source: site:index.html

- [#problem-title] An AI agent with standing access to production is a risk that you carry every day.
- And the person who must approve a change often cannot read the plan.

## setup

source: site:video/setup.html

- [beat:1] Setup is one download. It opens a wizard in your browser.
- [beat:2] You sign in to your cloud. Today, the wizard sets up the local target only.
- [beat:3] The wizard starts the control plane, and hands it over.
- [beat:4] The control plane deploys the rest of the product: six layers, each under its own grant, after two people approve.
- [beat:5] Then it maintains the product: upgrades, patches, drift and incidents.

## change

source: site:video/pipeline.html

- [beat:2] Follow one change. A detector sees a new release.
- [beat:3] S.R.C. uses AI to understand and propose the change, as deterministic code.
- [beat:4] A strict policy engine then classifies the risk from the plan itself, not from the AI's description.
- [beat:5] It decides which changes a person must approve. Slack, Teams and email tell the approvers.
- [beat:6] The access broker issues a grant, for one layer only.
- [beat:7] The runner applies exactly the approved plan.
- [beat:8] A canary check verifies the result. By then, the grant is gone.

## example

source: tour:paperboat

- Our test subject is Paperboat. It rents out live fish.
- S.R.C. deployed it, and then looked after it for one month.

## approval

source: dashboard:approval
fallback: site:index.html

- [#approver-title] The approver sees one sentence: what changes, and why now.
- [.approval] Then the impact, the risk tier, the safety steps, and the permission requested.

## timeline

source: dashboard:timeline
fallback: site:walkthrough.html

- [#blank-slate] This is the month. A first deployment, with two approvers.
- [#feature-release] A feature release.
- [#cve] A security patch.
- (if verify.failed) [#bad-release] Then a bad release. The health check passed, but the canary failed.
- (if verify.failed) S.R.C. rolled it back in {rollback_seconds} seconds.

## access

source: dashboard:access
fallback: site:architecture.html

- [#grant-title] The agent has no standing write access.
- [.lanes] In {days} days, there were {grants} grants. All together, they lasted {held}.

## close

source: site:index.html

- [#cost-title] Runbooks are fixed scripts. A model for each role and a daily cap control the cost of inference.
- [#evals-title] Evals measure each role. Your product should get better and easier to manage when new models come out.
- [#hero-title] S.R.C. gives you the convenience of managed services, without a third-party contract.
- The model proposes. Code decides. And a person approves.
