import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // Relative base so a built bundle works from a subdirectory or straight off
  // disk, not only from a server root.
  base: "./",
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
