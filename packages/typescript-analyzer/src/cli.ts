import { readFileSync } from "node:fs";
import { type AnalyzeRequest, AnalyzerRequestError, analyze } from "./index.js";

const MAX_INPUT_BYTES = 64 * 1024;

try {
  const input = readFileSync(0);
  if (input.byteLength > MAX_INPUT_BYTES)
    throw new AnalyzerRequestError("analyzer request exceeds 65536 bytes");
  let request: AnalyzeRequest;
  try {
    request = JSON.parse(input.toString("utf8")) as AnalyzeRequest;
  } catch {
    // JSON.parse messages quote part of the input.
    throw new AnalyzerRequestError("analyzer request is not valid JSON");
  }
  process.stdout.write(`${JSON.stringify(analyze(request))}\n`);
} catch (error) {
  // Do not echo stdin or compiler text: either may contain local source data.
  // Request errors carry fixed messages; anything else is reported by kind only.
  const message =
    error instanceof AnalyzerRequestError
      ? error.message
      : `analysis failed (${error instanceof Error ? error.name : "unknown error"})`;
  process.stderr.write(`typescript analyzer: ${message.slice(0, 500)}\n`);
  process.exitCode = 1;
}
