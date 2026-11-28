// pnpm dev: the whole Conn stack for local development, at one address.
//
//   1. checks .env for the GitHub OAuth values Conn Lab needs;
//   2. starts PostgreSQL (Docker Compose) and applies Conn migrations;
//   3. runs conn server on 127.0.0.1:8080 and the Conn Lab dev server on
//      127.0.0.1:5173, which proxies /v1 to conn server, so the Lab, its
//      session cookie, the GitHub callback, and the event stream share one
//      origin: http://127.0.0.1:5173.
//
// Ctrl+C stops both servers. PostgreSQL keeps running; pnpm db:stop stops it.

import { spawn, spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dirname, "..");
const LAB_URL = "http://127.0.0.1:5173";
const SERVER_URL = "http://127.0.0.1:8080";
const CALLBACK_URL = `${LAB_URL}/v1/auth/github/web/callback`;
const windows = process.platform === "win32";

const say = (line = "") => process.stdout.write(`${line}\n`);
const fail = (lines) => {
  process.stderr.write(`\n${lines.join("\n")}\n\n`);
  process.exit(1);
};

// 1. Settings. Variables already set in the shell win over .env, as in conn.
const envFile = join(root, ".env");
if (!existsSync(envFile)) {
  fail([
    "No .env file yet.",
    "  Copy .env.example to .env, fill in CONN_GITHUB_CLIENT_ID and CONN_GITHUB_CLIENT_SECRET,",
    "  then run pnpm dev again.",
  ]);
}
const fileValues = {};
for (const line of readFileSync(envFile, "utf8").split(/\r?\n/u)) {
  const match = /^\s*(CONN_[A-Z0-9_]+)\s*=\s*(.*?)\s*$/u.exec(line);
  if (match?.[1]) {
    fileValues[match[1]] = (match[2] ?? "").replace(/^(["'])(.*)\1$/u, "$2");
  }
}
const value = (key) => (process.env[key] ?? fileValues[key] ?? "").trim();
const missing = ["CONN_GITHUB_CLIENT_ID", "CONN_GITHUB_CLIENT_SECRET"].filter((key) => !value(key));
if (missing.length > 0) {
  fail([
    `.env is missing ${missing.join(" and ")}.`,
    "  Both come from your GitHub OAuth app (GitHub > Settings > Developer settings > OAuth Apps):",
    "  the Client ID, and a client secret you generate there.",
    `  Set the app's "Authorization callback URL" to ${CALLBACK_URL}`,
  ]);
}

// 2. Database. pnpm is a .cmd shim on Windows, which only a shell can start;
// these are fixed commands with no outside input, so each goes to the shell
// as one string.
const run = (title, command, hint) => {
  say(`• ${title}`);
  const result = Array.isArray(command)
    ? spawnSync(command[0], command.slice(1), { cwd: root, stdio: "inherit" })
    : spawnSync(command, { cwd: root, stdio: "inherit", shell: true });
  if (result.status !== 0) {
    fail([`${title} failed. ${hint}`]);
  }
};
run("Starting PostgreSQL", "pnpm db:start", "Is Docker running?");
run("Applying Conn migrations", "pnpm db:migrate", "Is PostgreSQL healthy? Try pnpm db:status.");

// 3. Servers. Each child is the real server process, with no shell or shim
// in between, so stopping a child stops its server. conn is a development
// build, so it reads the rest of .env itself; the public URL is fixed here
// so it always matches the Lab dev server.
const conn = join(root, "node_modules", ".cache", "conn-dev", windows ? "conn.exe" : "conn");
run("Building conn", ["go", "build", "-o", conn, "./cmd/conn"], "See the Go errors above.");
const vite = join(root, "apps", "web", "node_modules", "vite", "bin", "vite.js");

const children = [];
const start = (name, file, args, cwd, env) => {
  const child = spawn(file, args, {
    cwd,
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const prefix = `[${name}] `;
  for (const stream of [child.stdout, child.stderr]) {
    let buffered = "";
    stream.on("data", (chunk) => {
      buffered += chunk.toString();
      const lines = buffered.split(/\r?\n/u);
      buffered = lines.pop() ?? "";
      for (const line of lines) {
        if (line.trim()) say(`${prefix}${line}`);
      }
    });
  }
  child.on("exit", (code) => {
    if (!stopping) {
      say();
      say(`${name} stopped (exit ${code ?? "?"}). Stopping the others.`);
      stop(1);
    }
  });
  children.push(child);
};

let stopping = false;
const stop = (code = 0) => {
  if (stopping) return;
  stopping = true;
  for (const child of children) {
    if (child.exitCode === null) child.kill("SIGINT");
  }
  process.exit(code);
};
process.on("SIGINT", () => stop(0));
process.on("SIGTERM", () => stop(0));

say("• Starting conn server and Conn Lab");
start("server", conn, ["server", "--listen", "127.0.0.1:8080"], root, { CONN_PUBLIC_URL: LAB_URL });
start("lab", process.execPath, [vite], join(root, "apps", "web"), { CONN_LAB_API: SERVER_URL });

const ready = async (url) => {
  for (let attempt = 0; attempt < 360 && !stopping; attempt++) {
    try {
      if ((await fetch(url)).ok) return true;
    } catch {
      // not listening yet
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  return false;
};

if ((await ready(`${SERVER_URL}/readyz`)) && (await ready(LAB_URL))) {
  say();
  say("  ⚡ Conn Lab is alive");
  say(`     Open            ${LAB_URL}`);
  say(`     GitHub callback ${CALLBACK_URL}`);
  say("     (the OAuth app's Authorization callback URL must be exactly this)");
  say(`     Terminal clients use conn server directly at ${SERVER_URL}`);
  say("  Ctrl+C stops both servers.");
  say();
} else if (!stopping) {
  process.stderr.write("\nconn server or the Lab did not become ready within three minutes.\n");
  stop(1);
}
