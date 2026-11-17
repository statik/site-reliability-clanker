// Read and write 16-bit PCM WAV files. Enough for the narration and the music bed.

import { readFileSync, writeFileSync } from "node:fs";

export function readWav(path) {
  const buffer = readFileSync(path);
  if (buffer.toString("ascii", 0, 4) !== "RIFF" || buffer.toString("ascii", 8, 12) !== "WAVE") {
    throw new Error(`${path} is not a WAV file`);
  }
  let offset = 12;
  let format = null;
  while (offset + 8 <= buffer.length) {
    const id = buffer.toString("ascii", offset, offset + 4);
    const size = buffer.readUInt32LE(offset + 4);
    const start = offset + 8;
    if (id === "fmt ") {
      format = {
        channels: buffer.readUInt16LE(start + 2),
        sampleRate: buffer.readUInt32LE(start + 4),
        bits: buffer.readUInt16LE(start + 14),
      };
    } else if (id === "data") {
      if (!format || format.bits !== 16) throw new Error(`${path} is not 16-bit PCM`);
      const count = Math.floor(Math.min(size, buffer.length - start) / 2 / format.channels);
      const samples = new Float32Array(count);
      for (let index = 0; index < count; index += 1) {
        samples[index] = buffer.readInt16LE(start + index * 2 * format.channels) / 32768;
      }
      return { sampleRate: format.sampleRate, samples };
    }
    offset = start + size + (size % 2);
  }
  throw new Error(`${path} has no audio data`);
}

// `channels` is an array of Float32Array of the same length, with values from -1 to 1.
export function writeWav(path, channels, sampleRate) {
  const frames = channels[0].length;
  const dataSize = frames * channels.length * 2;
  const buffer = Buffer.alloc(44 + dataSize);
  buffer.write("RIFF", 0, "ascii");
  buffer.writeUInt32LE(36 + dataSize, 4);
  buffer.write("WAVEfmt ", 8, "ascii");
  buffer.writeUInt32LE(16, 16);
  buffer.writeUInt16LE(1, 20);
  buffer.writeUInt16LE(channels.length, 22);
  buffer.writeUInt32LE(sampleRate, 24);
  buffer.writeUInt32LE(sampleRate * channels.length * 2, 28);
  buffer.writeUInt16LE(channels.length * 2, 32);
  buffer.writeUInt16LE(16, 34);
  buffer.write("data", 36, "ascii");
  buffer.writeUInt32LE(dataSize, 40);
  let offset = 44;
  for (let frame = 0; frame < frames; frame += 1) {
    for (const channel of channels) {
      const value = Math.max(-1, Math.min(1, channel[frame]));
      buffer.writeInt16LE(Math.round(value * 32767), offset);
      offset += 2;
    }
  }
  writeFileSync(path, buffer);
}
