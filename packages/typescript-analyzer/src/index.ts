import { createHash } from "node:crypto";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import ts from "typescript";

export const MAX_CLAIMS = 500;
export const MAX_DIAGNOSTICS = 100;
const MAX_PROJECTS = 25;
// Conn contract bounds: semanticSubject, opaqueReference, relativePath, title.
const MAX_SUBJECT_LENGTH = 512;
const MAX_LOCATOR_LENGTH = 256;
const MAX_PATH_LENGTH = 1024;
const MAX_DISPLAY_NAME_LENGTH = 200;
const SKIPPED_CLAIM_CODE = "conn-claim-skipped";

export interface AnalyzeRequest {
  worktree: string;
  canonical_remote: string;
  tsconfig_path?: string;
  paths?: string[];
}

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
  relation: "affects" | "produces" | "consumes";
  provenance_kind: "static_analysis";
  evidence_refs: EvidenceRef[];
}

export interface Diagnostic {
  code: string;
  message: string;
  path?: string;
  line?: number;
}

export interface AnalyzerReport {
  report_version: 1;
  repository: { canonical_remote: string };
  claims: Claim[];
  diagnostics: Diagnostic[];
}

/**
 * Analyze configured TypeScript projects locally. The returned report contains
 * identifiers, paths, hashes, and compiler status only. It never includes
 * source text, source excerpts, prompt content, or compiler message text.
 */
export function analyze(request: AnalyzeRequest): AnalyzerReport {
  validateRequest(request);
  const worktree = resolve(request.worktree);
  const selectedPaths = new Set(
    (request.paths ?? []).map((path) => normalizedRelative(worktree, path)),
  );
  const diagnostics: Diagnostic[] = [];
  const claims = new Map<string, Claim>();
  const configs = projectConfigs(worktree, request.tsconfig_path);

  for (const configPath of configs) {
    if (claims.size >= MAX_CLAIMS || diagnostics.length >= MAX_DIAGNOSTICS) break;
    const parsed = readConfig(configPath, diagnostics, worktree);
    if (parsed === undefined) continue;
    // Building a program is the expensive step. With selected paths, skip a
    // project that contains none of them, and its unrelated diagnostics.
    if (selectedPaths.size > 0 && !includesSelectedPath(parsed.fileNames, worktree, selectedPaths))
      continue;
    const program = ts.createProgram({ rootNames: parsed.fileNames, options: parsed.options });
    for (const diagnostic of ts.getPreEmitDiagnostics(program))
      addDiagnostic(diagnostics, diagnostic, worktree);

    const output: ClaimOutput = { claims, diagnostics };
    for (const sourceFile of program.getSourceFiles()) {
      if (sourceFile.isDeclarationFile || !isRepositorySource(program, worktree, sourceFile))
        continue;
      const path = normalizedRelative(worktree, sourceFile.fileName);
      if (selectedPaths.size > 0 && !selectedPaths.has(path)) continue;
      collectExports(program, sourceFile, path, worktree, output);
      collectImports(program, sourceFile, path, worktree, output);
      if (claims.size >= MAX_CLAIMS) break;
    }
  }

  return {
    report_version: 1,
    repository: { canonical_remote: request.canonical_remote },
    claims: [...claims.values()].sort((left, right) =>
      left.normalized_subject.localeCompare(right.normalized_subject),
    ),
    diagnostics: diagnostics.sort((left, right) =>
      `${left.code}:${left.path ?? ""}:${left.line ?? 0}`.localeCompare(
        `${right.code}:${right.path ?? ""}:${right.line ?? 0}`,
      ),
    ),
  };
}

function validateRequest(request: AnalyzeRequest): void {
  if (!request || typeof request.worktree !== "string" || request.worktree.length === 0)
    throw new Error("worktree is required");
  if (!/^[a-z0-9][a-z0-9.-]*(\/[A-Za-z0-9._~-]+)+$/.test(request.canonical_remote))
    throw new Error("canonical_remote must be a normalized credential-free Git remote");
  if (
    request.paths !== undefined &&
    (request.paths.length > 500 ||
      request.paths.some(
        (path) => typeof path !== "string" || path.length === 0 || path.length > 1024,
      ))
  )
    throw new Error("paths must contain at most 500 bounded paths");
}

function projectConfigs(worktree: string, configured?: string): string[] {
  if (configured !== undefined) {
    const path = resolve(worktree, configured);
    if (!isInside(worktree, path) || !existsSync(path))
      throw new Error("tsconfig_path must name a file inside the worktree");
    return [path];
  }
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
  if (configs.length === 0) throw new Error("no tsconfig.json was found in the worktree");
  return configs.sort();
}

function readConfig(
  configPath: string,
  diagnostics: Diagnostic[],
  worktree: string,
): ts.ParsedCommandLine | undefined {
  const result = ts.readConfigFile(configPath, ts.sys.readFile);
  if (result.error !== undefined) {
    addDiagnostic(diagnostics, result.error, worktree);
    return undefined;
  }
  const parsed = ts.parseJsonConfigFileContent(
    result.config,
    ts.sys,
    dirname(configPath),
    undefined,
    configPath,
  );
  for (const diagnostic of parsed.errors) addDiagnostic(diagnostics, diagnostic, worktree);
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

interface ClaimOutput {
  claims: Map<string, Claim>;
  diagnostics: Diagnostic[];
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

function collectExports(
  program: ts.Program,
  file: ts.SourceFile,
  path: string,
  worktree: string,
  output: ClaimOutput,
): void {
  const checker = program.getTypeChecker();
  const moduleSymbol = checker.getSymbolAtLocation(file);
  if (moduleSymbol === undefined) return;
  for (const symbol of checker.getExportsOfModule(moduleSymbol)) {
    const declaration = symbol.valueDeclaration ?? symbol.declarations?.[0];
    if (declaration === undefined) continue;
    const name = checker.symbolToString(symbol);
    const reference = `${subjectPath(path)}/${stableName(name)}`;
    // `export *` can surface a declaration outside the worktree. Its location
    // must not be disclosed, so cite the re-exporting module instead.
    const declared = declaration.getSourceFile();
    const evidence = isInside(worktree, declared.fileName)
      ? evidenceFor(declared, declaration.getStart(), worktree, name)
      : evidenceFor(file, 0, worktree, name);
    addClaim(output, {
      subject_kind: "code_symbol",
      normalized_subject: `symbol:typescript:${reference}`,
      display_name: name,
      relation: "affects",
      provenance_kind: "static_analysis",
      evidence_refs: [evidence],
    });
    addClaim(output, {
      subject_kind: "contract",
      normalized_subject: `contract:typescript:${reference}`,
      display_name: name,
      relation: "produces",
      provenance_kind: "static_analysis",
      evidence_refs: [evidence],
    });
  }
}

function collectImports(
  program: ts.Program,
  file: ts.SourceFile,
  path: string,
  worktree: string,
  output: ClaimOutput,
): void {
  const checker = program.getTypeChecker();
  for (const statement of file.statements) {
    if (!ts.isImportDeclaration(statement) || !ts.isStringLiteral(statement.moduleSpecifier))
      continue;
    const moduleSymbol = checker.getSymbolAtLocation(statement.moduleSpecifier);
    const target = moduleSymbol?.declarations?.[0]?.getSourceFile();
    if (target === undefined || !isRepositorySource(program, worktree, target)) continue;
    const targetPath = subjectPath(normalizedRelative(worktree, target.fileName));
    const bindings = importBindings(statement.importClause);
    for (const name of bindings) {
      const resolved = checker
        .getExportsOfModule(moduleSymbol ?? ({} as ts.Symbol))
        .find((symbol) => symbol.name === name);
      const stable = stableName(resolved === undefined ? name : checker.symbolToString(resolved));
      const reference = `${targetPath}/${stable}`;
      addClaim(output, {
        subject_kind: "contract",
        normalized_subject: `contract:typescript:${reference}`,
        display_name: name,
        relation: "consumes",
        provenance_kind: "static_analysis",
        evidence_refs: [evidenceFor(file, statement.getStart(), worktree, `${path}/${name}`)],
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
 * cannot invalidate the whole report.
 */
function addClaim(output: ClaimOutput, claim: Claim): void {
  const { claims, diagnostics } = output;
  const key = `${claim.normalized_subject} ${claim.relation}`;
  if (claims.size >= MAX_CLAIMS || claims.has(key)) return;
  const evidence = claim.evidence_refs[0];
  if (
    claim.normalized_subject.length > MAX_SUBJECT_LENGTH ||
    (evidence !== undefined &&
      (evidence.locator.length > MAX_LOCATOR_LENGTH || evidence.path.length > MAX_PATH_LENGTH))
  ) {
    if (diagnostics.length < MAX_DIAGNOSTICS) {
      const diagnostic: Diagnostic = {
        code: SKIPPED_CLAIM_CODE,
        message: "A claim exceeded the Conn reference length bounds and was omitted.",
      };
      if (evidence !== undefined && evidence.path.length <= MAX_PATH_LENGTH) {
        diagnostic.path = evidence.path;
        diagnostic.line = evidence.line;
      }
      diagnostics.push(diagnostic);
    }
    return;
  }
  claims.set(key, { ...claim, display_name: displayName(claim.display_name) });
}

function displayName(value: string): string {
  const bounded = value.slice(0, MAX_DISPLAY_NAME_LENGTH);
  return bounded.trim() === "" ? "module" : bounded;
}

function evidenceFor(
  file: ts.SourceFile,
  position: number,
  worktree: string,
  locator: string,
): EvidenceRef {
  const path = normalizedRelative(worktree, file.fileName);
  return {
    kind: "source_location",
    locator: `typescript:${subjectPath(path)}:${stableName(locator)}`,
    path,
    line: file.getLineAndCharacterOfPosition(position).line + 1,
    content_hash: createHash("sha256").update(readFileSync(file.fileName)).digest("hex"),
  };
}

function addDiagnostic(output: Diagnostic[], diagnostic: ts.Diagnostic, worktree: string): void {
  if (output.length >= MAX_DIAGNOSTICS) return;
  const item: Diagnostic = {
    code: `TS${diagnostic.code}`,
    message: `TypeScript diagnostic TS${diagnostic.code}`,
  };
  if (diagnostic.file !== undefined && isInside(worktree, diagnostic.file.fileName)) {
    item.path = normalizedRelative(worktree, diagnostic.file.fileName);
    item.line = diagnostic.file.getLineAndCharacterOfPosition(diagnostic.start ?? 0).line + 1;
  }
  output.push(item);
}

function normalizedRelative(worktree: string, path: string): string {
  const value = relative(worktree, resolve(worktree, path)).split(sep).join("/");
  if (value === "" || value.startsWith("../") || value === "..")
    throw new Error("path must stay inside the worktree");
  return value;
}

function isInside(worktree: string, path: string): boolean {
  const relativePath = relative(worktree, resolve(path));
  return (
    relativePath !== "" &&
    !relativePath.startsWith(`..${sep}`) &&
    relativePath !== ".." &&
    !relativePath.startsWith("../")
  );
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
