// Empties internal/webui/site before a build, keeping the committed
// .gitkeep that lets conn compile without a web build.
import { readdirSync, rmSync } from "node:fs";
import { join } from "node:path";

const site = join(import.meta.dirname, "..", "..", "..", "internal", "webui", "site");
for (const entry of readdirSync(site)) {
  if (entry !== ".gitkeep") {
    rmSync(join(site, entry), { recursive: true, force: true });
  }
}
