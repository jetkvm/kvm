import defaultTheme from "tailwindcss/defaultTheme";
import flattenColorPalette from "tailwindcss/lib/util/flattenColorPalette";
import svgToDataUri from "mini-svg-data-uri";
import plugin from "tailwindcss/plugin";

/** @type {import("tailwindcss").Config} */
export default {
  content: ["./src/**/*.{ts,tsx,svg}", "./index.html"],
  darkMode: "selector",
  theme: {
    extend: {
      gridTemplateRows: {
        layout: "auto 1fr auto",
        headerBody: "auto 1fr",
        bodyFooter: "1fr auto",
      },
      gridTemplateColumns: {
        sidebar: "1fr minmax(360px, 25%)",
      },
      screens: {
        xs: "480px",
        "2xl": "1440px",
        "3xl": "1920px",
        "4xl": "2560px",
      },
      fontFamily: {
        sans: ["Circular", ...defaultTheme.fontFamily.sans],
        display: ["Circular", ...defaultTheme.fontFamily.sans],
        mono: ["Source Code Pro Variable", ...defaultTheme.fontFamily.mono],
      },
      maxWidth: {
        "8xl": "88rem",
        "9xl": "96rem",
        "10xl": "104rem",
        "11xl": "112rem",
        "12xl": "120rem",
      },
      animation: {
        enter: "enter .2s ease-out",
        leave: "leave .15s ease-in forwards",
        fadeInScale: "fadeInScale 1s ease-out forwards",
        fadeInScaleFloat:
          "fadeInScaleFloat 1s ease-out forwards, float 3s ease-in-out infinite",
        fadeIn: "fadeIn 1s ease-out forwards",
        slideUpFade: "slideUpFade 1s ease-out forwards",
      },
      animationDelay: {
        1000: "1000ms",
        1500: "1500ms",
      },
      keyframes: {
        enter: {
          "0%": {
            opacity: "0",
            transform: "scale(.9)",
          },
          "100%": {
            opacity: "1",
            transform: "scale(1)",
          },
        },
        leave: {
          "0%": {
            opacity: "1",
            transform: "scale(1)",
          },
          "100%": {
            opacity: "0",
            transform: "scale(.9)",
          },
        },
        fadeInScale: {
          "0%": { opacity: "0", transform: "scale(0.98)" },
          "100%": { opacity: "1", transform: "scale(1)" },
        },
        fadeInScaleFloat: {
          "0%": { opacity: "0", transform: "scale(0.98) translateY(10px)" },
          "100%": { opacity: "1", transform: "scale(1) translateY(0)" },
        },
        float: {
          "0%, 100%": { transform: "translateY(0)" },
          "50%": { transform: "translateY(-10px)" },
        },
        fadeIn: {
          "0%": { opacity: "0", transform: "translateY(10px)" },
          "70%": { opacity: "0.8", transform: "translateY(1px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
        slideUpFade: {
          "0%": { opacity: "0", transform: "translateY(20px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
      },
    },
  },
  plugins: [
    require("@tailwindcss/forms"),
    require("@tailwindcss/typography"),
    require("@headlessui/tailwindcss"),
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
    function ({ addUtilities, theme }) {
      const animationDelays = theme("animationDelay");
      const utilities = Object.entries(animationDelays).map(([key, value]) => ({
        [`.animation-delay-${key}`]: { animationDelay: value },
      }));
      addUtilities(utilities);
    },
  ],
};
