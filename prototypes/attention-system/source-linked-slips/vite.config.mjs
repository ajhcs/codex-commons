import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";

const webModules = (path) => fileURLToPath(new URL(`../../../web/node_modules/${path}`, import.meta.url));

export default defineConfig({
  resolve: {
    alias: [
      { find: /^react-dom\/client$/, replacement: webModules("react-dom/client.js") },
      { find: /^react-dom$/, replacement: webModules("react-dom/index.js") },
      { find: /^react$/, replacement: webModules("react/index.js") },
    ],
    dedupe: ["react", "react-dom"],
  },
  server: {
    host: "127.0.0.1",
  },
});
