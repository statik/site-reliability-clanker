# Narration for the explainer video

This file is the script of `out/explainer.mp4`. The video build reads it and makes the
narration, the captions and the timing from it. To change the video, change this file, then
run `./build-video.sh`.

The video has one idea: ship an AI site reliability engineer with your product instead of
an installer. Every picture is drawn by `video/explainer.html`, from `data/month.json`.

Format:

- Each `##` heading starts a part of the video. The heading is the name of the part.
- Each list item is one spoken sentence and one caption. It starts with a cue in square
  brackets. The cue names the picture that `video/explainer.html` shows while the voice
  says the sentence.
- `pause:` is the number of seconds of quiet after each sentence of the part. `hold:` is
  the number of seconds that the last picture of the part stays after its last sentence.
  The pauses set the pace of the video. The voice is not made slower. Each of the three
  steps is its own part, so that its `hold:` gives time after the step.
- `{days}` and `{held}` are filled in from `data/month.json`, so the narration cannot
  disagree with the data.
- A caption has at most two lines of 42 characters and stays for at least one second. The
  build stops if a caption does not fit. In a sentence that is too long for one caption, a
  `|` shows where the next caption starts. The voice still says it as one sentence, and
  the `|` is not sent to the voice, so you can move it at no cost. A `~` between two words
  is a space where a caption line does not break; the voice gets a plain space.
- Each sentence is sent to ElevenLabs one time and then kept in `out/cache/speech/`. A
  sentence with a changed word is sent again.

## problem

pause: 0.8

- [sell] You sell software that your customers run themselves.
- [build] So you build an installer. Then upgrades. Then patches.
- [late] Every customer runs them a little differently, and a little late.
- [incidents] Late patches turn into outages and security incidents.

## solution

pause: 0.9
hold: 1.5

- [dont] Don't build an installer.
- [ship] Ship an AI site reliability engineer with your product.
- [does] It installs, upgrades, patches, and monitors your product.

## setup

pause: 0.9
hold: 1.4

- [download] Setup is a single download, | which walks you through logging~in to your cloud provider | and sets up the control plane.
- [deploy] The control plane then deploys your product, layer by layer.

## step-one

pause: 1.6
hold: 2.2

- [find] One. It finds the needed maintenance | without someone having to manually ask for it: | a new release, a security fix, | or drift caused by untracked manual changes.
- [write] Each maintenance step is a version-controlled | Infrastructure as Code changeset.
## step-two

pause: 1.6
hold: 2.2

- [rate] Two. Code rates the risk of the change. The model has no say in that.
- [approve] A person reads a plain summary of what will change, and approves.
## step-three

pause: 1.6
hold: 2.6

- [grant] Three. Permissions are narrowly scoped, and issued only for that approved change.
- [revoke] They are revoked as soon as the job ends.
- [verify] Then it checks the result, and rolls back if the check fails.

## payoff

pause: 1.6
hold: 1.4

- [demo] We ran it on a demo app with a realistic architecture, to model the workflows.
- [held] In {days} days, it held access for {held} in total.
- [audit] Every step is in an audit log.

## next

hold: 4

- [try] Try it on your laptop, with one command.
