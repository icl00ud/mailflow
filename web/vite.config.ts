import path from "node:path";
import { defineConfig, type ProxyOptions } from "vite";
import react from "@vitejs/plugin-react";

// The panel is embedded by Go from internal/server/web/app. Vite writes the
// hashed bundle there so `go build` stays a single static binary. In dev the
// proxy forwards the API and the server-rendered auth pages to :8080 and
// rewrites Origin so the Go CSRF check sees PUBLIC_ORIGIN.
const target = "http://localhost:8080";
const rewriteOrigin: ProxyOptions = {
  target,
  changeOrigin: true,
  configure(proxy) {
    proxy.on("proxyReq", (req) => req.setHeader("Origin", target));
  },
};

export default defineConfig(({ command }) => ({
  plugins: [react()],
  // Production mounts the bundle under /app so it cannot shadow API routes;
  // in dev the SPA stays at / and the proxy forwards the API/auth pages.
  base: command === "build" ? "/app/" : "/",
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": rewriteOrigin,
      "/login": rewriteOrigin,
      "/logout": rewriteOrigin,
      "/invite": rewriteOrigin,
      "/static": rewriteOrigin,
    },
  },
  build: {
    outDir: "../internal/server/web/app",
    emptyOutDir: true,
    sourcemap: false,
    cssCodeSplit: false,
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom", "react-router-dom"],
          flow: ["@xyflow/react"],
        },
      },
    },
  },
}));
