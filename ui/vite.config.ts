import { defineConfig, type Plugin } from "vite";
import type { OutputAsset } from "rollup";
import react from "@vitejs/plugin-react-swc";
import tailwindcss from "@tailwindcss/vite";
import tsconfigPaths from "vite-tsconfig-paths";
import basicSsl from "@vitejs/plugin-basic-ssl";
import crypto from "node:crypto";

declare const process: {
  env: {
    JETKVM_PROXY_URL: string;
    USE_SSL: string;
  };
};

const toHash = (str: string | Uint8Array<ArrayBufferLike>) => {
  return crypto.createHash("sha256").update(str).digest("hex");
}

const indexHtmlHashPlugin = (): Plugin => ({
  name: "index-html-hash",
  enforce: "post",
  generateBundle(options, bundle) {
    const indexHtml = bundle["index.html"];
    for (const key in bundle) {
      if (key.includes("index-")) {
        console.log(bundle[key].originalFileNames);
      }
    }
    if (indexHtml) {
      bundle["static-hash.json"] = {
        type: "asset",
        source: JSON.stringify({
          hash: toHash((indexHtml as OutputAsset).source),
        }),
        fileName: "static-hash.json",
        needsCodeReference: false,
        name: undefined,
        names: ["static-hash.json"],
        originalFileName: null,
        originalFileNames: ["static-hash.json"],
      };
    }
  }
})

export default defineConfig(({ mode, command }) => {
  const isCloud = mode.indexOf("cloud") !== -1;
  const onDevice = mode === "device";
  const { JETKVM_PROXY_URL, USE_SSL } = process.env;
  const useSSL = USE_SSL === "true";

  const plugins = [
    tailwindcss(),
    tsconfigPaths(),
    react(),
    indexHtmlHashPlugin(),
  ];
  if (useSSL) {
    plugins.push(basicSsl());
  }

  return {
    plugins,
    esbuild: {
      pure: ["console.debug"],
    },
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
