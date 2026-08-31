import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // Relative base so a built bundle works from a subdirectory or straight off
  // disk, not only from a server root. Note this is why routing is by query
  // parameter rather than path — see src/route.ts.
  base: "./",
  server: {
    proxy: {
      // Mirrors what the Cloudflare Worker does in production: strip /api and
      // forward to the Go server. Same relative URLs work in both, so nothing
      // in the app needs to know where it is running. Point this at a local
      // `go run ./cmd/api` on 8080, or export VITE_API_TARGET to reach a
      // deployed one.
      "/api": {
        target: process.env.VITE_API_TARGET ?? "http://localhost:8080",
        changeOrigin: true,
        rewrite: (p) => p.replace(/^\/api/, ""),
      },
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
