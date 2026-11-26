import { createHash } from "node:crypto";
import ts from "typescript";

export interface ExportFingerprint {
  /** SHA-256 of the declaration text with whitespace runs collapsed. */
  fingerprint: string;
  line: number;
}

interface Part {
  text: string;
  position: number;
}

type NamedPart = [name: string, part: Part];

/** Parses one file version without type checking. */
export function parseSource(fileName: string, content: Buffer): ts.SourceFile {
  const text = content.toString("utf8");
  return ts.createSourceFile(
    fileName,
    text.charCodeAt(0) === 0xfeff ? text.slice(1) : text,
    ts.ScriptTarget.Latest,
  );
}

/**
 * Fingerprints each export of a file by name, syntactically. The base and
 * working-tree versions use this same function, so unchanged declarations
 * yield equal fingerprints. A fingerprint covers bodies as well as
 * signatures, because behavior can change without a signature change.
 * Overloads and merged declarations of one name share a fingerprint.
 * `export *` is not resolved.
 */
export function exportFingerprints(file: ts.SourceFile): Map<string, ExportFingerprint> {
  const locals = new Map<string, Part[]>();
  for (const statement of file.statements)
    for (const [name, part] of localDeclarations(statement, file)) append(locals, name, part);
  const exported = new Map<string, Part[]>();
  for (const statement of file.statements)
    for (const [name, part] of exportedDeclarations(statement, file, locals))
      append(exported, name, part);

  const fingerprints = new Map<string, ExportFingerprint>();
  for (const [name, parts] of exported) {
    const text = parts
      .map((part) => part.text)
      .join("\n")
      .replace(/\s+/g, " ")
      .trim();
    fingerprints.set(name, {
      fingerprint: createHash("sha256").update(text).digest("hex"),
      line: file.getLineAndCharacterOfPosition(parts[0]?.position ?? 0).line + 1,
    });
  }
  return fingerprints;
}

function append(map: Map<string, Part[]>, name: string, part: Part): void {
  const parts = map.get(name);
  if (parts === undefined) map.set(name, [part]);
  else parts.push(part);
}

/** Top-level bindings that a local `export { name }` can refer to. */
function localDeclarations(statement: ts.Statement, file: ts.SourceFile): NamedPart[] {
  if (ts.isVariableStatement(statement)) return variableParts(statement, file);
  if (ts.isImportDeclaration(statement)) return importParts(statement, file);
  const name = declarationName(statement);
  return name === undefined ? [] : [[name, declarationPart(statement, file)]];
}

function exportedDeclarations(
  statement: ts.Statement,
  file: ts.SourceFile,
  locals: ReadonlyMap<string, Part[]>,
): NamedPart[] {
  if (ts.isExportAssignment(statement))
    return statement.isExportEquals ? [] : [["default", declarationPart(statement, file)]];
  if (ts.isExportDeclaration(statement)) return exportDeclarationParts(statement, file, locals);
  if (!hasModifier(statement, ts.SyntaxKind.ExportKeyword)) return [];
  if (hasModifier(statement, ts.SyntaxKind.DefaultKeyword))
    return [["default", declarationPart(statement, file)]];
  if (ts.isVariableStatement(statement)) return variableParts(statement, file);
  const name = declarationName(statement);
  return name === undefined ? [] : [[name, declarationPart(statement, file)]];
}

function exportDeclarationParts(
  statement: ts.ExportDeclaration,
  file: ts.SourceFile,
  locals: ReadonlyMap<string, Part[]>,
): NamedPart[] {
  const clause = statement.exportClause;
  if (clause === undefined) return [];
  if (ts.isNamespaceExport(clause)) return [[clause.name.text, declarationPart(statement, file)]];
  const typeOnly = statement.isTypeOnly ? "type " : "";
  const from = statement.moduleSpecifier?.getText(file);
  return clause.elements.flatMap((element): NamedPart[] => {
    const name = element.name.text;
    const specifier = {
      text: `${typeOnly}${element.getText(file)}`,
      position: element.getStart(file),
    };
    if (from !== undefined)
      return [[name, { ...specifier, text: `${specifier.text} from ${from}` }]];
    const local = locals.get((element.propertyName ?? element.name).text);
    return (local ?? [specifier]).map((part): NamedPart => [name, part]);
  });
}

function declarationName(statement: ts.Statement): string | undefined {
  if (ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement))
    return statement.name?.text;
  if (
    ts.isInterfaceDeclaration(statement) ||
    ts.isTypeAliasDeclaration(statement) ||
    ts.isEnumDeclaration(statement) ||
    ts.isImportEqualsDeclaration(statement)
  )
    return statement.name.text;
  if (
    ts.isModuleDeclaration(statement) &&
    ts.isIdentifier(statement.name) &&
    (statement.flags & ts.NodeFlags.GlobalAugmentation) === 0
  )
    return statement.name.text;
  return undefined;
}

/** Each declarator is its own export; its text keeps the `const`, `let`, or `var` keyword. */
function variableParts(statement: ts.VariableStatement, file: ts.SourceFile): NamedPart[] {
  const list = statement.declarationList;
  const first = list.declarations[0];
  const keyword =
    first === undefined ? "" : file.text.slice(list.getStart(file), first.getStart(file));
  return list.declarations.flatMap((declaration) =>
    bindingNames(declaration.name).map(
      (name): NamedPart => [
        name,
        { text: `${keyword}${declaration.getText(file)}`, position: declaration.getStart(file) },
      ],
    ),
  );
}

function importParts(statement: ts.ImportDeclaration, file: ts.SourceFile): NamedPart[] {
  const clause = statement.importClause;
  if (clause === undefined) return [];
  const from = statement.moduleSpecifier.getText(file);
  const prefix = `import ${clause.isTypeOnly ? "type " : ""}`;
  const parts: NamedPart[] = [];
  if (clause.name !== undefined)
    parts.push([
      clause.name.text,
      { text: `${prefix}${clause.name.text} from ${from}`, position: clause.name.getStart(file) },
    ]);
  const bindings = clause.namedBindings;
  if (bindings !== undefined && ts.isNamespaceImport(bindings))
    parts.push([
      bindings.name.text,
      {
        text: `${prefix}* as ${bindings.name.text} from ${from}`,
        position: bindings.getStart(file),
      },
    ]);
  if (bindings !== undefined && ts.isNamedImports(bindings))
    for (const element of bindings.elements)
      parts.push([
        element.name.text,
        {
          text: `${prefix}{ ${element.getText(file)} } from ${from}`,
          position: element.getStart(file),
        },
      ]);
  return parts;
}

function bindingNames(name: ts.BindingName): string[] {
  if (ts.isIdentifier(name)) return [name.text];
  return (name.elements as readonly ts.ArrayBindingElement[]).flatMap((element) =>
    ts.isOmittedExpression(element) ? [] : bindingNames(element.name),
  );
}

/**
 * The declaration's own text without its `export` and `default` keywords, so
 * that `export function f` and `function f` plus `export { f }` agree.
 */
function declarationPart(node: ts.Node, file: ts.SourceFile): Part {
  const start = node.getStart(file);
  let text = file.text.slice(start, node.getEnd());
  const modifiers = ts.canHaveModifiers(node) ? (ts.getModifiers(node) ?? []) : [];
  for (const modifier of [...modifiers].reverse()) {
    if (
      modifier.kind !== ts.SyntaxKind.ExportKeyword &&
      modifier.kind !== ts.SyntaxKind.DefaultKeyword
    )
      continue;
    text = text.slice(0, modifier.getStart(file) - start) + text.slice(modifier.getEnd() - start);
  }
  return { text, position: start };
}

function hasModifier(node: ts.Node, kind: ts.SyntaxKind): boolean {
  return (
    ts.canHaveModifiers(node) &&
    (ts.getModifiers(node)?.some((modifier) => modifier.kind === kind) ?? false)
  );
}
