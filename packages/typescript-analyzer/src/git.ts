import { execFileSync } from "node:child_process";

const GIT_TIMEOUT_MS = 10_000;
const GIT_MAX_BUFFER = 8 * 1024 * 1024;
const FULL_COMMIT = /^[0-9a-f]{40}(?:[0-9a-f]{24})?$/;
// Only what Git needs to start and find the user's configuration. Conn
// session tokens and credential helpers' variables are never inherited.
const INHERITED_VARIABLES = new Set([
  "PATH",
  "SYSTEMROOT",
  "COMSPEC",
  "TEMP",
  "TMP",
  "HOME",
  "USERPROFILE",
]);

type GitResult = { ok: true; stdout: Buffer } | { ok: false; incomplete: boolean };

export type BaseFile =
  | { state: "found"; content: Buffer }
  | { state: "missing" }
  | { state: "unreadable" };

export function gitEnvironment(
  source: NodeJS.ProcessEnv = process.env,
  platform: NodeJS.Platform = process.platform,
): Record<string, string> {
  const environment: Record<string, string> = {};
  for (const [key, value] of Object.entries(source)) {
    const name = platform === "win32" ? key.toUpperCase() : key;
    if (value !== undefined && INHERITED_VARIABLES.has(name)) environment[key] = value;
  }
  environment.GIT_TERMINAL_PROMPT = "0";
  return environment;
}

/** Resolves a validated revision to a full commit ID, or undefined when it is unknown. */
export function resolveCommit(worktree: string, revision: string): string | undefined {
  return commitOutput(git(worktree, ["rev-parse", "--verify", "--quiet", `${revision}^{commit}`]));
}

export function headCommit(worktree: string): string | undefined {
  return commitOutput(git(worktree, ["rev-parse", "--verify", "--quiet", "HEAD"]));
}

/**
 * Lists paths that differ between the base commit and the working tree
 * (staged and unstaged), plus untracked files that are not ignored. Paths are
 * relative to the worktree. Rename detection is off so that a renamed file
 * reports both its removed and its added path.
 */
export function changedPaths(worktree: string, base: string): string[] | undefined {
  const changed = git(worktree, [
    "diff",
    "--no-ext-diff",
    "--no-renames",
    "--name-only",
    "--relative",
    "-z",
    base,
    "--",
  ]);
  const untracked = git(worktree, ["ls-files", "--others", "--exclude-standard", "-z"]);
  if (!changed.ok || !untracked.ok) return undefined;
  return [...nulSeparated(changed.stdout), ...nulSeparated(untracked.stdout)];
}

/** Reads one file of the base commit. The content stays in memory only. */
export function baseFile(worktree: string, base: string, path: string): BaseFile {
  const result = git(worktree, ["show", "--no-textconv", `${base}:./${path}`]);
  if (result.ok) return { state: "found", content: result.stdout };
  return result.incomplete ? { state: "unreadable" } : { state: "missing" };
}

function git(worktree: string, args: readonly string[]): GitResult {
  try {
    const stdout = execFileSync("git", args, {
      cwd: worktree,
      encoding: "buffer",
      env: gitEnvironment(),
      maxBuffer: GIT_MAX_BUFFER,
      shell: false,
      stdio: ["ignore", "pipe", "ignore"],
      timeout: GIT_TIMEOUT_MS,
      windowsHide: true,
    });
    return { ok: true, stdout };
  } catch (error) {
    // A timeout or an oversized output says nothing about whether the object
    // exists, so callers must not treat it as a missing file.
    const code = (error as NodeJS.ErrnoException | undefined)?.code;
    return { ok: false, incomplete: code === "ETIMEDOUT" || code === "ENOBUFS" };
  }
}

function commitOutput(result: GitResult): string | undefined {
  if (!result.ok) return undefined;
  const value = result.stdout.toString("utf8").trim();
  return FULL_COMMIT.test(value) ? value : undefined;
}

function nulSeparated(output: Buffer): string[] {
  return output
    .toString("utf8")
    .split("\0")
    .filter((path) => path.length > 0);
}
