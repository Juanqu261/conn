import { realpathSync, statSync } from "node:fs";
import { isAbsolute, posix, relative, resolve, sep, win32 } from "node:path";

export type AnalyzeMode = "planned" | "observed" | "diagnostics";

export interface AnalyzeRequest {
  request_version: 2;
  mode: AnalyzeMode;
  /** Absolute path of the trusted worktree. Git runs with it as the working directory. */
  worktree: string;
  canonical_remote: string;
  tsconfig_path?: string;
  /** Planned: required, 1 to 50 entries. Observed: optional, replaces the changed set. Diagnostics: not allowed. */
  paths?: string[];
  /** Planned only: worktree-relative path to the export names to include. */
  symbols?: Record<string, string[]>;
  /** Observed only: the Intent base revision. */
  base_revision_ref?: string;
}

/** A request the analyzer refuses. Its message never contains request values. */
export class AnalyzerRequestError extends Error {
  override name = "AnalyzerRequestError";
}

export interface ValidatedRequest {
  mode: AnalyzeMode;
  worktree: string;
  realWorktree: string;
  canonicalRemote: string;
  tsconfigPath: string | undefined;
  /** Normalized worktree-relative paths with `/` separators, without duplicates. */
  paths: string[];
  symbols: Map<string, ReadonlySet<string>> | undefined;
  baseRevisionRef: string | undefined;
}

const MAX_PATH_LENGTH = 1024;
const MAX_PLANNED_PATHS = 50;
const MAX_OBSERVED_PATHS = 500;
const MAX_SYMBOLS_PER_PATH = 50;
const MAX_SYMBOL_LENGTH = 200;
const MAX_NAMED_REF_LENGTH = 256;
const CANONICAL_REMOTE = /^[a-z0-9][a-z0-9.-]*(\/[A-Za-z0-9._~-]+)+$/;
const COMMIT_REF = /^[0-9a-f]{7,64}$/;
const NAMED_REF = /^[A-Za-z0-9._/-]+$/;
// biome-ignore lint/suspicious/noControlCharactersInRegex: paths must not carry control characters.
const CONTROL_CHARACTER = /[\u0000-\u001f\u007f]/;

/**
 * Validates the whole request before the analyzer touches Git or builds a
 * TypeScript program. Every rejection uses a fixed message.
 */
export function validateRequest(input: unknown): ValidatedRequest {
  if (!isRecord(input)) throw new AnalyzerRequestError("request must be a JSON object");
  if (input.request_version !== 2) throw new AnalyzerRequestError("request_version must be 2");
  const mode = input.mode;
  if (mode !== "planned" && mode !== "observed" && mode !== "diagnostics")
    throw new AnalyzerRequestError("mode must be planned, observed, or diagnostics");
  const worktree = existingWorktree(input.worktree);
  if (typeof input.canonical_remote !== "string" || !CANONICAL_REMOTE.test(input.canonical_remote))
    throw new AnalyzerRequestError(
      "canonical_remote must be a normalized credential-free Git remote",
    );

  const paths = requestPaths(worktree.path, mode, optional(input.paths));
  const symbols = requestSymbols(worktree.path, mode, paths, optional(input.symbols));
  const baseRevisionRef = optional(input.base_revision_ref);
  if (baseRevisionRef !== undefined) {
    if (mode !== "observed")
      throw new AnalyzerRequestError("base_revision_ref is only allowed in observed mode");
    if (typeof baseRevisionRef !== "string" || !isRevisionRef(baseRevisionRef))
      throw new AnalyzerRequestError(
        "base_revision_ref must be a commit ID or a ref name that does not start with -",
      );
  }

  return {
    mode,
    worktree: worktree.path,
    realWorktree: worktree.real,
    canonicalRemote: input.canonical_remote,
    tsconfigPath: tsconfigPath(worktree.path, optional(input.tsconfig_path)),
    paths,
    symbols,
    baseRevisionRef,
  };
}

/**
 * A revision is a lowercase object ID or a plain ref name. A leading `-` is
 * refused so that a revision can never be read as a Git option.
 */
export function isRevisionRef(value: string): boolean {
  return (
    COMMIT_REF.test(value) ||
    (value.length <= MAX_NAMED_REF_LENGTH && NAMED_REF.test(value) && !value.startsWith("-"))
  );
}

/**
 * Accepts a non-empty relative path without control characters, drive
 * prefixes, or `..` segments. Paths reported by Git pass through the same check.
 */
export function isAcceptablePath(value: string): boolean {
  return (
    value.length > 0 &&
    value.length <= MAX_PATH_LENGTH &&
    !CONTROL_CHARACTER.test(value) &&
    !isAbsolute(value) &&
    !posix.isAbsolute(value) &&
    !win32.isAbsolute(value) &&
    !/^[A-Za-z]:/.test(value) &&
    !value.split(/[\\/]/).includes("..")
  );
}

export function normalizedRelative(worktree: string, path: string): string {
  const value = relative(worktree, resolve(worktree, path)).split(sep).join("/");
  if (value === "" || value.startsWith("../") || value === ".." || isAbsolute(value))
    throw new AnalyzerRequestError("path must stay inside the worktree");
  return value;
}

export function isInside(worktree: string, path: string): boolean {
  const relativePath = relative(worktree, resolve(path));
  return (
    relativePath !== "" &&
    relativePath !== ".." &&
    !relativePath.startsWith(`..${sep}`) &&
    !relativePath.startsWith("../") &&
    !isAbsolute(relativePath)
  );
}

function existingWorktree(value: unknown): { path: string; real: string } {
  if (typeof value !== "string" || value.length === 0 || !isAbsolute(value))
    throw new AnalyzerRequestError("worktree must be an absolute path");
  const path = resolve(value);
  try {
    const real = realpathSync(path);
    if (statSync(real).isDirectory()) return { path, real };
  } catch {
    // Reported below with a fixed message.
  }
  throw new AnalyzerRequestError("worktree must be an existing directory");
}

function requestPaths(worktree: string, mode: AnalyzeMode, value: unknown): string[] {
  if (mode === "diagnostics") {
    if (value !== undefined)
      throw new AnalyzerRequestError("paths is not allowed in diagnostics mode");
    return [];
  }
  if (value === undefined) {
    if (mode === "planned") throw new AnalyzerRequestError("planned mode requires 1 to 50 paths");
    return [];
  }
  const [minimum, maximum] = mode === "planned" ? [1, MAX_PLANNED_PATHS] : [0, MAX_OBSERVED_PATHS];
  if (!Array.isArray(value) || value.length < minimum || value.length > maximum)
    throw new AnalyzerRequestError(
      mode === "planned"
        ? "planned mode requires 1 to 50 paths"
        : "observed mode accepts at most 500 paths",
    );
  return [...new Set(value.map((path) => relativePath(worktree, path, "paths")))];
}

function requestSymbols(
  worktree: string,
  mode: AnalyzeMode,
  paths: readonly string[],
  value: unknown,
): Map<string, ReadonlySet<string>> | undefined {
  if (value === undefined) return undefined;
  if (mode !== "planned") throw new AnalyzerRequestError("symbols is only allowed in planned mode");
  if (!isRecord(value))
    throw new AnalyzerRequestError("symbols must map planned paths to export names");
  const symbols = new Map<string, Set<string>>();
  for (const [key, names] of Object.entries(value)) {
    const path = relativePath(worktree, key, "symbols keys");
    if (!paths.includes(path))
      throw new AnalyzerRequestError("symbols keys must be among the planned paths");
    if (
      !Array.isArray(names) ||
      names.length > MAX_SYMBOLS_PER_PATH ||
      names.some(
        (name) => typeof name !== "string" || name.length === 0 || name.length > MAX_SYMBOL_LENGTH,
      )
    )
      throw new AnalyzerRequestError(
        "symbols must list at most 50 export names of 1 to 200 characters per path",
      );
    const selected = symbols.get(path) ?? new Set<string>();
    for (const name of names as string[]) selected.add(name);
    symbols.set(path, selected);
  }
  return symbols;
}

function tsconfigPath(worktree: string, value: unknown): string | undefined {
  if (value === undefined) return undefined;
  if (typeof value === "string" && value.length > 0 && value.length <= MAX_PATH_LENGTH) {
    const path = resolve(worktree, value);
    try {
      if (isInside(worktree, path) && statSync(path).isFile()) return path;
    } catch {
      // Reported below with a fixed message.
    }
  }
  throw new AnalyzerRequestError("tsconfig_path must name a file inside the worktree");
}

function relativePath(worktree: string, value: unknown, field: string): string {
  if (typeof value !== "string" || !isAcceptablePath(value))
    throw new AnalyzerRequestError(
      `${field} must be non-empty relative paths of at most 1024 characters inside the worktree`,
    );
  return normalizedRelative(worktree, value);
}

/** Go encodes an absent slice or map as null; treat it as an omitted field. */
function optional(value: unknown): unknown {
  return value === null ? undefined : value;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
