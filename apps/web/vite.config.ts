import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Conn Lab builds into internal/webui/site, which conn server embeds
// (decision 0013). In development the Vite server proxies /v1 to a native
// conn server, so the Lab, its cookies, and the event stream share one
// origin: set CONN_PUBLIC_URL=http://127.0.0.1:5173 on that server.
const api = process.env.CONN_LAB_API ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../../internal/webui/site",
    // scripts/clean-site.mjs empties the folder but keeps .gitkeep.
    emptyOutDir: false,
    assetsDir: "assets",
    // Never inline assets as data: URIs; the Content-Security-Policy allows
    // fonts and images only from this origin.
    assetsInlineLimit: 0,
    target: "es2022",
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/v1": { target: api, changeOrigin: false },
    },
  },
});
