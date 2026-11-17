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
| `narration.md` | The script of the video: narration, captions, and what each segment shows |
| `build-video.sh` | Builds `out/explainer.mp4` and `out/explainer.srt` |
| `lib/`, `video/` | The code behind the scripts above, and the drawn scenes of the video |
| `css/`, `js/`, `fonts/`, `img/` | The assets of the site. `css/site.css` defines the colours. `img/icon.svg` and `img/icon-small.svg` are the icon |
| `out/` | Build output. Git ignores it |

## Set up

You need Node 22 or later and pnpm. The video build also needs ffmpeg and the macOS `say`
command.

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
node capture.mjs           # screenshots into screens/
node capture.mjs --video   # also record click-through videos into out/segments/
```

| Target | Address | Views |
|---|---|---|
| Dashboard | `http://127.0.0.1:7070` | Overview, Timeline, an approval page, Topology, Access, Audit, each in the light and the dark theme |
| Paperboat | `http://localhost:8088` | Inventory, Rentals, Deliveries, Support desk |
| Mail inbox | `http://127.0.0.1:8025` | The inbox and one approval email |
| Webhook sink | `http://127.0.0.1:7071` | The page |

A target that is not up is skipped with a message. `screens/manifest.json` lists what was
skipped, and the Live screens page says so. Run the script again when the target is up.

The script blocks every request that is not a `GET`, so it cannot approve, run or change
anything in the control plane. One side effect remains: the mail inbox marks the one
email that the script opens as read.

The work items in the pictures are found through the API (the one that waits for
approval, the one that was rolled back), not by a fixed number. The webhook sink keeps
its payloads in memory, so after a restart it is empty; the script then keeps the
earlier picture of it.

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
is not 120 to 180 seconds long.

| Step | Script | What it does |
|---|---|---|
| 1 | `build.mjs` | Makes the pages from the data |
| 2 | `video/narrate.mjs` | Speaks each sentence of `narration.md` with `say`, measures it, and writes the timeline, the narration track and the `.srt` file |
| 2 | `video/music.mjs` | Synthesizes the music bed from the notes in `js/score.mjs` |
| 3 | `video/record.mjs` | Playwright records each segment for as long as its narration, and draws the caption pictures |
| 4 | `video/assemble.mjs` | ffmpeg cuts and joins the segments, puts a frame around the recordings of the running applications, above the band of the captions, lays the captions over them, and mixes the voice over the music |

Things to know:

- To change the video, edit `narration.md`. The top of that file explains the format.
- A segment with `source: dashboard:...` shows the real dashboard when it is up at
  `http://127.0.0.1:7070`. If it is not up, the segment shows its `fallback`.
- Three segments are drawn scenes, not recordings: `video/opening.html` (the layers of the
  platform on each target), `video/setup.html` (from the download to a product that is
  looked after) and `video/pipeline.html` (one change through its checkpoints). The
  narration moves them with `[beat:N]` cues. Their numbers come from `data/month.json`.
  The step "Sign in to your cloud" of the setup scene is a drawing and says so, because
  the wizard does not set up a cloud yet. `video/wizard/` holds the two real screenshots
  of the setup wizard that the scene shows.
- `VIDEO_FOOTAGE=<directory> ./build-video.sh` builds the video while the demo is not
  available. The build then does not contact the running applications. It takes the
  picture of each `tour:` and `dashboard:` segment from `<directory>/<segment>.mp4`, a clip
  that you kept from an earlier recording, and stops if a clip is not as long as its segment.
- Playwright records a 1440 x 810 window, so that the text is large enough to read, and
  ffmpeg scales it to 1920 x 1080.
- A sentence in `narration.md` can depend on the data, with `(if event.type)`. A new run
  of the month that has no stale plan, for example, gets a video without that sentence.
- The build deletes its large intermediate files when it succeeds.
- `NARRATION_VOICE=Samantha ./build-video.sh` uses another voice.
- To use a voice from ElevenLabs, put `ELEVEN_LABS_API_KEY` and `ELEVEN_LABS_VOICE_ID` in
  the `.env` file of the repository. The build then sends each sentence of `narration.md`
  to ElevenLabs and keeps the answer in `out/cache/speech/`, so only a changed sentence is
  sent again.
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
`--cream`, `--dark` and the others). Every other colour of the site and of the drawn
scenes is one of them or a mix of two. The site has one theme, a warm dark ground. The recordings of the dashboard and of Paperboat keep their own colours.

## The figure of the system

Figure 1 of the architecture page, and the scene `video/pipeline.html`, show S.R.C. as a
bench with real parts. `lib/machine.mjs` draws it with the Anatomy kit, an isometric SVG
kit under the MIT licence. Its core file is in `lib/anatomy/` with the licence, and its
styles are `css/anatomy.css`; the site has no other copy and installs nothing for it.
`lib/machine.mjs` also works out where each moving part is at each of the ten stops of a
change, and `js/machine.js` eases between those places.

`img/icon.svg` is the icon: an aircraft marshaller with two wands. The video shows it.
`img/icon-small.svg` is the same mark with fewer and thicker shapes, for the favicon and
the header, where it is 16 to 32 pixels.
