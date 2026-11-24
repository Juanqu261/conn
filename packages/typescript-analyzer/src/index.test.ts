import { mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
// Test-only: validate reports with the same canonical schemas the Go CLI uses.
import { createRegistry } from "../../contracts/src/index.js";
import { analyze, MAX_CLAIMS, MAX_DIAGNOSTICS } from "./index.js";

const contractsDir = fileURLToPath(new URL("../../../contracts/v1/", import.meta.url));
const registry = createRegistry(
  readdirSync(contractsDir)
    .filter((file) => file.endsWith(".schema.json"))
    .map((file) => JSON.parse(readFileSync(join(contractsDir, file), "utf8")) as object),
);
const expectValidReport = (report: unknown): void =>
  expect(registry.validate("urn:conn:schema:v1:local-analyzer-report", report)).toEqual({
    valid: true,
  });

function worktreeWith(files: Record<string, string>): string {
  const worktree = mkdtempSync(join(tmpdir(), "conn-ts-analyzer-"));
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(join(worktree, path, ".."), { recursive: true });
    writeFileSync(join(worktree, path), content);
  }
  return worktree;
}

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

  it("analyzes the two-intent fixture locally without serializing its source literal", () => {
    const worktree = fileURLToPath(
      new URL("../../../fixtures/semantic-intent-radar-two-intents/", import.meta.url),
    );

    const report = analyze({
      worktree,
      canonical_remote: "github.com/conn-fixtures/semantic-intent-radar",
    });
    const serialized = JSON.stringify(report);

    expect(report.diagnostics).toEqual([]);
    expect(report.claims).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          normalized_subject: "symbol:typescript:apps/gateway/src/retry.ts/shouldRetryCharge",
        }),
        expect.objectContaining({
          normalized_subject: "symbol:typescript:apps/orders/src/retry.ts/shouldRetryOrder",
        }),
      ]),
    );
    expect(serialized).not.toContain("FIXTURE_SOURCE_MUST_NOT_BE_TRANSMITTED");
    expect(serialized).not.toContain(
      "Retry transport failures without creating more than one charge",
    );
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

  it("maps path characters outside the subject alphabet instead of failing the report", () => {
    const worktree = worktreeWith({
      "tsconfig.json": JSON.stringify({
        compilerOptions: { module: "NodeNext", moduleResolution: "NodeNext" },
        include: ["**/*.ts"],
      }),
      "app/[slug]/(group) page.ts": "export function page(): number { return 1; }\n",
      "app/@team/consumer.ts":
        'import { page } from "../[slug]/(group) page.js"; export const value = page();\n',
    });

    const report = analyze({ worktree, canonical_remote: "github.com/acme/web" });

    expectValidReport(report);
    expect(report.claims).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          normalized_subject: "symbol:typescript:app/_slug_/_group__page.ts/page",
          evidence_refs: [expect.objectContaining({ path: "app/[slug]/(group) page.ts" })],
        }),
        expect.objectContaining({
          normalized_subject: "contract:typescript:app/_slug_/_group__page.ts/page",
          relation: "consumes",
        }),
      ]),
    );
  });

  it("ignores external libraries and never cites declarations outside the worktree", () => {
    const root = worktreeWith({
      "outside.ts": "export const hidden = 1;\n",
      "pkg/tsconfig.json": JSON.stringify({
        compilerOptions: { module: "NodeNext", moduleResolution: "NodeNext", rootDir: ".." },
        include: ["src/**/*.ts"],
      }),
      "pkg/node_modules/@scope/lib/package.json": JSON.stringify({
        name: "@scope/lib",
        type: "module",
        exports: { ".": "./index.ts" },
      }),
      "pkg/node_modules/@scope/lib/index.ts": "export const fromLibrary = 1;\n",
      "pkg/src/index.ts":
        'export * from "../../outside.js";\nimport { fromLibrary } from "@scope/lib";\nexport const own = fromLibrary;\n',
    });
    const worktree = join(root, "pkg");

    const report = analyze({ worktree, canonical_remote: "github.com/acme/web" });
    const serialized = JSON.stringify(report);

    expectValidReport(report);
    expect(report.claims).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ normalized_subject: "symbol:typescript:src/index.ts/hidden" }),
        expect.objectContaining({ normalized_subject: "symbol:typescript:src/index.ts/own" }),
      ]),
    );
    expect(serialized).not.toContain("outside.ts");
    expect(serialized).not.toContain("node_modules");
  });

  it("produces a schema-valid report for this pnpm workspace", () => {
    const worktree = fileURLToPath(new URL("../../../", import.meta.url));

    const report = analyze({
      worktree,
      canonical_remote: "github.com/conn/conn",
      tsconfig_path: "packages/typescript-analyzer/tsconfig.json",
      paths: ["packages/typescript-analyzer/src/index.ts"],
    });

    expectValidReport(report);
    expect(JSON.stringify(report)).not.toContain("node_modules");
  });
});
