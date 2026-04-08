/**
 * components/keyboard/VirtualKeyboard.tsx
 *
 * Pure keyboard renderer. Receives a KeyboardLayout and pressedScancodes,
 * renders keycaps, derives the display layer from modifier state.
 *
 * All state management (latching, HID sends, modifier tracking) lives in
 * the parent wrapper — this component is stateless except for the derived layer.
 */

import React, { useMemo, useCallback } from 'react';
import { KeyboardLayout, KeyLegends, KeyLayer } from './types/schema';
import { keys } from '@/keyboardMappings';
import { Keycap } from './Keycap';

export interface VirtualKeyboardProps {
  /** KeyboardLayout received from Go backend via JSON-RPC. */
  keyboard: KeyboardLayout;

  /** Whether the META key is currently held. */
  isMetaActive: boolean;

  /** Called when the user presses a virtual key. */
  onKeySend: (scancode: number) => void;

  /**
   * Set of currently pressed scancodes (from keysDownState).
   * Used for visual pressed highlights AND layer derivation.
   * The parent is responsible for populating this from the HID store.
   */
  pressedScancodes?: ReadonlySet<number>;

  /** Extra CSS classes to add to the .vkb container (e.g. LED state classes). */
  vkbClassName?: string;
}

export function VirtualKeyboard({
  keyboard,
  isMetaActive: _isMetaActive,
  onKeySend,
  pressedScancodes,
  vkbClassName,
}: VirtualKeyboardProps) {
  // Derive display layer purely from pressedScancodes
  const layer: KeyLayer = useMemo(() => {
    const hasShift = pressedScancodes?.has(keys.ShiftLeft) ||
                     pressedScancodes?.has(keys.ShiftRight);
    const hasAltgr = pressedScancodes?.has(keys.AltRight);

    if (hasShift && hasAltgr) return 'shift-altgr';
    if (hasShift)             return 'shift';
    if (hasAltgr)             return 'altgr';
    return 'all';
  }, [pressedScancodes]);

  const handleKeyPress = useCallback((scancode: number, _legends: KeyLegends) => {
    onKeySend(scancode);
  }, [onKeySend]);

  return (
    <div className="vkb-wrapper">
      <div
        className={['vkb', vkbClassName].filter(Boolean).join(' ')}
        data-layer={layer}
        style={{
          '--board-w': keyboard.boardW,
          '--board-h': keyboard.boardH,
        } as React.CSSProperties}
        onMouseDown={e => e.preventDefault()}
      >
        {keyboard.keys.map((key, i) => (
          <Keycap
            key={i}
            transportKey={key}
            onPress={handleKeyPress}
            isPressed={
              pressedScancodes !== undefined && key.scancode !== 0
                ? pressedScancodes.has(key.scancode)
                : false
            }
          />
        ))}
      </div>
    </div>
  );
}
