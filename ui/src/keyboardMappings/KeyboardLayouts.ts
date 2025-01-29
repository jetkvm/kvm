import {keysUKApple, charsUKApple, modifiersUKApple } from './layouts/uk_apple';
import {keysUS, charsUS, modifiersUS } from './layouts/us';

export enum KeyboardLayout {
    US = "us",
    UKApple = "uk_apple",
  }

export function getKeyboardMappings(layout: KeyboardLayout) {
    switch (layout) {
      case KeyboardLayout.UKApple:
        return {
          keys: keysUKApple,
          chars: charsUKApple,
          modifiers: modifiersUKApple,
        };
      case KeyboardLayout.US:
      default:
        return {
          keys: keysUS,
          chars: charsUS,
          modifiers: modifiersUS,
        };
    }
  }