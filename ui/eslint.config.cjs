const {
    defineConfig,
    globalIgnores,
} = require("eslint/config");

const globals = require("globals");

const {
    fixupConfigRules,
} = require("@eslint/compat");

const tsParser = require("@typescript-eslint/parser");
const reactRefresh = require("eslint-plugin-react-refresh");
const js = require("@eslint/js");

const {
    FlatCompat,
} = require("@eslint/eslintrc");

const compat = new FlatCompat({
    baseDirectory: __dirname,
    recommendedConfig: js.configs.recommended,
    allConfig: js.configs.all
});

module.exports = defineConfig([{
    languageOptions: {
        globals: {
            ...globals.browser,
        },

        parser: tsParser,
        ecmaVersion: "latest",
        sourceType: "module",

        parserOptions: {
            project: ["./tsconfig.json", "./tsconfig.node.json"],
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
        "plugin:react-hooks/recommended",
        "plugin:react/recommended",
        "plugin:react/jsx-runtime",
        "plugin:import/recommended",
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
            "newlines-between": "always",
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
                    ["@assets", "./src/assets"],
                    ["@", "./src"],
                ],

                extensions: [".ts", ".tsx", ".js", ".jsx", ".json"],
            },
        },
    },
}, globalIgnores([
    "**/dist",
    "**/.eslintrc.cjs",
    "**/tailwind.config.js",
    "**/postcss.config.js",
])]);
