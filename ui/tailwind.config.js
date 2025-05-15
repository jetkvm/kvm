import flattenColorPalette from "tailwindcss/lib/util/flattenColorPalette";
import svgToDataUri from "mini-svg-data-uri";
import plugin from "tailwindcss/plugin";

/** @type {import("tailwindcss").Config} */
export default {
  darkMode: "selector",
  plugins: [
    require("@tailwindcss/forms"),
    plugin(function ({ addVariant }) {
      addVariant("disabled-within", `&:has(input:is(:disabled),button:is(:disabled))`);
    }),
    plugin(function ({ addVariant }) {
      addVariant("invalid-within", `&:has(input:is(:invalid))`);
    }),
    function ({ matchUtilities, theme }) {
      matchUtilities(
        {
          "bg-grid-sm": value => ({
            backgroundImage: `url("${svgToDataUri(
              `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="${value}"><path d="M0 .5H23.5V24"/></svg>`,
            )}")`,
          }),
        },
        { values: flattenColorPalette(theme("backgroundColor")), type: "color" },
      );
    },
    function ({ matchUtilities, theme }) {
      matchUtilities(
        {
          "bg-grid": value => ({
            backgroundImage: `url("${svgToDataUri(
              `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" width="32" height="32" fill="none" stroke="${value}"><path d="M0 .5H31.5V32"/></svg>`,
            )}")`,
          }),
        },
        { values: flattenColorPalette(theme("backgroundColor")), type: "color" },
      );
    },
    function ({ matchUtilities, theme }) {
      matchUtilities(
        {
          "bg-grid-lg": value => ({
            backgroundImage: `url("${svgToDataUri(
              `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" width="128" height="128" fill="none" stroke="${value}"><path d="M0 .5H127.5V127"/></svg>`,
            )}")`,
          }),
        },
        {
          values: flattenColorPalette(theme("backgroundColor")),
          type: "color",
        },
      );
    },
  ],
};
