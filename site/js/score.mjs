// The score of the generated soundtrack: a slow, calm loop in D major. The browser
// (js/soundtrack.js, Web Audio) and the video build (video/music.mjs, a small offline
// synthesizer) both play these notes, so the site and the video share one piece of music.
// Nothing is downloaded and nothing is sampled.

export const BPM = 66;
export const BEATS_PER_BAR = 4;
export const BEAT_SECONDS = 60 / BPM;

// Dmaj9, Bm7, Gmaj7, Aadd9: one bar each.
export const CHORDS = [
  { root: 38, tones: [57, 61, 64, 66] },
  { root: 35, tones: [54, 57, 62, 66] },
  { root: 43, tones: [55, 59, 62, 66] },
  { root: 45, tones: [57, 59, 61, 64] },
];

// Chord tone for each eighth note of a bar, one octave up. null is a rest.
const PLUCK_PATTERNS = [
  [0, null, 2, null, 3, null, 1, null],
  [null, 2, null, 3, null, null, 1, null],
  [0, null, null, 2, null, 3, null, 2],
  [null, 1, null, 2, null, 3, null, null],
];

export function frequency(midi) {
  return 440 * 2 ** ((midi - 69) / 12);
}

// The notes of one bar: { voice, midi, beat, beats, level }. `beat` counts from the start
// of the bar. The plucked melody rests for the first two bars of the piece.
export function notesForBar(bar) {
  const chord = CHORDS[bar % CHORDS.length];
  const notes = [];
  for (const midi of chord.tones) {
    notes.push({ voice: "pad", midi, beat: 0, beats: BEATS_PER_BAR, level: 0.2 });
  }
  notes.push({ voice: "bass", midi: chord.root, beat: 0, beats: 3, level: 0.5 });
  notes.push({ voice: "bass", midi: chord.root + 7, beat: 3, beats: 1, level: 0.28 });
  if (bar < 2) return notes;
  const pattern = PLUCK_PATTERNS[(bar + Math.floor(bar / 4)) % PLUCK_PATTERNS.length];
  pattern.forEach((tone, index) => {
    if (tone === null) return;
    notes.push({ voice: "pluck", midi: chord.tones[tone] + 12, beat: index / 2, beats: 0.5, level: 0.3 });
  });
  return notes;
}
