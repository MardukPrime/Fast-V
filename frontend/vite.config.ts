import { defineConfig } from "vite";
import solid from "vite-plugin-solid";
import wails from "@wailsio/runtime/plugins/vite";

export default defineConfig({
  base: "./",
  plugins: [solid(), wails("./bindings")],
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    // Android System WebView versions still in the wild can be older than the
    // default "modules" target; es2019 keeps optional chaining etc. away from
    // the parser while remaining fast on modern WebViews.
    target: "es2019",
  },
});
