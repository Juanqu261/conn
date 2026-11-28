import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { contrastRatio, readTokens } from "../src/theme/contrast";

// Protects requirement 4.3 of web-lab-foundation: every text and essential
// graphic pairing the Lab uses meets WCAG 2.2 AA in both themes. Text needs
// 4.5:1; stitches and other essential graphics need 3:1.

const css = readFileSync(join(import.meta.dirname, "..", "src", "theme", "tokens.css"), "utf8");
const themes = {
  "lights out (dark)": readTokens(css, ":root"),
  "lights on (light)": readTokens(css, ':root[data-theme="light"]'),
};

const TEXT = 4.5;
const GRAPHIC = 3;
const pairs: Array<[foreground: string, background: string, minimum: number]> = [
  ["ink", "crypt-bg", TEXT],
  ["ink", "crypt-surface", TEXT],
  ["ink", "crypt-raised", TEXT],
  ["ink-muted", "crypt-bg", TEXT],
  ["ink-muted", "crypt-surface", TEXT],
  ["ink-muted", "crypt-raised", TEXT],
  ["on-formal", "formal-700", TEXT],
  ["formal-ink", "formal-700", TEXT],
  ["on-voltage", "voltage", TEXT],
  ["voltage-ink", "crypt-bg", TEXT],
  ["voltage-ink", "crypt-surface", TEXT],
  ["voltage-ink", "crypt-raised", TEXT],
  ["pumpkin", "crypt-raised", TEXT],
  ["pumpkin", "crypt-surface", TEXT],
  ["bruise", "crypt-raised", TEXT],
  ["bruise", "crypt-surface", TEXT],
  ["blood", "crypt-raised", TEXT],
  ["blood", "crypt-surface", TEXT],
  ["alive", "crypt-raised", TEXT],
  ["alive", "crypt-surface", TEXT],
  ["stitch", "crypt-bg", GRAPHIC],
  ["stitch", "crypt-surface", GRAPHIC],
  ["stitch", "crypt-raised", GRAPHIC],
];

describe.each(Object.entries(themes))("%s palette", (_, tokens) => {
  it.each(pairs)("%s on %s meets %d:1", (foreground, background, minimum) => {
    const fg = tokens[foreground];
    const bg = tokens[background];
    expect(fg, `--${foreground} is defined`).toBeDefined();
    expect(bg, `--${background} is defined`).toBeDefined();
    expect(contrastRatio(fg ?? "", bg ?? "")).toBeGreaterThanOrEqual(minimum);
  });
});
