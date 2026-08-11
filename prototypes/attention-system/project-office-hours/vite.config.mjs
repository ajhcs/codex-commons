import { fileURLToPath } from "node:url";

const dependency = (path) => fileURLToPath(new URL(`../../../web/node_modules/${path}`, import.meta.url));

export default {
  esbuild: {
    jsx: "automatic",
  },
  resolve: {
    alias: [
      { find: /^react$/, replacement: dependency("react/index.js") },
      { find: /^react\/jsx-runtime$/, replacement: dependency("react/jsx-runtime.js") },
      { find: /^react\/jsx-dev-runtime$/, replacement: dependency("react/jsx-dev-runtime.js") },
      { find: /^react-dom$/, replacement: dependency("react-dom/index.js") },
      { find: /^react-dom\/client$/, replacement: dependency("react-dom/client.js") },
    ],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    host: "127.0.0.1",
  },
};
