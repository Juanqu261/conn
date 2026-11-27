import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
// Test-only: validate reports with the same canonical schemas the Go CLI uses.
import { createRegistry } from "../../contracts/src/index.js";
import { gitEnvironment } from "./git.js";
import {
  type AnalyzeRequest,
  type AnalyzerReport,
  AnalyzerRequestError,
  analyze,
  MAX_CLAIMS,
} from "./index.js";

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

const remote = "github.com/acme/web";
const relations = (report: AnalyzerReport): string[] =>
  report.claims.map((claim) => `${claim.relation} ${claim.normalized_subject}`).sort();

function worktreeWith(files: Record<string, string>): string {
  const worktree = mkdtempSync(join(tmpdir(), "conn-ts-analyzer-"));
  writeFiles(worktree, files);
  return worktree;
}

function writeFiles(worktree: string, files: Record<string, string>): void {
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(join(worktree, path, ".."), { recursive: true });
    writeFileSync(join(worktree, path), content);
  }
}

function planned(
  worktree: string,
  paths: string[],
  options: Pick<AnalyzeRequest, "symbols" | "tsconfig_path"> = {},
): AnalyzerReport {
  return analyze({
    request_version: 2,
    mode: "planned",
    worktree,
    canonical_remote: remote,
    paths,
    ...options,
  });
}

const nodeNextProject = JSON.stringify({
  compilerOptions: { module: "NodeNext", moduleResolution: "NodeNext", strict: true },
  include: ["**/*.ts"],
});

// Each case builds real TypeScript programs, which is slow on a loaded machine.
describe("planned analysis", { timeout: 30_000 }, () => {
  it("derives changes and consumes for the pre-edit fixture without its source literal", () => {
    const worktree = fileURLToPath(
      new URL("../../../fixtures/pre-edit-two-sessions/", import.meta.url),
    );

    const report = planned(worktree, [
      "src/payments/retry.ts",
      "src/orders/checkout.ts",
      "src/payments/new.ts",
    ]);
    const subset = planned(worktree, ["src/payments/retry.ts"], {
      symbols: { "src/payments/retry.ts": ["shouldRetryCharge"] },
    });

    for (const item of [report, subset]) {
      expectValidReport(item);
      expect(item.repository).toEqual({ canonical_remote: remote });
      expect(JSON.stringify(item)).not.toContain("PRE_EDIT_FIXTURE_SOURCE_MUST_NOT_BE_TRANSMITTED");
    }
    expect(relations(report)).toEqual([
      "changes contract:typescript:src/orders/checkout.ts/nextCheckoutStep",
      "changes contract:typescript:src/payments/retry.ts/ChargeAttempt",
      "changes contract:typescript:src/payments/retry.ts/shouldRetryCharge",
      "changes symbol:typescript:src/orders/checkout.ts/nextCheckoutStep",
      "changes symbol:typescript:src/payments/retry.ts/ChargeAttempt",
      "changes symbol:typescript:src/payments/retry.ts/shouldRetryCharge",
      "consumes contract:typescript:src/payments/retry.ts/ChargeAttempt",
      "consumes contract:typescript:src/payments/retry.ts/shouldRetryCharge",
    ]);
    expect(report.diagnostics).toEqual([
      { code: "conn-planned-path-new", message: expect.any(String), path: "src/payments/new.ts" },
    ]);
    expect(relations(subset)).toEqual([
      "changes contract:typescript:src/payments/retry.ts/shouldRetryCharge",
      "changes symbol:typescript:src/payments/retry.ts/shouldRetryCharge",
    ]);
  });

  it("replaces compiler diagnostic text with a bounded code-only message", () => {
    const worktree = worktreeWith({
      "tsconfig.json": JSON.stringify({ include: ["broken.ts"] }),
      "broken.ts": 'const message: number = "PRIVATE_SOURCE_LITERAL";\n',
    });

    const report = planned(worktree, ["broken.ts"]);

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
      "tsconfig.json": nodeNextProject,
      "app/[slug]/(group) page.ts": "export function page(): number { return 1; }\n",
      "app/@team/consumer.ts":
        'import { page } from "../[slug]/(group) page.js"; export const value = page();\n',
    });

    const report = planned(worktree, ["app/[slug]/(group) page.ts", "app/@team/consumer.ts"]);

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

    const report = planned(join(root, "pkg"), ["src/index.ts"]);
    const serialized = JSON.stringify(report);

    expectValidReport(report);
    expect(relations(report)).toEqual(
      expect.arrayContaining([
        "changes symbol:typescript:src/index.ts/hidden",
        "changes symbol:typescript:src/index.ts/own",
      ]),
    );
    expect(serialized).not.toContain("outside.ts");
    expect(serialized).not.toContain("node_modules");
  });

  it("skips TypeScript projects that contain none of the planned paths", () => {
    const worktree = worktreeWith({
      "selected/tsconfig.json": nodeNextProject,
      "selected/charge.ts": "export const charge = 1;\n",
      "selected/notes.md": "Not TypeScript.\n",
      "unrelated/tsconfig.json": nodeNextProject,
      "unrelated/broken.ts": 'export const value: number = "not a number";\n',
    });

    const selected = planned(worktree, ["selected/charge.ts", "selected/notes.md"]);
    const unrelated = planned(worktree, ["unrelated/broken.ts"]);

    expect(unrelated.diagnostics).toEqual(
      expect.arrayContaining([expect.objectContaining({ path: "unrelated/broken.ts" })]),
    );
    expect(selected.diagnostics).toEqual([
      {
        code: "conn-planned-path-unanalyzed",
        message: expect.any(String),
        path: "selected/notes.md",
      },
    ]);
    expect(relations(selected)).toEqual([
      "changes contract:typescript:selected/charge.ts/charge",
      "changes symbol:typescript:selected/charge.ts/charge",
    ]);
  });

  it("produces a schema-valid report for this pnpm workspace", () => {
    const worktree = fileURLToPath(new URL("../../../", import.meta.url));

    const report = planned(worktree, ["packages/typescript-analyzer/src/index.ts"], {
      tsconfig_path: "packages/typescript-analyzer/tsconfig.json",
    });

    expectValidReport(report);
    expect(report.claims.length).toBeGreaterThan(0);
    expect(JSON.stringify(report)).not.toContain("node_modules");
  });
});

describe("request validation", () => {
  const worktree = worktreeWith({ "src/a.ts": "export const a = 1;\n" });
  const valid = {
    request_version: 2,
    mode: "planned",
    worktree,
    canonical_remote: remote,
    paths: ["src/a.ts"],
  };

  it.each([
    ["a version 1 request", { request_version: 1 }, /request_version/],
    ["an unknown mode", { mode: "diff" }, /mode/],
    ["a relative worktree", { worktree: "relative/worktree" }, /worktree/],
    ["planned mode without paths", { paths: undefined }, /paths/],
    [
      "more than 50 planned paths",
      { paths: Array.from({ length: 51 }, (_, i) => `${i}.ts`) },
      /paths/,
    ],
    ["an empty path", { paths: [""] }, /paths/],
    ["an absolute path", { paths: [join(worktree, "src/a.ts")] }, /paths/],
    ["a drive-relative path", { paths: ["C:a.ts"] }, /paths/],
    ["a parent segment", { paths: ["src/../../outside.ts"] }, /paths/],
    ["an over-long path", { paths: [`${"a".repeat(1025)}.ts`] }, /paths/],
    ["symbols for an unplanned path", { symbols: { "src/b.ts": ["b"] } }, /symbols/],
    ["an empty symbol name", { symbols: { "src/a.ts": [""] } }, /symbols/],
    ["more than 50 symbol names", { symbols: { "src/a.ts": Array(51).fill("a") } }, /symbols/],
    ["symbols in observed mode", { mode: "observed", symbols: { "src/a.ts": ["a"] } }, /symbols/],
    ["a base revision in planned mode", { base_revision_ref: "main" }, /base_revision_ref/],
    [
      "an option-like base revision",
      { mode: "observed", base_revision_ref: "--output=/tmp/x" },
      /base_revision_ref/,
    ],
    [
      "a short option base revision",
      { mode: "observed", base_revision_ref: "-p" },
      /base_revision_ref/,
    ],
    [
      "a revision with a path",
      { mode: "observed", base_revision_ref: "HEAD:src/a.ts" },
      /base_revision_ref/,
    ],
  ])("rejects %s before analysis or Git runs", (_name, change, message) => {
    const request = { ...valid, ...change } as unknown as AnalyzeRequest;

    expect(() => analyze(request)).toThrow(AnalyzerRequestError);
    expect(() => analyze(request)).toThrow(message);
  });
});

describe("Git subprocess environment", () => {
  it("passes only allowlisted variables, matched case-insensitively on Windows", () => {
    const inherited = {
      Path: "C:\\Git\\cmd",
      SystemRoot: "C:\\Windows",
      HOME: "C:\\Users\\dev",
      CONN_TOKEN: "conn_sess_secret",
      GITHUB_TOKEN: "ghp_secret",
      GIT_ASKPASS: "askpass.exe",
      GIT_DIR: "C:\\elsewhere\\.git",
    };

    expect(gitEnvironment(inherited, "win32")).toEqual({
      Path: "C:\\Git\\cmd",
      SystemRoot: "C:\\Windows",
      HOME: "C:\\Users\\dev",
      GIT_TERMINAL_PROMPT: "0",
    });
  });
});

const gitAvailable = (() => {
  try {
    execFileSync("git", ["--version"], { stdio: "ignore", windowsHide: true });
    return true;
  } catch {
    return false;
  }
})();

function git(worktree: string, ...args: string[]): string {
  return execFileSync("git", args, {
    cwd: worktree,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
    windowsHide: true,
  }).trim();
}

function commitAll(worktree: string, message: string): string {
  git(worktree, "add", "--all");
  git(worktree, "commit", "--quiet", "--no-verify", "--message", message);
  return git(worktree, "rev-parse", "HEAD");
}

/** A temporary repository whose first commit holds `files`. Returns the base commit. */
function repositoryWith(files: Record<string, string>): { worktree: string; base: string } {
  const worktree = worktreeWith(files);
  git(worktree, "init", "--quiet");
  for (const [key, value] of [
    ["user.name", "Conn Test"],
    ["user.email", "conn-test@example.invalid"],
    ["commit.gpgsign", "false"],
    ["core.autocrlf", "false"],
  ] as const)
    git(worktree, "config", key, value);
  return { worktree, base: commitAll(worktree, "base") };
}

function observed(worktree: string, base?: string): AnalyzerReport {
  return analyze({
    request_version: 2,
    mode: "observed",
    worktree,
    canonical_remote: remote,
    ...(base === undefined ? {} : { base_revision_ref: base }),
  });
}

const observedProject = JSON.stringify({
  compilerOptions: { module: "NodeNext", moduleResolution: "NodeNext", strict: true },
  include: ["src/**/*.ts"],
});

describe.skipIf(!gitAvailable)(
  "observed analysis (requires git on PATH)",
  { timeout: 60_000 },
  () => {
    it("compares the base revision with the working tree by export fingerprint", () => {
      const baseRetry = [
        "export interface ChargeAttempt { idempotencyKey: string; attempt: number }",
        'const marker = "OBSERVED_SOURCE_MARKER_BASE";',
        "export function shouldRetryCharge(attempt: ChargeAttempt): boolean {",
        "  return marker.length > 0 && attempt.attempt < 3;",
        "}",
        "export function legacyRetry(): boolean { return false; }",
        "",
      ].join("\n");
      const { worktree, base } = repositoryWith({
        "tsconfig.json": observedProject,
        "src/payments/retry.ts": baseRetry,
        "src/orders/checkout.ts": [
          'import { type ChargeAttempt, shouldRetryCharge } from "../payments/retry.js";',
          'export function nextCheckoutStep(attempt: ChargeAttempt): "retry" | "fail" {',
          '  return shouldRetryCharge(attempt) ? "retry" : "fail";',
          "}",
          "",
        ].join("\n"),
        "src/legacy.ts": 'export const legacyFlag = "OBSERVED_SOURCE_MARKER_DELETED";\n',
        "src/stable.ts": "export const stable = 1;\n",
      });
      // A commit after the base: the comparison is base to working tree, not HEAD.
      writeFileSync(
        join(worktree, "src/orders/checkout.ts"),
        `${readFileSync(join(worktree, "src/orders/checkout.ts"), "utf8")}export const checkoutVersion = 2;\n`,
      );
      const head = commitAll(worktree, "checkout version");
      writeFiles(worktree, {
        // Only whitespace changes in ChargeAttempt; only the body changes in shouldRetryCharge.
        "src/payments/retry.ts": [
          "export interface ChargeAttempt {",
          "  idempotencyKey: string;",
          "  attempt: number",
          "}",
          'const marker = "OBSERVED_SOURCE_MARKER_HEAD";',
          "export function shouldRetryCharge(attempt: ChargeAttempt): boolean {",
          "  return marker.length > 0 && attempt.attempt < 5;",
          "}",
          "export const retryBudget = 5;",
          "",
        ].join("\n"),
        // Untracked and not ignored.
        "src/payments/refund.ts":
          'import { retryBudget } from "./retry.js";\nexport const refundRetries = retryBudget;\n',
      });
      rmSync(join(worktree, "src/legacy.ts"));

      const report = observed(worktree, base.slice(0, 12));

      expectValidReport(report);
      expect(report.repository).toEqual({
        canonical_remote: remote,
        base_revision_ref: base,
        head_revision_ref: head,
      });
      expect(relations(report)).toEqual([
        "changes contract:typescript:src/payments/retry.ts/shouldRetryCharge",
        "changes symbol:typescript:src/payments/retry.ts/shouldRetryCharge",
        "consumes contract:typescript:src/payments/retry.ts/ChargeAttempt",
        "consumes contract:typescript:src/payments/retry.ts/retryBudget",
        "consumes contract:typescript:src/payments/retry.ts/shouldRetryCharge",
        "produces contract:typescript:src/orders/checkout.ts/checkoutVersion",
        "produces contract:typescript:src/payments/refund.ts/refundRetries",
        "produces contract:typescript:src/payments/retry.ts/retryBudget",
        "removes contract:typescript:src/legacy.ts/legacyFlag",
        "removes contract:typescript:src/payments/retry.ts/legacyRetry",
        "removes symbol:typescript:src/legacy.ts/legacyFlag",
        "removes symbol:typescript:src/payments/retry.ts/legacyRetry",
      ]);
      // A removal cites the base version by hash; its content never leaves the process.
      expect(
        report.claims.find(
          (claim) =>
            claim.normalized_subject === "symbol:typescript:src/payments/retry.ts/legacyRetry",
        )?.evidence_refs,
      ).toEqual([
        expect.objectContaining({
          path: "src/payments/retry.ts",
          line: 6,
          content_hash: createHash("sha256").update(baseRetry).digest("hex"),
        }),
      ]);
      expect(report.diagnostics).toEqual([]);
      expect(JSON.stringify(report)).not.toContain("OBSERVED_SOURCE_MARKER");
    });

    it("keeps claims in relation priority order when it truncates", () => {
      const names = (prefix: string, from: number, to: number): string[] =>
        Array.from(
          { length: to - from },
          (_, i) => `${prefix}${String(from + i).padStart(3, "0")}`,
        );
      const declare = (items: string[], value: string): string =>
        items.map((name) => `export const ${name} = ${value};\n`).join("");
      const { worktree, base } = repositoryWith({
        "tsconfig.json": observedProject,
        "src/big.ts": declare(names("e", 0, 300), '"OBSERVED_SOURCE_MARKER"'),
      });
      writeFiles(worktree, {
        "src/big.ts": [
          declare(names("e", 100, 200), '"OBSERVED_SOURCE_MARKER_CHANGED"'),
          declare(names("e", 200, 300), '"OBSERVED_SOURCE_MARKER"'),
          declare(names("n", 0, 200), "1"),
        ].join(""),
        "src/use.ts": `import { ${names("e", 100, 150).join(", ")} } from "./big.js";\n`,
      });

      const report = observed(worktree, base);
      const count = (relation: string): number =>
        report.claims.filter((claim) => claim.relation === relation).length;

      expectValidReport(report);
      expect(report.claims).toHaveLength(MAX_CLAIMS);
      // 200 removes, 200 changes, 50 consumes, and 200 produces were found.
      expect([count("removes"), count("changes"), count("consumes"), count("produces")]).toEqual([
        200, 200, 50, 50,
      ]);
      expect(
        report.claims
          .filter((claim) => claim.relation === "produces")
          .map((claim) => claim.normalized_subject),
      ).toEqual(names("n", 0, 50).map((name) => `contract:typescript:src/big.ts/${name}`));
      expect(report.diagnostics).toEqual([
        { code: "conn-claims-truncated", message: "Kept 500 of 650 claims; omitted 150." },
      ]);
      expect(JSON.stringify(report)).not.toContain("OBSERVED_SOURCE_MARKER");
    });

    it.each([
      ["missing", undefined],
      ["an unknown commit", "0123456789abcdef"],
      ["an unknown branch", "refs/heads/missing"],
    ])("reports the base as unavailable, with no claims, when it is %s", (_name, reference) => {
      const { worktree } = repositoryWith({
        "tsconfig.json": observedProject,
        "src/a.ts": "export const a = 1;\n",
      });
      writeFiles(worktree, { "src/a.ts": 'export const a = "OBSERVED_SOURCE_MARKER";\n' });

      const report = observed(worktree, reference);

      expectValidReport(report);
      expect(report.repository).toEqual({ canonical_remote: remote });
      expect(report.claims).toEqual([]);
      expect(report.diagnostics).toEqual([
        { code: "conn-base-unavailable", message: expect.any(String) },
      ]);
      expect(JSON.stringify(report)).not.toContain("OBSERVED_SOURCE_MARKER");
    });
  },
);

describe("diagnostics analysis", { timeout: 30_000 }, () => {
  it("counts diagnostic codes per path without claims or message text", () => {
    const worktree = worktreeWith({
      "tsconfig.json": nodeNextProject,
      "src/retry.ts":
        "export function shouldRetryCharge(attempt: number, key: string): boolean { return attempt < 3 && key !== ''; }\n",
      "src/charge.ts":
        "import { shouldRetryCharge } from './retry.js';\nexport const first = shouldRetryCharge(1);\nexport const second = shouldRetryCharge(2);\n",
    });
    try {
      const report = analyze({
        request_version: 2,
        mode: "diagnostics",
        worktree,
        canonical_remote: remote,
      });
      expectValidReport(report);
      expect(report.claims).toEqual([]);
      expect(report.diagnostic_counts).toEqual([
        { code: "TS2554", path: "src/charge.ts", count: 2 },
      ]);
      expect(report.diagnostic_counts_truncated).toBe(false);
      expect(JSON.stringify(report)).not.toContain("Expected");
      expect(() =>
        analyze({
          request_version: 2,
          mode: "diagnostics",
          worktree,
          canonical_remote: remote,
          paths: ["src/retry.ts"],
        }),
      ).toThrow(AnalyzerRequestError);
    } finally {
      rmSync(worktree, { recursive: true, force: true });
    }
  });
});
