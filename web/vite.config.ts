import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: { proxy: { "/api": { target: "http://localhost:8080", changeOrigin: true } } },
  build: {
    outDir: fileURLToPath(new URL("../internal/web/dist", import.meta.url)),
    emptyOutDir: true,
  },
  plugins: [react(), tailwindcss()],
});
