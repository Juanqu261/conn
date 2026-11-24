import { readFileSync } from "node:fs";
import { type AnalyzeRequest, analyze } from "./index.js";

const MAX_INPUT_BYTES = 64 * 1024;

try {
  const input = readFileSync(0);
  if (input.byteLength > MAX_INPUT_BYTES) throw new Error("analyzer request exceeds 65536 bytes");
  const request = JSON.parse(input.toString("utf8")) as AnalyzeRequest;
  process.stdout.write(`${JSON.stringify(analyze(request))}\n`);
} catch (error) {
  const message = error instanceof Error ? error.message : "invalid analyzer request";
  // Do not echo stdin or compiler text: either may contain local source data.
  process.stderr.write(`typescript analyzer: ${message.slice(0, 500)}\n`);
  process.exitCode = 1;
}
