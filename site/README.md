# Concept site, screenshots and explainer video

This directory holds the public concept site for Site Reliability Clanker, the scripts
that take real screenshots of the running demo, and the build of the explainer video.
The scripts run on your machine. The workflow `.github/workflows/pages.yaml` publishes the
site to GitHub Pages (see "Publish the site").

## What is here

| Path | What it is |
|---|---|
| `index.html`, `walkthrough.html`, `architecture.html`, `screens.html` | The four pages. Plain HTML, CSS and ES modules, no framework |
| `data/month.json` | A snapshot of the scripted month from the control plane. The pages are made from it |
| `export-data.mjs` | Writes `data/month.json` from a running control plane |
| `build.mjs` | Writes the data-driven parts of the pages from `data/month.json` and `screens/manifest.json` |
| `capture.mjs` | Takes the screenshots in `screens/` with Playwright |
| `check.mjs` | Opens every page in a browser and fails on the problems listed below |
| `narration.md` | The script of the video: the sentences, their captions, and the cue of each picture |
| `build-video.sh` | Builds `out/explainer.mp4` and `out/explainer.srt` |
| `lib/`, `video/` | The code behind the scripts above, and the drawn pictures of the video |
| `css/`, `js/`, `fonts/`, `img/` | The assets of the site. `css/site.css` defines the colours. `img/icon.svg` and `img/icon-small.svg` are the icon |
| `out/` | Build output. Git ignores it |

## Set up

You need Node 22 or later and pnpm. The video build also needs ffmpeg and the two
ElevenLabs settings (see "Build the video").

```
cd site
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
```

The only dependency is Playwright, at an exact version. To view the site you need no
dependency at all.

## Preview the site

```
cd site
python3 -m http.server 4173
```

Open <http://127.0.0.1:4173/>. Do not use the ports 5001, 7070, 7071, 8025 or 8088: the
demo uses them.

- On the walkthrough, the right arrow or Space goes to the next step and the left arrow
  goes back. Home and End go to the first and the last step.
- The text is in the HTML, so the pages work without JavaScript.
- With "reduce motion" set in the system, nothing moves by itself.
- The pages make no request to another host. The IBM Plex fonts are in `fonts/`.

## Export the data

The control plane must be running at `http://127.0.0.1:7070` with the scripted month.

```
node export-data.mjs
```

The script sends only `GET` requests. It writes `data/month.json`, then runs `build.mjs`.
It removes the grant signatures. It stops with an error, and writes nothing, if the data
contains something that looks like a secret, a home directory path, the name of this
machine, or an email address outside `example.com`.

Use `--api <base url>` for another address and `--no-build` to skip the page build.

The steps of the walkthrough follow the `sim.scene_started` events in the data. The
headlines and the sentence frames are in `lib/story.mjs`. All names, times, tiers and
numbers come from the data.

## Build the pages

```
node build.mjs           # write the pages
node build.mjs --check   # exit 1 if a page is not up to date with the data
```

The build replaces the text between `<!-- build:NAME -->` and `<!-- /build:NAME -->`
in each page. Edit the text outside those markers by hand. Do not edit the text inside
them: change `lib/` and run the build.

## Capture the screenshots

```
node capture.mjs
```

| Target | Address | Views |
|---|---|---|
| Dashboard | `http://127.0.0.1:7070` | Overview, Timeline, the page of one work item, Topology, Access, Audit |
| Paperboat | `http://localhost:8088` | Inventory, Rentals, Deliveries, Support desk |
| Mail inbox | `http://127.0.0.1:8025` | The inbox and one approval email |
| Webhook sink | `http://127.0.0.1:7071` | The page |

A target that is not up is skipped with a message. `screens/manifest.json` lists what was
skipped, and the Live screens page says so. Run the script again when the target is up.

The dashboard has one dark look, so there is one picture of each view. Paperboat, the mail
inbox and the webhook sink keep their own looks.

The script blocks every request that is not a `GET`, so it cannot approve, run or change
anything in the control plane. One side effect remains: the mail inbox marks the one
email that the script opens as read.

The work item in the picture is found through the API, not by a fixed number: a change
that waits for approval, or, if none waits, the newest change with a summary. The caption
of the picture says which of the two it shows. The webhook sink keeps its payloads in
memory, so after a restart it is empty; the script then keeps the earlier picture of it,
and the caption says so.

## Check the site

```
node check.mjs
```

It opens the four pages at 1440 and 390 pixels wide, and writes full-page
screenshots to `out/check/`. It fails on a request to another host, a console error, a
broken image, or a page that scrolls sideways. It also checks that the text is there
without JavaScript, that the keys move through the walkthrough, and that nothing plays
by itself with reduced motion.

## Build the video

```
./build-video.sh
```

The result is `out/explainer.mp4` (1920 x 1080, H.264, AAC, captions burned in) and
`out/explainer.srt` (the same captions as a sidecar file). The script fails if the video
is not 60 to 110 seconds long.

The video has one idea: ship an AI site reliability engineer with your product instead of
an installer. It tells of the problem, the solution, the first setup, how a change goes
through in three steps, the numbers of the scripted month, and the command to try it.
The build needs no running demo. Every picture is drawn from `data/month.json`.

| Step | Script | What it does |
|---|---|---|
| 1 | `video/narrate.mjs` | Checks the captions, gets each sentence of `narration.md` from the ElevenLabs voice, measures it, and writes the timeline, the narration track and the `.srt` file |
| 1 | `video/music.mjs` | Synthesizes the music bed from the notes in `js/score.mjs` |
| 2 | `video/record.mjs` | Playwright records `video/explainer.html` for each part, for as long as its narration, and draws the caption pictures |
| 3 | `video/assemble.mjs` | ffmpeg cuts and joins the parts, lays the captions over them, and mixes the voice over a quiet, steady music bed |

Things to know:

- To change the video, edit `narration.md`. The top of that file explains the format.
- The voice is the ElevenLabs voice of the owner. Put `ELEVEN_LABS_API_KEY` and
  `ELEVEN_LABS_VOICE_ID` in the `.env` file at the root of the repository, or in the
  environment. Without them the build stops. There is no other voice.
- The build sends each sentence of `narration.md` and the name of the model to ElevenLabs,
  and nothing else. It keeps the answer in `out/cache/speech/`, so a sentence is sent one
  time. A sentence with a changed word is sent again, and that costs credits.
- A caption has at most two lines of 42 characters and stays for at least one second.
  `node video/narrate.mjs --check` tests the text without the voice, and the build stops
  on a caption that does not fit. A `|` in a longer sentence starts another caption. The
  duration in the sentence about the held access stays on one caption line.
- The pace comes from the pauses. `pause:` and `hold:` in `narration.md` set the quiet
  after the sentences of a part. `narrate.mjs` prints the words a second of the whole
  video and of the voice alone. The voice itself is not made slower.
- Each sentence in `narration.md` has a cue, for example `[approve]`. `video/explainer.html`
  has one picture for each cue or group of cues (`data-cues`), and `video/scene.css` gives
  every picture the same way in and out. A part of a picture with `data-at` appears when
  its cue starts; `--at` is the share of the sentence that passes first, so a change of
  the words does not need a change of the picture.
- The pictures of the three steps follow one change of the month: the patch of the
  database, which the model wrote and which is done. Its pull request, tier, summary for
  the approver, approver and grant are on the screen. The summaries of the application
  upgrades name the demo application, which the video leaves out.
- The sound: `narrate.mjs` brings the voice track to a peak of -3 dB with one gain.
  `assemble.mjs` lays the music under it at one steady level, about 15 dB under the voice,
  with no compressor. A limiter is there only as a safety net and adds no gain. The music
  has no noise in it. The build keeps `out/build/narration.wav` (the voice alone) and
  `out/build/music.wav` (the bed alone).
- The end card shows `make demo`, the command of "Run the local demo" in the README of
  the repository, and the address of the site.
- Playwright records a 1440 x 810 window, so that the text is large enough to read, and
  ffmpeg scales it to 1920 x 1080.
- The build deletes its large intermediate files when it succeeds.
- The ffmpeg from Homebrew has no subtitle filter, so the build burns in the captions as
  pictures, drawn in the site's typeface.

## Publish the site

A push to `main` that changes `site/` starts the workflow `Pages`. It copies the pages,
`css`, `js`, `fonts`, `img` and `screens` to GitHub Pages. The video is too large for the
repository, so the workflow gets it from the release `site-video`. After you build a new
video, replace it and publish again:

```
gh release upload site-video out/explainer.mp4 --clobber
gh workflow run Pages
```

## Fonts

`fonts/` holds four IBM Plex files (Sans Light, Regular and SemiBold; Mono Regular) from
the npm packages `@ibm/plex-sans` 1.1.0 and `@ibm/plex-mono` 1.1.0, under the SIL Open
Font License (`fonts/LICENSE.txt`). The spacing and type follow the IBM Carbon Design
System, written by hand in `css/site.css`.

## Colours and icon

The palette is eleven colours, defined once at the top of `css/site.css` (`--honey`,
`--cream`, `--dark` and the others). Every other colour of the site and of the pictures
of the video is one of them or a mix of two. The site has one theme, a warm dark ground.

## The figure of the system

Figure 1 of the architecture page shows S.R.C. as a bench with real parts. The video does
not show it. `lib/machine.mjs` draws it with the Anatomy kit, an isometric SVG
kit under the MIT licence. Its core file is in `lib/anatomy/` with the licence, and its
styles are `css/anatomy.css`; the site has no other copy and installs nothing for it.
`lib/machine.mjs` also works out where each moving part is at each of the ten stops of a
change, and `js/machine.js` eases between those places.

`img/icon.svg` is the icon: an aircraft marshaller with two wands. The video shows it
beside the name.
`img/icon-small.svg` is the same mark with fewer and thicker shapes, for the favicon and
the header, where it is 16 to 32 pixels.
