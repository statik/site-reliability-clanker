import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const backend = "http://127.0.0.1:7070";
const proxy = { "/api": backend, "/git": backend };

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { "@": new URL("./src", import.meta.url).pathname } },
  server: { proxy },
  preview: { proxy },
  build: { outDir: "dist", chunkSizeWarningLimit: 4000 },
  css: { preprocessorOptions: { scss: { quietDeps: true } } },
  test: { environment: "node", include: ["src/**/*.test.{ts,tsx}"] },
});
