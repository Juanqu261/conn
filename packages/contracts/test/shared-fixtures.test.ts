import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { createRegistry } from "../src/index.js";

// Protects cross-language schema interpretation: Ajv must accept and reject
// exactly the documents the Go validator does, at the same locations.

interface FixtureCase {
  name: string;
  schema: string;
  valid: boolean;
  at?: string;
  document: unknown;
}

const contractsDir = join(import.meta.dirname, "..", "..", "..", "contracts", "v1");
const readJson = (path: string): unknown => JSON.parse(readFileSync(path, "utf8"));

const registry = createRegistry(
  readdirSync(contractsDir)
    .filter((file) => file.endsWith(".schema.json"))
    .sort()
    .map((file) => readJson(join(contractsDir, file)) as object),
);
const { cases } = readJson(join(contractsDir, "fixtures", "cases.json")) as {
  cases: FixtureCase[];
};

describe("shared contract fixtures", () => {
  it.each(cases)("$name", ({ schema, valid, at, document }) => {
    const result = registry.validate(schema, document);
    if (valid) {
      expect(result).toEqual({ valid: true });
      return;
    }
    expect(at).toBeTypeOf("string");
    expect(result.valid).toBe(false);
    if (!result.valid) {
      expect(result.violations.length).toBeGreaterThan(0);
      for (const violation of result.violations) {
        expect(violation.instancePath, violation.message).toBe(at);
      }
    }
  });
});
