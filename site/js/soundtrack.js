// An optional soundtrack for the walkthrough, synthesized in the browser with the Web
// Audio API from the notes in score.mjs: a soft pad, a low bass, a plucked melody with an
// echo, over quiet filtered noise that sounds like water. It starts only when the reader
// presses the button.

import { BEATS_PER_BAR, BEAT_SECONDS, frequency, notesForBar } from "./score.mjs";

const LOOKAHEAD_SECONDS = 0.2;
const MASTER_LEVEL = 0.5;
const BAR_SECONDS = BEATS_PER_BAR * BEAT_SECONDS;

class Soundtrack {
  constructor() {
    this.context = new AudioContext();
    this.master = this.context.createGain();
    this.master.gain.value = 0;
    this.master.connect(this.context.destination);

    this.echo = this.context.createDelay(2);
    this.echo.delayTime.value = BEAT_SECONDS * 0.75;
    const feedback = this.context.createGain();
    feedback.gain.value = 0.35;
    this.echo.connect(feedback).connect(this.echo);
    this.echo.connect(this.master);

    this.startWater();
    this.bar = 0;
    this.nextBarTime = 0;
    this.timer = null;
  }

  startWater() {
    const length = this.context.sampleRate * 2;
    const buffer = this.context.createBuffer(1, length, this.context.sampleRate);
    const data = buffer.getChannelData(0);
    for (let index = 0; index < length; index += 1) data[index] = Math.random() * 2 - 1;
    const source = this.context.createBufferSource();
    source.buffer = buffer;
    source.loop = true;
    const filter = this.context.createBiquadFilter();
    filter.type = "bandpass";
    filter.frequency.value = 700;
    filter.Q.value = 0.6;
    const level = this.context.createGain();
    level.gain.value = 0.035;
    const swell = this.context.createOscillator();
    swell.frequency.value = 0.11;
    const depth = this.context.createGain();
    depth.gain.value = 0.02;
    swell.connect(depth).connect(level.gain);
    source.connect(filter).connect(level).connect(this.master);
    source.start();
    swell.start();
  }

  play(note, barTime) {
    const start = barTime + note.beat * BEAT_SECONDS;
    const length = note.beats * BEAT_SECONDS;
    const shape = {
      pad: { type: "triangle", attack: 0.9, release: 1.4, cutoff: 1400, echo: 0 },
      bass: { type: "sine", attack: 0.04, release: 0.5, cutoff: 500, echo: 0 },
      pluck: { type: "sine", attack: 0.005, release: 0.9, cutoff: 4000, echo: 0.5 },
    }[note.voice];
    const oscillator = this.context.createOscillator();
    oscillator.type = shape.type;
    oscillator.frequency.value = frequency(note.midi);
    const filter = this.context.createBiquadFilter();
    filter.type = "lowpass";
    filter.frequency.value = shape.cutoff;
    const gain = this.context.createGain();
    const hold = note.voice === "pluck" ? 0 : Math.max(0, length - shape.attack);
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.linearRampToValueAtTime(note.level * 0.4, start + shape.attack);
    gain.gain.setValueAtTime(note.level * 0.4, start + shape.attack + hold);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + shape.attack + hold + shape.release);
    oscillator.connect(filter).connect(gain).connect(this.master);
    if (shape.echo > 0) {
      const send = this.context.createGain();
      send.gain.value = shape.echo;
      gain.connect(send).connect(this.echo);
    }
    oscillator.start(start);
    oscillator.stop(start + shape.attack + hold + shape.release + 0.1);
  }

  tick() {
    while (this.nextBarTime < this.context.currentTime + LOOKAHEAD_SECONDS + BAR_SECONDS) {
      for (const note of notesForBar(this.bar)) this.play(note, this.nextBarTime);
      this.nextBarTime += BAR_SECONDS;
      this.bar += 1;
    }
  }

  async start() {
    await this.context.resume();
    const now = this.context.currentTime;
    this.master.gain.cancelScheduledValues(now);
    this.master.gain.setValueAtTime(this.master.gain.value, now);
    this.master.gain.linearRampToValueAtTime(MASTER_LEVEL, now + 2);
    this.nextBarTime = now + 0.1;
    this.timer = window.setInterval(() => this.tick(), 100);
    this.tick();
  }

  async stop() {
    const now = this.context.currentTime;
    this.master.gain.cancelScheduledValues(now);
    this.master.gain.setValueAtTime(this.master.gain.value, now);
    this.master.gain.linearRampToValueAtTime(0, now + 0.8);
    window.clearInterval(this.timer);
    this.timer = null;
    await new Promise((resolve) => window.setTimeout(resolve, 900));
    if (this.timer === null) await this.context.suspend();
  }
}

function init() {
  const button = document.querySelector(".sound-toggle");
  if (!button) return;
  if (!window.AudioContext) {
    button.hidden = true;
    return;
  }
  let soundtrack = null;
  let playing = false;
  button.addEventListener("click", async () => {
    soundtrack ??= new Soundtrack();
    playing = !playing;
    button.textContent = playing ? "Stop soundtrack" : "Play soundtrack";
    button.setAttribute("aria-pressed", String(playing));
    if (playing) await soundtrack.start();
    else await soundtrack.stop();
  });
}

init();
