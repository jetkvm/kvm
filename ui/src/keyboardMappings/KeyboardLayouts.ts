import { keysUKApple, charsUKApple, modifiersUKApple } from './layouts/uk_apple';
import { keysUK, charsUK, modifiersUK } from './layouts/uk';
import { keysUS, charsUS, modifiersUS } from './layouts/us';
import { keysDE_T1, charsDE_T1, modifiersDE_T1 } from './layouts/de_t1';
import { keysES, charsES, modifiersES } from './layouts/es';

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
      case "es-ES":
        return {
          keys: keysES,
          chars: charsES,
          modifiers: modifiersES,
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