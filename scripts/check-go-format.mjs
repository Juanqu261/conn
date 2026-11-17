import { execFileSync, spawnSync } from "node:child_process";

const output = execFileSync(
  "git",
  ["ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go"],
  { encoding: "utf8" },
);
const files = output
  .split(/\r?\n/u)
  .map((file) => file.trim())
  .filter(Boolean);

if (files.length === 0) {
  process.exit(0);
}

const result = spawnSync("gofmt", ["-l", ...files], {
  encoding: "utf8",
  shell: false,
});

if (result.error) {
  throw result.error;
}
if (result.status !== 0) {
  process.stderr.write(result.stderr);
  process.exit(result.status ?? 1);
}
if (result.stdout.trim()) {
  process.stderr.write(`The following Go files require formatting:\n${result.stdout}`);
  process.exit(1);
}
