import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import tailwindcss from "@tailwindcss/vite";
import tsconfigPaths from "vite-tsconfig-paths";
import basicSsl from "@vitejs/plugin-basic-ssl";

declare const process: {
  env: {
    JETKVM_PROXY_URL: string;
    USE_SSL: string;
  };
};

export default defineConfig(({ mode, command }) => {
  const isCloud = mode.indexOf("cloud") !== -1;
  const onDevice = mode === "device";
  const { JETKVM_PROXY_URL, USE_SSL } = process.env;
  const useSSL = USE_SSL === "true";

  const plugins = [
    tailwindcss(),
    tsconfigPaths(),
    react()
  ];
  if (useSSL) {
    plugins.push(basicSsl());
  }

  return {
    plugins,
    build: { outDir: isCloud ? "dist" : "../static" },
    server: {
      host: "0.0.0.0",
      https: useSSL,
      proxy: JETKVM_PROXY_URL
        ? {
            "/me": JETKVM_PROXY_URL,
            "/device": JETKVM_PROXY_URL,
            "/webrtc": JETKVM_PROXY_URL,
            "/auth": JETKVM_PROXY_URL,
            "/storage": JETKVM_PROXY_URL,
            "/cloud": JETKVM_PROXY_URL,
            "/developer": JETKVM_PROXY_URL,
          }
        : undefined,
    },
    base: onDevice && command === "build" ? "/static" : "/",
  };
});
