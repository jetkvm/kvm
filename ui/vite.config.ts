import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import tsconfigPaths from "vite-tsconfig-paths";

export default defineConfig(({ mode }) => {
  const isCloud = mode === "production";
  const onDevice = mode === "device";
  return {
    plugins: [tsconfigPaths(), react()],
    build: { outDir: isCloud ? "dist" : "../static" },
    server: { host: "0.0.0.0" },
    base: onDevice ? "/static" : "/",
  };
});
