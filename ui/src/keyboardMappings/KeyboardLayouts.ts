import {keysUKApple, charsUKApple, modifiersUKApple } from './layouts/uk_apple';
import {keysUK, charsUK, modifiersUK } from './layouts/uk';
import {keysUS, charsUS, modifiersUS } from './layouts/us';
import { keysDE_T1, charsDE_T1, modifiersDE_T1 } from './layouts/de_t1';

export function getKeyboardMappings(layout: string) {
  switch (layout) {
    case "uk_apple":
      return {
        keys: keysUKApple,
        chars: charsUKApple,
        modifiers: modifiersUKApple,
      };
    case "uk":
      return {
        keys: keysUK,
        chars: charsUK,
        modifiers: modifiersUK,
      };
      case "de_t1":
        return {
          keys: keysDE_T1,
          chars: charsDE_T1,
          modifiers: modifiersDE_T1,
        };
    case "us":
      default:
        return {
          keys: keysUS,
          chars: charsUS,
          modifiers: modifiersUS,
        };
    }
}