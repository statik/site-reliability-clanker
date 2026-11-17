import { compileString } from "sass";
import { describe, expect, it } from "vitest";

type Rgba = [red: number, green: number, blue: number, alpha: number];

const TEXT = 4.5;
const GRAPHIC = 3;

// Writes every colour of the palette as "red green blue alpha", with channels from 0 to 255.
const EMIT_PALETTE = `
@use "sass:color";
@use "sass:list";
@use "sass:meta";
@use "palette";

@mixin emit($name, $colour) {
  --#{$name}: #{color.channel($colour, "red", $space: rgb)}
    #{color.channel($colour, "green", $space: rgb)}
    #{color.channel($colour, "blue", $space: rgb)} #{color.alpha($colour)};
}

:root {
  @each $name, $value in meta.module-variables("palette") {
    @if meta.type-of($value) == map {
      @each $key, $entry in $value {
        @include emit("#{$name}-#{$key}", $entry);
      }
    } @else if meta.type-of($value) == list {
      @each $entry in $value {
        @include emit("#{$name}-#{list.index($value, $entry)}", $entry);
      }
    } @else {
      @include emit($name, $value);
    }
  }
}`;

function parseColour(text: string): Rgba {
  const [red, green, blue, alpha, ...rest] = text.trim().split(/\s+/).map(Number);
  if (red === undefined || green === undefined || blue === undefined || alpha === undefined ||
    rest.length > 0 || [red, green, blue, alpha].some(Number.isNaN)) {
    throw new Error(`palette.test.ts cannot read the colour "${text}"`);
  }
  return [red, green, blue, alpha];
}

function loadPalette(): Map<string, Rgba> {
  const styles = new URL(".", import.meta.url).pathname;
  const { css } = compileString(EMIT_PALETTE, { loadPaths: [styles] });
  const colours = new Map<string, Rgba>();
  for (const [, name, value] of css.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) {
    if (name !== undefined && value !== undefined) {
      colours.set(name, parseColour(value));
    }
  }
  return colours;
}

const palette = loadPalette();

function colour(name: string): Rgba {
  const found = palette.get(name);
  if (found === undefined) {
    throw new Error(`_palette.scss has no colour "${name}"`);
  }
  return found;
}

/** Puts a colour that can be translucent on an opaque ground. */
function over(top: Rgba, ground: Rgba): Rgba {
  const mix = (a: number, b: number) => a * top[3] + b * (1 - top[3]);
  return [mix(top[0], ground[0]), mix(top[1], ground[1]), mix(top[2], ground[2]), 1];
}

function linear(channel: number): number {
  const share = channel / 255;
  return share <= 0.04045 ? share / 12.92 : ((share + 0.055) / 1.055) ** 2.4;
}

function luminance([red, green, blue]: Rgba): number {
  return 0.2126 * linear(red) + 0.7152 * linear(green) + 0.0722 * linear(blue);
}

/** The WCAG 2 contrast ratio of two opaque colours. */
function contrast(a: Rgba, b: Rgba): number {
  const light = Math.max(luminance(a), luminance(b));
  const dark = Math.min(luminance(a), luminance(b));
  return (light + 0.05) / (dark + 0.05);
}

function curve(share: number): number {
  return share > 0.008856 ? Math.cbrt(share) : 7.787 * share + 16 / 116;
}

/** A colour in CIE L*a*b*, where equal distances look about equally different. */
function lab([red, green, blue]: Rgba): [number, number, number] {
  const [r, g, b] = [linear(red), linear(green), linear(blue)];
  const x = curve((0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047);
  const y = curve(0.2126 * r + 0.7152 * g + 0.0722 * b);
  const z = curve((0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883);
  return [116 * y - 16, 500 * (x - y), 200 * (y - z)];
}

/** The CIE76 colour difference. About 2 is just visible; the tags need far more. */
function difference(a: Rgba, b: Rgba): number {
  const [first, second] = [lab(a), lab(b)];
  return Math.hypot(first[0] - second[0], first[1] - second[1], first[2] - second[2]);
}

const LEAST_DIFFERENCE = 12;
// The tiers (gray, teal, blue, red) and the states use these. The three grays are one colour.
const DISTINCT_TAGS = ["red", "magenta", "purple", "blue", "cyan", "teal", "green", "gray"];
const GROUNDS = ["theme-background", "layer-01", "layer-02", "layer-03"];
const HOVER_GROUNDS = ["theme-background", "layer-01"];
const TAGS = ["red", "magenta", "purple", "blue", "cyan", "teal", "green", "gray", "cool-gray",
  "warm-gray"];
const FILLED_BUTTONS = ["primary", "primary-hover", "primary-active", "secondary",
  "secondary-hover", "secondary-active", "danger-primary", "danger-hover", "danger-active"];

type Pair = [fore: string, ground: string, least: number];

function onGrounds(fore: string, least: number): Pair[] {
  return GROUNDS.map((ground): Pair => [fore, ground, least]);
}

/** Every pair of a foreground and the ground it is drawn on, with the ratio it must reach. */
function pairs(): Pair[] {
  return [
    ...["theme-text-primary", "theme-text-secondary", "theme-text-helper", "theme-link-primary",
      "theme-link-primary-hover", "theme-link-visited", "theme-text-error",
      "button-button-tertiary", "button-button-danger-secondary"].flatMap(
      (fore) => onGrounds(fore, TEXT)),
    ...["theme-focus", "theme-interactive", "theme-icon-secondary",
      "theme-support-error", "theme-support-success", "theme-support-warning",
      "theme-support-info", "theme-support-caution-minor", "series-1", "series-2", "series-3",
      "series-4", "series-5"].flatMap((fore) => onGrounds(fore, GRAPHIC)),
    ...FILLED_BUTTONS.map((kind): Pair =>
      ["theme-text-on-color", `button-button-${kind}`, TEXT]),
    ...TAGS.map((kind): Pair => [`tag-text-${kind}`, `tag-fill-${kind}`, TEXT]),
    ["theme-border-strong-01", "theme-background", GRAPHIC],
    ["theme-border-strong-01", "layer-01", GRAPHIC],
    ["theme-border-strong-02", "layer-02", GRAPHIC],
    ["theme-border-strong-03", "layer-03", GRAPHIC],
    ["theme-text-on-color", "theme-interactive", TEXT],
    ["theme-text-on-color", "theme-support-success", TEXT],
    ["theme-text-inverse", "theme-background-inverse", TEXT],
    ["theme-text-inverse", "theme-background-inverse-hover", TEXT],
    ["theme-text-inverse", "theme-layer-selected-inverse", TEXT],
    ["theme-link-inverse", "theme-background-inverse", TEXT],
    ["theme-text-primary", "theme-highlight", TEXT],
    ["theme-text-secondary", "theme-highlight", TEXT],
    ["theme-text-primary", "theme-layer-hover-01", TEXT],
    ["theme-text-secondary", "theme-layer-hover-01", TEXT],
    ["theme-text-primary", "theme-layer-selected-hover-01", TEXT],
    ["theme-support-error-inverse", "theme-background-inverse", GRAPHIC],
    ["theme-support-info-inverse", "theme-background-inverse", GRAPHIC],
    ["theme-support-success-inverse", "theme-background-inverse", GRAPHIC],
  ];
}

describe("the colours of the theme", () => {
  it.each(pairs())("%s on %s has a contrast of %f:1 or more", (fore, ground, least) => {
    expect(contrast(colour(fore), colour(ground))).toBeGreaterThanOrEqual(least);
  });

  it.each(HOVER_GROUNDS.flatMap((ground) =>
    ["theme-background-hover", "theme-background-selected", "theme-background-selected-hover",
      "theme-background-active"].map((veil) => [veil, ground])),
  )("text stays readable on %s over %s", (veil, ground) => {
    const shown = over(colour(veil as string), colour(ground as string));
    expect(contrast(colour("theme-text-primary"), shown)).toBeGreaterThanOrEqual(TEXT);
    expect(contrast(colour("theme-text-secondary"), shown)).toBeGreaterThanOrEqual(TEXT);
  });

  it.each(DISTINCT_TAGS.flatMap((kind, index) =>
    DISTINCT_TAGS.slice(index + 1).map((other) => [kind, other])),
  )("the %s tag and the %s tag differ to the eye", (kind, other) => {
    const apart = difference(colour(`tag-fill-${kind}`), colour(`tag-fill-${other}`));
    expect(apart).toBeGreaterThanOrEqual(LEAST_DIFFERENCE);
  });
});
