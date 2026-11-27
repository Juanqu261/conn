import { createHash } from "node:crypto";
import { readdirSync, readFileSync, realpathSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import ts from "typescript";
import { type ExportFingerprint, exportFingerprints, parseSource } from "./fingerprint.js";
import { baseFile, changedPaths, headCommit, resolveCommit } from "./git.js";
import {
  type AnalyzeMode,
  type AnalyzeRequest,
  isAcceptablePath,
  isInside,
  normalizedRelative,
  type ValidatedRequest,
  validateRequest,
} from "./request.js";

export { type AnalyzeMode, type AnalyzeRequest, AnalyzerRequestError } from "./request.js";

export const MAX_CLAIMS = 500;
export const MAX_DIAGNOSTICS = 100;
export const MAX_DIAGNOSTIC_COUNTS = 2000;
const MAX_PROJECTS = 25;
const MAX_OBSERVED_FILES = 500;
// Conn contract bounds: semanticSubject, opaqueReference, relativePath, title.
const MAX_SUBJECT_LENGTH = 512;
const MAX_LOCATOR_LENGTH = 256;
const MAX_PATH_LENGTH = 1024;
const MAX_DISPLAY_NAME_LENGTH = 200;
const TYPESCRIPT_SOURCE = /\.(?:ts|tsx|mts|cts)$/;
const DECLARATION_FILE = /\.d(?:\.[^./]+)?\.(?:ts|mts|cts)$/;

export type ClaimRelation = "removes" | "changes" | "consumes" | "produces";

/** When claims exceed MAX_CLAIMS, lower values are kept first. */
const RELATION_PRIORITY: Record<ClaimRelation, number> = {
  removes: 0,
  changes: 1,
  consumes: 2,
  produces: 3,
};

export const DIAGNOSTIC_CODES = {
  plannedPathNew: "conn-planned-path-new",
  plannedPathUnanalyzed: "conn-planned-path-unanalyzed",
  observedPathUnanalyzed: "conn-observed-path-unanalyzed",
  baseUnavailable: "conn-base-unavailable",
  changedPathsUnavailable: "conn-changed-paths-unavailable",
  changedPathsTruncated: "conn-changed-paths-truncated",
  claimsTruncated: "conn-claims-truncated",
  claimSkipped: "conn-claim-skipped",
} as const;

export interface EvidenceRef {
  kind: "source_location";
  locator: string;
  path: string;
  line: number;
  content_hash: string;
}

export interface Claim {
  subject_kind: "code_symbol" | "contract";
  normalized_subject: string;
  display_name: string;
  relation: ClaimRelation;
  provenance_kind: "static_analysis";
  evidence_refs: EvidenceRef[];
}

export interface Diagnostic {
  code: string;
  message: string;
  path?: string;
  line?: number;
}

export interface AnalyzerRepository {
  canonical_remote: string;
  /** Observed mode: the resolved full commit ID of the Intent base. */
  base_revision_ref?: string;
  /** Observed mode: the full commit ID of HEAD, when it exists. */
  head_revision_ref?: string;
}

export interface DiagnosticCount {
  code: string;
  path?: string;
  count: number;
}

export interface AnalyzerReport {
  report_version: 2;
  mode: AnalyzeMode;
  repository: AnalyzerRepository;
  claims: Claim[];
  diagnostics: Diagnostic[];
  /** Diagnostics mode only. */
  diagnostic_counts?: DiagnosticCount[];
  /** Diagnostics mode only. */
  diagnostic_counts_truncated?: boolean;
}

interface Context {
  request: ValidatedRequest;
  claims: Map<string, Claim>;
  /** Analyzer diagnostics; kept ahead of compiler diagnostics at the bound. */
  own: Diagnostic[];
  compiler: Diagnostic[];
  /** SHA-256 of file bytes by absolute file name. */
  hashes: Map<string, string>;
}

/**
 * Analyze TypeScript files locally. Planned mode describes files a participant
 * intends to change. Observed mode compares the Intent base revision with the
 * working tree. The report contains identifiers, paths, line numbers, hashes,
 * and diagnostic codes only. It never includes source text, base file content,
 * source excerpts, prompt content, or compiler message text.
 */
export function analyze(request: AnalyzeRequest): AnalyzerReport {
  const validated = validateRequest(request);
  const context: Context = {
    request: validated,
    claims: new Map(),
    own: [],
    compiler: [],
    hashes: new Map(),
  };
  const repository: AnalyzerRepository = { canonical_remote: validated.canonicalRemote };
  if (validated.mode === "diagnostics") return analyzeDiagnostics(context, repository);
  if (validated.mode === "planned") analyzePlanned(context);
  else analyzeObserved(context, repository);
  return report(context, repository);
}

/**
 * Counts compiler diagnostic codes per repository path over every discovered
 * program, for comparing a merged tree with each branch alone. A diagnostic
 * reported by several programs counts once. Diagnostics outside the worktree
 * or in node_modules count without a path. Message text is never included.
 */
function analyzeDiagnostics(context: Context, repository: AnalyzerRepository): AnalyzerReport {
  const { worktree, tsconfigPath } = context.request;
  const seen = new Set<string>();
  const counts = new Map<string, DiagnosticCount>();
  const count = (diagnostic: ts.Diagnostic): void => {
    const file = diagnostic.file;
    const key = `${diagnostic.code}|${file?.fileName ?? ""}|${diagnostic.start ?? -1}|${diagnostic.length ?? -1}`;
    if (seen.has(key)) return;
    seen.add(key);
    let path: string | undefined;
    if (file !== undefined && isInside(worktree, file.fileName)) {
      const relativePath = normalizedRelative(worktree, file.fileName);
      if (
        !relativePath.split("/").includes("node_modules") &&
        relativePath.length <= MAX_PATH_LENGTH
      )
        path = relativePath;
    }
    const code = `TS${diagnostic.code}`;
    const entryKey = `${code}|${path ?? ""}`;
    const entry =
      counts.get(entryKey) ?? (path === undefined ? { code, count: 0 } : { code, path, count: 0 });
    entry.count++;
    counts.set(entryKey, entry);
  };
  for (const configPath of projectConfigs(worktree, tsconfigPath)) {
    const result = ts.readConfigFile(configPath, ts.sys.readFile);
    if (result.error !== undefined) {
      count(result.error);
      continue;
    }
    const parsed = ts.parseJsonConfigFileContent(
      result.config,
      ts.sys,
      dirname(configPath),
      undefined,
      configPath,
    );
    for (const diagnostic of parsed.errors) count(diagnostic);
    if (parsed.fileNames.length === 0) continue;
    const program = ts.createProgram({ rootNames: parsed.fileNames, options: parsed.options });
    for (const diagnostic of ts.getPreEmitDiagnostics(program)) count(diagnostic);
  }
  const sorted = [...counts.values()].sort(
    (left, right) =>
      compareText(left.path ?? "", right.path ?? "") || compareText(left.code, right.code),
  );
  return {
    report_version: 2,
    mode: "diagnostics",
    repository,
    claims: [],
    diagnostics: context.own.slice(0, MAX_DIAGNOSTICS),
    diagnostic_counts: sorted.slice(0, MAX_DIAGNOSTIC_COUNTS),
    diagnostic_counts_truncated: sorted.length > MAX_DIAGNOSTIC_COUNTS,
  };
}

function analyzePlanned(context: Context): void {
  const { request } = context;
  const existing: string[] = [];
  const readable = new Set<string>();
  for (const path of request.paths) {
    const { state } = worktreeFile(request, path);
    if (state === "missing")
      addOwn(context, {
        code: DIAGNOSTIC_CODES.plannedPathNew,
        message: "Planned path does not exist yet; no claims were derived.",
        path,
      });
    else existing.push(path);
    // A directory or a link that leaves the worktree is never analyzed.
    if (state === "file") readable.add(path);
  }
  const analyzed = visitProgramFiles(context, readable, (program, sourceFile, path) => {
    collectExports(context, program, sourceFile, path, request.symbols?.get(path));
    collectImports(context, program, sourceFile, path);
  });
  for (const path of existing)
    if (!analyzed.has(path))
      addOwn(context, {
        code: DIAGNOSTIC_CODES.plannedPathUnanalyzed,
        message:
          "No discovered TypeScript program includes the planned path; no claims were derived.",
        path,
      });
}

function analyzeObserved(context: Context, repository: AnalyzerRepository): void {
  const { request } = context;
  const reference = request.baseRevisionRef;
  const base = reference === undefined ? undefined : resolveCommit(request.worktree, reference);
  if (base === undefined) {
    addOwn(context, {
      code: DIAGNOSTIC_CODES.baseUnavailable,
      message: "The Intent base revision is unavailable, so no observed claims were derived.",
    });
    return;
  }
  repository.base_revision_ref = base;
  const head = headCommit(request.worktree);
  if (head !== undefined) repository.head_revision_ref = head;

  let candidates = request.paths;
  if (candidates.length === 0) {
    const listed = changedPaths(request.worktree, base);
    if (listed === undefined) {
      addOwn(context, {
        code: DIAGNOSTIC_CODES.changedPathsUnavailable,
        message: "Git could not list the changed files, so no observed claims were derived.",
      });
      return;
    }
    candidates = listed
      .filter(isAcceptablePath)
      .map((path) => normalizedRelative(request.worktree, path));
  }
  const selected = [...new Set(candidates)].filter(isTypeScriptSource).sort(compareText);
  if (selected.length > MAX_OBSERVED_FILES) {
    addOwn(context, {
      code: DIAGNOSTIC_CODES.changedPathsTruncated,
      message: `Analyzed ${MAX_OBSERVED_FILES} of ${selected.length} changed TypeScript files; omitted ${selected.length - MAX_OBSERVED_FILES}.`,
    });
    selected.length = MAX_OBSERVED_FILES;
  }

  const present = new Set<string>();
  for (const path of selected) {
    const current = readWorktreeFile(request, path);
    const previous = current === "unreadable" ? undefined : baseFile(request.worktree, base, path);
    if (current === "unreadable" || previous?.state === "unreadable") {
      addOwn(context, {
        code: DIAGNOSTIC_CODES.observedPathUnanalyzed,
        message: "The changed file could not be read safely; no claims were derived for it.",
        path,
      });
      continue;
    }
    if (current !== undefined) present.add(path);
    compareExports(
      context,
      path,
      previous?.state === "found" ? previous.content : undefined,
      current,
    );
  }
  visitProgramFiles(context, present, (program, sourceFile, path) =>
    collectImports(context, program, sourceFile, path),
  );
}

/**
 * Emits `changes` for an export whose fingerprint differs, `removes` for an
 * export missing from the working tree, and `produces` (contract only) for a
 * new export. Unchanged exports emit nothing. Base content is parsed in memory
 * and only its hash and line numbers reach the report.
 */
function compareExports(
  context: Context,
  path: string,
  baseContent: Buffer | undefined,
  currentContent: Buffer | undefined,
): void {
  const before: Map<string, ExportFingerprint> =
    baseContent === undefined ? new Map() : exportFingerprints(parseSource(path, baseContent));
  const after: Map<string, ExportFingerprint> =
    currentContent === undefined
      ? new Map()
      : exportFingerprints(parseSource(path, currentContent));
  if (currentContent !== undefined) {
    const hash = sha256(currentContent);
    // Import evidence for this file then cites the same bytes.
    context.hashes.set(resolve(context.request.worktree, path), hash);
    for (const [name, current] of after) {
      const previous = before.get(name);
      if (previous !== undefined && previous.fingerprint === current.fingerprint) continue;
      addExportClaims(
        context,
        path,
        name,
        previous === undefined ? "produces" : "changes",
        sourceEvidence(path, name, current.line, hash),
      );
    }
  }
  if (baseContent === undefined) return;
  const baseHash = sha256(baseContent);
  for (const [name, previous] of before)
    if (!after.has(name))
      addExportClaims(
        context,
        path,
        name,
        "removes",
        sourceEvidence(path, name, previous.line, baseHash),
      );
}

/**
 * Builds each discovered TypeScript program that includes a selected path not
 * yet visited, and calls `visit` once per selected repository source file.
 * Returns the visited paths.
 */
function visitProgramFiles(
  context: Context,
  paths: ReadonlySet<string>,
  visit: (program: ts.Program, sourceFile: ts.SourceFile, path: string) => void,
): Set<string> {
  const { worktree, tsconfigPath } = context.request;
  const visited = new Set<string>();
  if (paths.size === 0) return visited;
  for (const configPath of projectConfigs(worktree, tsconfigPath)) {
    const pending = new Set([...paths].filter((path) => !visited.has(path)));
    if (pending.size === 0) break;
    const parsed = readConfig(context, configPath);
    // Building a program is the expensive step. Skip a project that contains
    // none of the pending paths, and its unrelated diagnostics.
    if (parsed === undefined || !includesSelectedPath(parsed.fileNames, worktree, pending))
      continue;
    const program = ts.createProgram({ rootNames: parsed.fileNames, options: parsed.options });
    for (const diagnostic of ts.getPreEmitDiagnostics(program)) addCompiler(context, diagnostic);
    for (const sourceFile of program.getSourceFiles()) {
      if (sourceFile.isDeclarationFile || !isRepositorySource(program, worktree, sourceFile))
        continue;
      const path = normalizedRelative(worktree, sourceFile.fileName);
      if (!pending.has(path)) continue;
      visited.add(path);
      visit(program, sourceFile, path);
    }
  }
  return visited;
}

function projectConfigs(worktree: string, configured: string | undefined): string[] {
  if (configured !== undefined) return [configured];
  const configs: string[] = [];
  const visit = (directory: string, depth: number): void => {
    if (configs.length >= MAX_PROJECTS || depth > 5) return;
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.name === "node_modules" || entry.name === ".git" || entry.name === "dist") continue;
      const path = join(directory, entry.name);
      if (entry.isFile() && entry.name === "tsconfig.json") configs.push(path);
      if (entry.isDirectory()) visit(path, depth + 1);
      if (configs.length >= MAX_PROJECTS) return;
    }
  };
  visit(worktree, 0);
  return configs.sort();
}

function readConfig(context: Context, configPath: string): ts.ParsedCommandLine | undefined {
  const result = ts.readConfigFile(configPath, ts.sys.readFile);
  if (result.error !== undefined) {
    addCompiler(context, result.error);
    return undefined;
  }
  const parsed = ts.parseJsonConfigFileContent(
    result.config,
    ts.sys,
    dirname(configPath),
    undefined,
    configPath,
  );
  for (const diagnostic of parsed.errors) addCompiler(context, diagnostic);
  return parsed;
}

function includesSelectedPath(
  fileNames: readonly string[],
  worktree: string,
  selectedPaths: ReadonlySet<string>,
): boolean {
  return fileNames.some(
    (fileName) =>
      isInside(worktree, fileName) && selectedPaths.has(normalizedRelative(worktree, fileName)),
  );
}

/**
 * A repository source is inside the worktree and not an external library.
 * Third-party packages, including the pnpm node_modules/.pnpm store, are not
 * work that another Intent in this repository can change.
 */
function isRepositorySource(program: ts.Program, worktree: string, file: ts.SourceFile): boolean {
  return (
    isInside(worktree, file.fileName) &&
    !program.isSourceFileFromExternalLibrary(file) &&
    !normalizedRelative(worktree, file.fileName).split("/").includes("node_modules")
  );
}

function isTypeScriptSource(path: string): boolean {
  return (
    TYPESCRIPT_SOURCE.test(path) &&
    !DECLARATION_FILE.test(path) &&
    !path.split("/").includes("node_modules")
  );
}

type WorktreeFile = { state: "file"; absolute: string } | { state: "missing" | "unreadable" };

/**
 * Locates a worktree-relative path. A path whose real location is outside the
 * worktree, such as a symbolic link to another directory, or which is not a
 * regular file, is unreadable.
 */
function worktreeFile(request: ValidatedRequest, path: string): WorktreeFile {
  const absolute = resolve(request.worktree, path);
  let real: string;
  try {
    real = realpathSync(absolute);
  } catch (error) {
    const code = (error as NodeJS.ErrnoException).code;
    return { state: code === "ENOENT" || code === "ENOTDIR" ? "missing" : "unreadable" };
  }
  try {
    if (isInside(request.realWorktree, real) && statSync(real).isFile())
      return { state: "file", absolute };
  } catch {
    // Unreadable below.
  }
  return { state: "unreadable" };
}

/** Returns the bytes of a working-tree file, undefined when it is absent. */
function readWorktreeFile(
  request: ValidatedRequest,
  path: string,
): Buffer | "unreadable" | undefined {
  const file = worktreeFile(request, path);
  if (file.state !== "file") return file.state === "missing" ? undefined : "unreadable";
  try {
    return readFileSync(file.absolute);
  } catch {
    return "unreadable";
  }
}

function collectExports(
  context: Context,
  program: ts.Program,
  file: ts.SourceFile,
  path: string,
  subset: ReadonlySet<string> | undefined,
): void {
  const { worktree } = context.request;
  const checker = program.getTypeChecker();
  const moduleSymbol = checker.getSymbolAtLocation(file);
  if (moduleSymbol === undefined) return;
  for (const symbol of checker.getExportsOfModule(moduleSymbol)) {
    const declaration = symbol.valueDeclaration ?? symbol.declarations?.[0];
    if (declaration === undefined) continue;
    const name = checker.symbolToString(symbol);
    if (subset !== undefined && !subset.has(symbol.name) && !subset.has(name)) continue;
    // `export *` can surface a declaration outside the worktree. Its location
    // must not be disclosed, so cite the re-exporting module instead.
    const declared = declaration.getSourceFile();
    const evidence = isInside(worktree, declared.fileName)
      ? evidenceFor(context, declared, declaration.getStart(), name)
      : evidenceFor(context, file, 0, name);
    addExportClaims(context, path, name, "changes", evidence);
  }
}

/** `changes` and `removes` cover the symbol and its contract; `produces` covers the contract. */
function addExportClaims(
  context: Context,
  path: string,
  name: string,
  relation: "changes" | "removes" | "produces",
  evidence: EvidenceRef,
): void {
  const reference = `${subjectPath(path)}/${stableName(name)}`;
  if (relation !== "produces")
    addClaim(context, {
      subject_kind: "code_symbol",
      normalized_subject: `symbol:typescript:${reference}`,
      display_name: name,
      relation,
      provenance_kind: "static_analysis",
      evidence_refs: [evidence],
    });
  addClaim(context, {
    subject_kind: "contract",
    normalized_subject: `contract:typescript:${reference}`,
    display_name: name,
    relation,
    provenance_kind: "static_analysis",
    evidence_refs: [evidence],
  });
}

function collectImports(
  context: Context,
  program: ts.Program,
  file: ts.SourceFile,
  path: string,
): void {
  const { worktree } = context.request;
  const checker = program.getTypeChecker();
  for (const statement of file.statements) {
    if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier))
      continue;
    const moduleSymbol = checker.getSymbolAtLocation(statement.moduleSpecifier);
    const target = moduleSymbol?.declarations?.[0]?.getSourceFile();
    if (moduleSymbol === undefined || target === undefined) continue;
    if (!isRepositorySource(program, worktree, target)) continue;
    const targetPath = subjectPath(normalizedRelative(worktree, target.fileName));
    const exports = checker.getExportsOfModule(moduleSymbol);
    for (const name of importBindings(statement.importClause)) {
      const resolved = exports.find((symbol) => symbol.name === name);
      const stable = stableName(resolved === undefined ? name : checker.symbolToString(resolved));
      addClaim(context, {
        subject_kind: "contract",
        normalized_subject: `contract:typescript:${targetPath}/${stable}`,
        display_name: name,
        relation: "consumes",
        provenance_kind: "static_analysis",
        evidence_refs: [evidenceFor(context, file, statement.getStart(), `${path}/${name}`)],
      });
    }
  }
}

function importBindings(clause: ts.ImportClause | undefined): string[] {
  if (clause === undefined) return ["module"];
  const bindings: string[] = [];
  if (clause.name !== undefined) bindings.push("default");
  if (clause.namedBindings !== undefined && ts.isNamespaceImport(clause.namedBindings))
    bindings.push("module");
  if (clause.namedBindings !== undefined && ts.isNamedImports(clause.namedBindings))
    for (const item of clause.namedBindings.elements)
      bindings.push(item.propertyName?.text ?? item.name.text);
  return bindings.length === 0 ? ["module"] : bindings;
}

/**
 * Adds a claim only when it fits the Conn contract bounds. A claim that cannot
 * be represented is omitted with a code-only diagnostic, so one unusual file
 * cannot invalidate the whole report. The claim limit applies later, in
 * relation priority order.
 */
function addClaim(context: Context, claim: Claim): void {
  const key = `${claim.normalized_subject} ${claim.relation}`;
  if (context.claims.has(key)) return;
  const evidence = claim.evidence_refs[0];
  if (
    claim.normalized_subject.length > MAX_SUBJECT_LENGTH ||
    (evidence !== undefined &&
      (evidence.locator.length > MAX_LOCATOR_LENGTH || evidence.path.length > MAX_PATH_LENGTH))
  ) {
    const diagnostic: Diagnostic = {
      code: DIAGNOSTIC_CODES.claimSkipped,
      message: "A claim exceeded the Conn reference length bounds and was omitted.",
    };
    if (evidence !== undefined && evidence.path.length <= MAX_PATH_LENGTH) {
      diagnostic.path = evidence.path;
      diagnostic.line = evidence.line;
    }
    addOwn(context, diagnostic);
    return;
  }
  context.claims.set(key, { ...claim, display_name: displayName(claim.display_name) });
}

function displayName(value: string): string {
  // biome-ignore lint/suspicious/noControlCharactersInRegex: control characters are replaced.
  const bounded = value.replace(/[\u0000-\u001f\u007f]/g, " ").slice(0, MAX_DISPLAY_NAME_LENGTH);
  return bounded.trim() === "" ? "module" : bounded;
}

function evidenceFor(
  context: Context,
  file: ts.SourceFile,
  position: number,
  locator: string,
): EvidenceRef {
  const path = normalizedRelative(context.request.worktree, file.fileName);
  const absolute = resolve(file.fileName);
  let hash = context.hashes.get(absolute);
  if (hash === undefined) {
    hash = sha256(readFileSync(absolute));
    context.hashes.set(absolute, hash);
  }
  return sourceEvidence(path, locator, file.getLineAndCharacterOfPosition(position).line + 1, hash);
}

function sourceEvidence(path: string, locator: string, line: number, hash: string): EvidenceRef {
  return {
    kind: "source_location",
    locator: `typescript:${subjectPath(path)}:${stableName(locator)}`,
    path,
    line,
    content_hash: hash,
  };
}

function addOwn(context: Context, diagnostic: Diagnostic): void {
  if (context.own.length < MAX_DIAGNOSTICS) context.own.push(diagnostic);
}

function addCompiler(context: Context, diagnostic: ts.Diagnostic): void {
  if (context.compiler.length >= MAX_DIAGNOSTICS) return;
  const { worktree } = context.request;
  const item: Diagnostic = {
    code: `TS${diagnostic.code}`,
    message: `TypeScript diagnostic TS${diagnostic.code}`,
  };
  if (diagnostic.file !== undefined && isInside(worktree, diagnostic.file.fileName)) {
    item.path = normalizedRelative(worktree, diagnostic.file.fileName);
    item.line = diagnostic.file.getLineAndCharacterOfPosition(diagnostic.start ?? 0).line + 1;
  }
  context.compiler.push(item);
}

/**
 * Applies the claim limit in relation priority order, then sorts the kept
 * claims by subject. The truncation notice and analyzer diagnostics are kept
 * ahead of compiler diagnostics at the diagnostic limit.
 */
function report(context: Context, repository: AnalyzerRepository): AnalyzerReport {
  const ranked = [...context.claims.values()].sort(
    (left, right) =>
      RELATION_PRIORITY[left.relation] - RELATION_PRIORITY[right.relation] ||
      compareText(left.normalized_subject, right.normalized_subject),
  );
  const kept = ranked.slice(0, MAX_CLAIMS);
  const notices: Diagnostic[] = [];
  if (ranked.length > kept.length)
    notices.push({
      code: DIAGNOSTIC_CODES.claimsTruncated,
      message: `Kept ${kept.length} of ${ranked.length} claims; omitted ${ranked.length - kept.length}.`,
    });
  const diagnostics = [...notices, ...context.own, ...context.compiler].slice(0, MAX_DIAGNOSTICS);
  return {
    report_version: 2,
    mode: context.request.mode,
    repository,
    claims: kept.sort(
      (left, right) =>
        compareText(left.normalized_subject, right.normalized_subject) ||
        RELATION_PRIORITY[left.relation] - RELATION_PRIORITY[right.relation],
    ),
    diagnostics: diagnostics.sort((left, right) =>
      compareText(
        `${left.code}:${left.path ?? ""}:${left.line ?? 0}`,
        `${right.code}:${right.path ?? ""}:${right.line ?? 0}`,
      ),
    ),
  };
}

function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

function sha256(content: Buffer): string {
  return createHash("sha256").update(content).digest("hex");
}

function stableName(value: string): string {
  return value.replace(/[^A-Za-z0-9._~-]/g, "_").slice(0, 160) || "module";
}

/**
 * Maps a repository-relative path onto the semantic-subject alphabet while
 * keeping its directory structure: `app/[slug]/page.ts` becomes
 * `app/_slug_/page.ts`. The unmodified path remains in the evidence reference.
 */
function subjectPath(path: string): string {
  return path
    .split("/")
    .map((segment) => segment.replace(/[^A-Za-z0-9._~-]/g, "_") || "_")
    .join("/");
}

export const analyzerCapability = { language: "typescript", status: "local-compiler-api" } as const;
export type AnalyzerCapability = typeof analyzerCapability;
