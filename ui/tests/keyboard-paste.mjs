// Unit checks for the paste helpers: text to key presses, and which key events
// are the client's paste shortcuts, a dictation tool's paste, or typed text.
// Run with: node tests/keyboard-paste.mjs
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";

const src = p => fileURLToPath(new URL(`../src/${p}`, import.meta.url));
const bundle = await rolldown({
  input: "\0entry",
  plugins: [
    {
      name: "paste-test-entry",
      resolveId(source, importer) {
        if (source === "\0entry") return source;
        if (source === "@/keyboardPaste") return src("keyboardPaste.ts");
        if (source === "@/keyboardLayouts/en_US") return src("keyboardLayouts/en_US.ts");
        if (source === "../keyboardLayouts" && importer?.includes("keyboardLayouts/"))
          return src("keyboardLayouts.ts");
        if (source.startsWith("@/keyboardLayouts/")) return src(`${source.slice(2)}.ts`);
      },
      load(id) {
        if (id === "\0entry")
          return `export * from "@/keyboardPaste"; export { en_US } from "@/keyboardLayouts/en_US";`;
      },
    },
  ],
});
const { output } = await bundle.generate({ format: "esm" });
const paste = await import(
  `data:text/javascript;base64,${Buffer.from(output[0].code).toString("base64")}`
);
const { textToMacroSteps, isClientPasteShortcut, isInjectedPasteChord, isTypedCharacter, en_US } =
  paste;

// Text becomes one press per character, with Shift where the layout needs it,
// Enter for a newline, and characters the layout lacks reported once.
const { steps, skipped } = textToMacroSteps("Hi\n€€", en_US, 5);
assert.deepEqual(steps, [
  { keys: ["KeyH"], modifiers: ["ShiftLeft"], delay: 5 },
  { keys: ["KeyI"], modifiers: null, delay: 5 },
  { keys: ["Enter"], modifiers: null, delay: 5 },
]);
assert.deepEqual(skipped, ["€"]);

const key = o => ({
  key: "",
  code: "",
  ctrlKey: false,
  shiftKey: false,
  altKey: false,
  metaKey: false,
  ...o,
});
const ctrlV = key({ key: "v", code: "KeyV", ctrlKey: true });
const ctrlShiftV = key({ key: "V", code: "KeyV", ctrlKey: true, shiftKey: true });
const cmdV = key({ key: "v", code: "KeyV", metaKey: true });
const dictationCtrlV = key({ key: "v", code: "", ctrlKey: true });

// Each shortcut pastes only when its own switch is on.
assert.equal(isClientPasteShortcut(ctrlV, true, false), true);
assert.equal(isClientPasteShortcut(ctrlV, false, true), false);
assert.equal(isClientPasteShortcut(ctrlShiftV, false, true), true);
assert.equal(isClientPasteShortcut(ctrlShiftV, true, false), false);
assert.equal(isClientPasteShortcut(cmdV, true, false), true);
assert.equal(isClientPasteShortcut(dictationCtrlV, true, false), true);

// A dictation tool's Ctrl+V has no key code; a person's does.
assert.equal(isInjectedPasteChord(dictationCtrlV), true);
assert.equal(isInjectedPasteChord(ctrlV), false);
assert.equal(isInjectedPasteChord(key({ ...dictationCtrlV, shiftKey: true })), false);

// Simulated typing carries a character and no key code.
assert.equal(isTypedCharacter(key({ key: "a" })), true);
assert.equal(isTypedCharacter(key({ key: "Enter" })), true);
assert.equal(isTypedCharacter(key({ key: "Process" })), false);
assert.equal(isTypedCharacter(key({ key: "a", code: "KeyA" })), false);
assert.equal(isTypedCharacter(key({ key: "a", ctrlKey: true })), false);

console.log(
  "Paste helpers: text to key presses, paste shortcuts, injected paste, typed characters: OK",
);
