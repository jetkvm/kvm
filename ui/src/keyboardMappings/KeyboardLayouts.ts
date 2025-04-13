import { keysUKApple, charsUKApple, modifiersUKApple } from './layouts/uk_apple';
import { keysUK, charsUK, modifiersUK } from './layouts/uk';
import { keysUS, charsUS, modifiersUS } from './layouts/us';
import { keysDE_T1, charsDE_T1, modifiersDE_T1 } from './layouts/de_t1';

export function getKeyboardMappings(layout: string) {
  switch (layout) {
    case "en-GB_apple":
      return {
        keys: keysUKApple,
        chars: charsUKApple,
        modifiers: modifiersUKApple,
      };
    case "en-GB":
      return {
        keys: keysUK,
        chars: charsUK,
        modifiers: modifiersUK,
      };
      case "de-DE":
        return {
          keys: keysDE_T1,
          chars: charsDE_T1,
          modifiers: modifiersDE_T1,
        };
    case "en-US":
      default:
        return {
          keys: keysUS,
          chars: charsUS,
          modifiers: modifiersUS,
        };
    }
}

export const modifierDisplayMap: Record<string, string> = {
  ControlLeft: "Left Ctrl",
  ControlRight: "Right Ctrl",
  ShiftLeft: "Left Shift",
  ShiftRight: "Right Shift",
  AltLeft: "Left Alt",
  AltRight: "Right Alt",
  MetaLeft: "Left Meta",
  MetaRight: "Right Meta",
} as Record<string, string>;

export const keyDisplayMap: Record<string, string> = {
  CtrlAltDelete: "Ctrl + Alt + Delete",
  AltMetaEscape: "Alt + Meta + Escape",
  Escape: "esc",
  Tab: "tab",
  Backspace: "backspace",
  Enter: "enter",
  CapsLock: "caps lock",
  ShiftLeft: "shift",
  ShiftRight: "shift",
  ControlLeft: "ctrl",
  AltLeft: "alt",
  AltRight: "alt",
  MetaLeft: "meta",
  MetaRight: "meta",
  Space: " ",
  Home: "home",
  PageUp: "pageup",
  Delete: "delete",
  End: "end",
  PageDown: "pagedown",
  ArrowLeft: "←",
  ArrowRight: "→",
  ArrowUp: "↑",
  ArrowDown: "↓",
  
  // Letters
  KeyA: "a", KeyB: "b", KeyC: "c", KeyD: "d", KeyE: "e",
  KeyF: "f", KeyG: "g", KeyH: "h", KeyI: "i", KeyJ: "j",
  KeyK: "k", KeyL: "l", KeyM: "m", KeyN: "n", KeyO: "o",
  KeyP: "p", KeyQ: "q", KeyR: "r", KeyS: "s", KeyT: "t",
  KeyU: "u", KeyV: "v", KeyW: "w", KeyX: "x", KeyY: "y",
  KeyZ: "z",

  // Numbers
  Digit1: "1", Digit2: "2", Digit3: "3", Digit4: "4", Digit5: "5",
  Digit6: "6", Digit7: "7", Digit8: "8", Digit9: "9", Digit0: "0",

  // Symbols
  Minus: "-",
  Equal: "=",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  Backquote: "`",
  IntlBackslash: "\\",

  // Function keys
  F1: "F1", F2: "F2", F3: "F3", F4: "F4",
  F5: "F5", F6: "F6", F7: "F7", F8: "F8",
  F9: "F9", F10: "F10", F11: "F11", F12: "F12",

  // Numpad
  Numpad0: "Num 0", Numpad1: "Num 1", Numpad2: "Num 2",
  Numpad3: "Num 3", Numpad4: "Num 4", Numpad5: "Num 5",
  Numpad6: "Num 6", Numpad7: "Num 7", Numpad8: "Num 8",
  Numpad9: "Num 9", NumpadAdd: "Num +", NumpadSubtract: "Num -",
  NumpadMultiply: "Num *", NumpadDivide: "Num /", NumpadDecimal: "Num .",
  NumpadEnter: "Num Enter",

  // Mappings for Keyboard Layout Mapping
  "q": "q",
  "w": "w",
  "e": "e",
  "r": "r",
  "t": "t",
  "y": "y",
  "u": "u",
  "i": "i",
  "o": "o",
  "p": "p",
  "a": "a",
  "s": "s",
  "d": "d",
  "f": "f",
  "g": "g",
  "h": "h",
  "j": "j",
  "k": "k",
  "l": "l",
  "z": "z",
  "x": "x",
  "c": "c",
  "v": "v",
  "b": "b",
  "n": "n",
  "m": "m",
  
  "Q": "Q",
  "W": "W",
  "E": "E",
  "R": "R",
  "T": "T",
  "Y": "Y",
  "U": "U",
  "I": "I",
  "O": "O",
  "P": "P",
  "A": "A",
  "S": "S",
  "D": "D",
  "F": "F",
  "G": "G",
  "H": "H",
  "J": "J",
  "K": "K",
  "L": "L",
  "Z": "Z",
  "X": "X",
  "C": "C",
  "V": "V",
  "B": "B",
  "N": "N",
  "M": "M",
  
  "1": "1",
  "2": "2",
  "3": "3",
  "4": "4",
  "5": "5",
  "6": "6",
  "7": "7",
  "8": "8",
  "9": "9",
  "0": "0",
                      
  "!": "!",
  "@": "@",
  "#": "#",
  "$": "$",
  "%": "%",
  "^": "^",
  "&": "&",
  "*": "*",
  "(": "(",
  ")": ")",
                    
  "-": "-",
  "_": "_",
                    
  "[": "[",
  "]": "]",
  "{": "{",
  "}": "}",
  
  "|": "|",
  
  ";": ";",
  ":": ":",
  
  "'": "'",
  "\"": "\"",
  
  ",": ",",
  "<": "<",
  
  ".": ".",
  ">": ">",
  
  "/": "/",
  "?": "?",
  
  "`": "`",
  "~": "~",
  
  "\\": "\\"                 
};