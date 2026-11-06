import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// https://vitejs.dev/config/
export default defineConfig({
  root: fileURLToPath(new URL(".", import.meta.url)),
  resolve: { alias: { "@": fileURLToPath(new URL("./src/web", import.meta.url)) } },
  plugins: [react()],
});
