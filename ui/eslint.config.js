import globals from "globals";
import { defineConfig, globalIgnores } from "eslint/config";
import { fixupConfigRules } from "@eslint/compat";
import js from "@eslint/js";
import { FlatCompat } from "@eslint/eslintrc";
import path from "path";
import { fileURLToPath } from "url";

import tsParser from "@typescript-eslint/parser";
import reactRefresh from "eslint-plugin-react-refresh";

// mimic CommonJS variables
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const compat = new FlatCompat({
    baseDirectory: __dirname,
    recommendedConfig: js.configs.recommended,
    allConfig: js.configs.all
});

export default defineConfig([
  {
    languageOptions: {
        globals: globals.browser,
        parser: tsParser,
        ecmaVersion: "latest",
        sourceType: "module",
        parserOptions: {
            project: ["./tsconfig.app.json", "./tsconfig.node.json"],
            tsconfigRootDir: __dirname,
            ecmaFeatures: {
                jsx: true
            }
        },
    },
    extends: fixupConfigRules(compat.extends(
        "eslint:recommended",
        "plugin:@typescript-eslint/recommended",
        "plugin:@typescript-eslint/stylistic",
        "plugin:react/recommended",
        "plugin:react-hooks/recommended",
        "plugin:react/jsx-runtime",
        "plugin:import/recommended",
        "plugin:prettier/recommended",
        "prettier",
    )),
    plugins: {
        "react-refresh": reactRefresh,
    },
    rules: {
        "react-refresh/only-export-components": ["warn", {
            allowConstantExport: true,
        }],

        "import/order": ["error", {
            groups: ["builtin", "external", "internal", "parent", "sibling"],
            "newlines-between": "ignore",
        }],

        "@typescript-eslint/no-unused-vars": ["warn", {
            "argsIgnorePattern": "^_", "varsIgnorePattern": "^_"
        }],
    },
    settings: {
        "react": {
            "version": "detect"
        },
        "import/resolver": {
            alias: {
                map: [
                    ["@components", "./src/components"],
                    ["@routes", "./src/routes"],
                    ["@hooks", "./src/hooks"],
                    ["@providers", "./src/providers"],
                    ["@assets", "./src/assets"],
                    ["@localizations", "./localization/paraglide"],
                    ["@", "./src"],
                ],

                extensions: [".ts", ".tsx", ".js", ".jsx", ".json"],
            },
        },
    },
},
globalIgnores([
    "**/dist",
    "**/tailwind.config.js",
    "**/postcss.config.js",
])]);
