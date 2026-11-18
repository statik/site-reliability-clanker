// Prerender the gallery of real screenshots from screens/manifest.json, which
// capture.mjs writes.

import { html } from "./html.mjs";

const GROUPS = [
  ["dashboard", "The S.R.C. dashboard", "Six views of the dashboard that the control plane serves, as an approver sees them."],
  ["paperboat", "Paperboat, the managed application", "The fish rental business that S.R.C. deploys and looks after."],
  ["notifications", "Where the notifications arrive", "The mail inbox and the webhook sink of the demo, with the messages that the control plane sent."],
];

function figure(shot) {
  return html`<figure class="shot">
    <a href="screens/${shot.file}"><img src="screens/${shot.file}" alt="${shot.alt}"
      width="${shot.width}" height="${shot.height}" loading="lazy"></a>
    <figcaption><strong>${shot.title}</strong>
      <span class="shot-note">${shot.note ?? ""}</span></figcaption>
  </figure>`;
}

export function renderGallery(manifest) {
  const sections = GROUPS.map(([group, title, text]) => {
    const shots = manifest.shots.filter((shot) => shot.group === group);
    const missing = (manifest.skipped ?? []).filter((item) => item.group === group);
    return html`<section class="gallery-group" aria-labelledby="group-${group}">
      <h2 id="group-${group}">${title}</h2>
      <p class="section-lead">${text}</p>
      ${shots.length > 0 ? html`<div class="gallery">${shots.map(figure)}</div>` : ""}
      ${missing.length > 0 ? html`<div class="notice" role="note">
        <p><strong>Not captured yet.</strong> ${missing[0].reason}</p>
        <p>These views are waiting: ${missing.map((item) => item.title).join(", ")}. Run
          <code>node capture.mjs</code> again when the dashboard is up, and they appear here.</p>
      </div>` : ""}
    </section>`;
  });
  const when = manifest.captured_at
    ? html`<p class="figure-source">Captured on ${manifest.captured_at.slice(0, 10)} with
        Playwright. Nothing on this page is a mock-up.</p>`
    : html`<div class="notice" role="note"><p>No screenshots are captured yet. Run
        <code>node capture.mjs</code>.</p></div>`;
  return html`${when}${sections}`;
}
