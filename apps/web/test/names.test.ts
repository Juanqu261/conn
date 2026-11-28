import { describe, expect, it } from "vitest";
import { projectIdFromSlug, projectSlug, summaryLine } from "../src/names/directory";

// Protects stable, shareable Lab URLs (requirement 3.6) and the Intent name
// rule shared with conn app (requirement 5.1).

describe("Project URL segments", () => {
  const id = "019a8f00-0001-7000-8000-000000000001";

  it.each([
    ["Checkout reliability", `checkout-reliability~${id}`],
    ["Café — Ünïcode & spaces!", `cafe-unicode-spaces~${id}`],
    ["***", id],
  ])("%s becomes %s and resolves back to the ID", (name, slug) => {
    expect(projectSlug(id, name)).toBe(slug);
    expect(projectIdFromSlug(slug)).toBe(id);
  });

  it("ignores a renamed prefix", () => {
    expect(projectIdFromSlug(`old-name~${id}`)).toBe(id);
  });
});

describe("Intent summary lines", () => {
  it.each([
    ["# Retry failed payments\n\nmore", "Retry failed payments"],
    ["\n\n- **Refund** flow", "Refund flow"],
    ["1. `Fix` the gateway", "Fix the gateway"],
    ["   \n", "Unknown Intent"],
    ["x".repeat(130), `${"x".repeat(119)}…`],
  ])("%j reads as %j", (markdown, line) => {
    expect(summaryLine(markdown)).toBe(line);
  });
});
