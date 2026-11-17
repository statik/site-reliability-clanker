// Shared by the drawn scenes of the video. video/record.mjs sets data-beat on the root
// element when a sentence starts. This module adds the classes beat-1 ... beat-N for
// every beat up to that one, so that what a beat shows stays on the screen, and it tells
// the scene which beat started.

const root = document.documentElement;

export function onBeat(listener = () => {}) {
  let shown = 0;
  const update = () => {
    const beat = Number(root.dataset.beat ?? 0);
    while (shown < beat) {
      shown += 1;
      root.classList.add(`beat-${shown}`);
      listener(shown);
    }
  };
  new MutationObserver(update).observe(root, { attributes: true, attributeFilter: ["data-beat"] });
  update();
}
