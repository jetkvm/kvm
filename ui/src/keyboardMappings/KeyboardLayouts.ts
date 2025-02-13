import {keysUKApple, charsUKApple, modifiersUKApple } from './layouts/uk_apple';
import {keysUS, charsUS, modifiersUS } from './layouts/us';

export function getKeyboardMappings(layout: string) {
  switch (layout) {
    case "uk_apple":
      return {
        keys: keysUKApple,
        chars: charsUKApple,
        modifiers: modifiersUKApple,
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