import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  build: {
    chunkSizeWarningLimit: 700,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (id.includes("vuetify")) return "vuetify";
          if (id.includes("chart.js") || id.includes("vue-chartjs"))
            return "chartjs";
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://localhost:8080",
      "/admin": "http://localhost:8080",
    },
  },
});
