# Narration for the explainer video

This file is the script of `out/explainer.mp4`. The video build reads it and makes the
narration, the captions and the timing from it, so change the video by changing this file,
then run `./build-video.sh`.

Format:

- Each `##` heading starts a segment. The heading is the segment's name.
- `source:` is what the segment shows. `fallback:` is what it shows when the source is not
  available (the dashboard is not up, or Blender is not installed).
  - `blender:title` is the 3D title sequence (`video/title.py`).
  - `site:<page>` is a page of this site. `tour:<name>` and `dashboard:<name>` are the
    scripted click-throughs in `lib/tours.mjs`.
- Each list item is one caption and one spoken sentence. The segment is as long as its
  sentences.
- A caption can start with a cue in square brackets, for a `site:` source. The recorder
  does it when the sentence starts: `[#id]` scrolls to that part of the page, `[flow:N]`
  moves the system diagram to stop N, and `[page:file.html]` opens another page.
- A caption can start with `(if event.type)` or `(unless event.type)`, before the cue. The
  sentence is then used only if the month has (or does not have) an event of that type, so
  the narration does not tell of something that a new run of the month did not do.
- `{days}`, `{grants}`, `{held}`, `{events}` and `{rollback_seconds}` are filled in from
  `data/month.json`, so the narration cannot disagree with the data.
- The voice says some words differently from how the caption spells them (AI, AWS). That
  list is in `video/script.mjs`.

## title

source: blender:title
fallback: site:index.html

- Site Reliability Clanker.
- An AI site reliability engineer that asks first.

## problem

source: site:index.html

- [#problem-title] Most of operations is maintenance.
- A Kubernetes version reaches its end of support. A base image gets a vulnerability. The database needs a patch.
- An AI agent could do this work.
- But an agent with standing access to production is a risk that you carry every day.
- And the person who must approve a change often cannot read the plan.

## example

source: tour:paperboat

- So we built a test subject. Paperboat rents out live fish.
- It has a live inventory, fish that age, deliveries, and a support desk.
- Clanker deployed it, and then looked after it for one month.

## change

source: site:architecture.html

- [#flow-title] Follow one change through the system.
- [flow:1] A detector sees that a new release is published.
- [flow:3] The model drafts the change, as a pull request. That is all the model can do.
- [flow:6] Policy code reads the plan, and sets the risk tier.
- [flow:8] A person approves.
- [flow:9] The access broker issues a grant, for one layer only.
- [flow:10] The runner applies exactly the plan that was approved.
- [flow:14] A canary check verifies the result. By then, the grant is already gone.

## approval

source: dashboard:approval
fallback: site:index.html

- [#approver-title] The approver sees one sentence: what changes, and why now.
- [.approval] Then the impact, the risk tier, the safety steps, and the permission requested.
- The model writes the words. The facts come from the plan.

## timeline

source: dashboard:timeline
fallback: site:walkthrough.html

- [#blank-slate] This is the month. A first deployment, with two approvers.
- [#feature-release] A feature release.
- [#cve] A security patch.
- (if verify.failed) [#bad-release] Then a bad release. The health check passed, but the canary failed.
- (if verify.failed) Clanker rolled it back in {rollback_seconds} seconds.
- (if incident.opened) [#incident] At two in the morning, inventory requests start to fail.
- (if agent.failed) The model writes a diagnosis, but it proposes no routine fix. So code stops, and a human decides.
- (unless agent.failed) The model writes a diagnosis. What may run is for policy code to decide.
- (if apply.refused) [#stale-plan] Later, an approved plan goes stale. The apply is refused, and the approval is asked again.

## access

source: dashboard:access
fallback: site:architecture.html

- [#grant-title] The agent has no standing write access.
- [#grant-figure] Each grant is signed, scoped to one layer, and tied to the approved plan.
- [.lanes] In {days} days, there were {grants} grants.
- All together, they lasted {held}.

## close

source: site:walkthrough.html

- [#audit] Every one of the {events} records is in a hash-chained audit log.
- Today, the local target runs end to end. The stacks for AWS, Google Cloud and Azure are validated, and not yet applied.
- [page:index.html] Site Reliability Clanker. The model proposes. Code decides. And a person approves.
