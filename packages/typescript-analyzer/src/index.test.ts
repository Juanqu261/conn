import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { analyze, MAX_CLAIMS, MAX_DIAGNOSTICS } from "./index.js";

describe("analyze", () => {
  it("uses the TypeScript compiler API for exports and resolved imports without source text", () => {
    const worktree = mkdtempSync(join(tmpdir(), "conn-ts-analyzer-"));
    writeFileSync(
      join(worktree, "tsconfig.json"),
      JSON.stringify({
        compilerOptions: { module: "NodeNext", moduleResolution: "NodeNext" },
        include: ["*.ts"],
      }),
    );
    writeFileSync(
      join(worktree, "src.ts"),
      'export const charge = "ghp_this_is_source_and_must_not_escape";\n',
    );
    writeFileSync(
      join(worktree, "consumer.ts"),
      'import { charge } from "./src.js"; export { charge };\n',
    );

    const report = analyze({ worktree, canonical_remote: "github.com/acme/payments" });
    const serialized = JSON.stringify(report);

    expect(report.claims).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          normalized_subject: "symbol:typescript:src.ts/charge",
          relation: "affects",
        }),
        expect.objectContaining({
          normalized_subject: "contract:typescript:src.ts/charge",
          relation: "produces",
        }),
        expect.objectContaining({
          normalized_subject: "contract:typescript:src.ts/charge",
          relation: "consumes",
        }),
      ]),
    );
    expect(serialized).not.toContain("ghp_this_is_source_and_must_not_escape");
    expect(report.claims.length).toBeLessThanOrEqual(MAX_CLAIMS);
    expect(report.diagnostics.length).toBeLessThanOrEqual(MAX_DIAGNOSTICS);
  });

  it("replaces compiler diagnostic text with a bounded code-only message", () => {
    const worktree = mkdtempSync(join(tmpdir(), "conn-ts-analyzer-"));
    writeFileSync(join(worktree, "tsconfig.json"), JSON.stringify({ include: ["broken.ts"] }));
    writeFileSync(
      join(worktree, "broken.ts"),
      'const message: number = "PRIVATE_SOURCE_LITERAL";\n',
    );

    const report = analyze({ worktree, canonical_remote: "github.com/acme/payments" });

    expect(report.diagnostics).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          message: expect.stringMatching(/^TypeScript diagnostic TS\d+$/),
        }),
      ]),
    );
    expect(JSON.stringify(report)).not.toContain("PRIVATE_SOURCE_LITERAL");
  });
});
