import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import tsconfigPaths from "vite-tsconfig-paths";

declare const process: {
  env: {
    JETKVM_PROXY_URL: string;
  };
};

export default defineConfig(({ mode, command }) => {
  const isCloud = mode === "production";
  const onDevice = mode === "device";
  const { JETKVM_PROXY_URL } = process.env;

  return {
    plugins: [tsconfigPaths(), react()],
    build: { outDir: isCloud ? "dist" : "../static" },
    server: {
      host: "0.0.0.0",
      proxy: JETKVM_PROXY_URL ? {
        '/me': JETKVM_PROXY_URL,
        '/device': JETKVM_PROXY_URL,
        '/webrtc': JETKVM_PROXY_URL,
        '/auth': JETKVM_PROXY_URL,
        '/storage': JETKVM_PROXY_URL,
        '/cloud': JETKVM_PROXY_URL,
      } : undefined
    },
    base: onDevice && command === 'build' ? "/static" : "/",
  };
});
