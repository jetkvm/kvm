/**
 * QuickActions — a dropdown menu of common key combinations that are hard or
 * impossible to type through a browser (because the operator's OS or browser
 * intercepts them before they reach the KVM).
 *
 * Labels use standard keyboard symbols (⌃ ⌥ ⌘ ⇧ ⌫ ⎋) and key names as
 * printed on physical keycaps — these are universal and not localized.
 */

import { useCallback } from "react";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { LuChevronDown, LuKeyboard } from "react-icons/lu";

import { cx } from "@/cva.config";
import type { MacroSteps } from "@hooks/useKeyboard";
import { m } from "@localizations/messages.js";

// ---------------------------------------------------------------------------
// Symbols (standard across documentation and keycaps)
// ---------------------------------------------------------------------------

const SYM = {
  ctrl:  "⌃",
  alt:   "⌥",
  shift: "⇧",
  meta:  "⌘",
  bksp:  "⌫",
  del:   "⌦",
  esc:   "Esc",
  tab:   "⇥",
  space: "␣",
  f4:   "F4",
  l:   "L",
  r:  "R",
} as const;

// ---------------------------------------------------------------------------
// Action definitions
// ---------------------------------------------------------------------------

interface QuickAction {
  label: string;
  title: () => string;
  testId: string;
  steps: MacroSteps;
}

interface ActionGroup {
  title: string;
  actions: QuickAction[];
}

const ACTION_GROUPS: ActionGroup[] = [
  {
    title: "Common",
    actions: [
      {
        label: `${SYM.ctrl} ${SYM.alt} ${SYM.del}`,
        title: () => "Ctrl + Alt + Del",
        testId: "ctrl-alt-del",
        steps: [{ keys: ["Delete"], modifiers: ["ControlLeft", "AltLeft"], delay: 100 }],
      },
      {
        label: `${SYM.alt} ${SYM.tab}`,
        title: () => "Alt + Tab",
        testId: "alt-tab",
        steps: [{ keys: ["Tab"], modifiers: ["AltLeft"], delay: 100 }],
      },
      {
        label: `${SYM.alt} F4`,
        title: () => `Alt + F4 (${m.keyboard_combo_explain_close_window()})`,
        testId: "alt-f4",
        steps: [{ keys: ["F4"], modifiers: ["AltLeft"], delay: 100 }],
      },
    ],
  },
  {
    title: "Windows",
    actions: [
      {
        label: `${SYM.ctrl} ${SYM.shift} ${SYM.esc}`,
        title: () => `Ctrl + Shift + Esc (${m.keyboard_combo_explain_task_manager()})`,
        testId: "ctrl-shift-esc",
        steps: [{ keys: ["Escape"], modifiers: ["ControlLeft", "ShiftLeft"], delay: 100 }],
      },
      {
        label: `${SYM.meta} L`,
        title: () => `Win + L (${m.keyboard_combo_explain_lock()})`,
        testId: "meta-l",
        steps: [{ keys: ["KeyL"], modifiers: ["MetaLeft"], delay: 100 }],
      },
      {
        label: `${SYM.meta} R`,
        title: () => `Win + R (${m.keyboard_combo_explain_run_dialog()})`,
        testId: "meta-r",
        steps: [{ keys: ["KeyR"], modifiers: ["MetaLeft"], delay: 100 }],
      },
    ],
  },
  {
    title: "Mac",
    actions: [
      {
        label: `${SYM.meta} ${SYM.space}`,
        title: () => `Cmd + Space (${m.keyboard_combo_explain_spotlight()})`,
        testId: "meta-space",
        steps: [{ keys: ["Space"], modifiers: ["MetaLeft"], delay: 100 }],
      },
    ],
  },
  {
    title: "Linux",
    actions: [
      {
        label: `${SYM.ctrl} ${SYM.alt} ${SYM.bksp}`,
        title: () => `Ctrl + Alt + Backspace (${m.keyboard_combo_explain_kill_x11()})`,
        testId: "ctrl-alt-bksp",
        steps: [{ keys: ["Backspace"], modifiers: ["ControlLeft", "AltLeft"], delay: 100 }],
      },
      {
        label: `${SYM.alt} ${SYM.meta} ${SYM.esc}`,
        title: () => "Alt + Meta + Esc",
        testId: "alt-meta-esc",
        steps: [{ keys: ["Escape"], modifiers: ["AltLeft", "MetaLeft"], delay: 100 }],
      },
    ],
  },
];

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface QuickActionsProps {
  onExecuteMacro: (steps: MacroSteps) => Promise<void>;
}

export function QuickActions({ onExecuteMacro }: QuickActionsProps) {
  const handleClick = useCallback(
    (steps: MacroSteps) => {
      void onExecuteMacro(steps);
    },
    [onExecuteMacro],
  );

  return (
    <Menu as="div" className="relative">
      <MenuButton
        className={cx(
          "inline-flex cursor-pointer items-center gap-1.5 rounded px-2 py-1 text-xs font-medium",
          "border border-orange-400/30 bg-orange-500/10 text-orange-300/85",
          "transition-all duration-150",
          "hover:border-orange-400/40 hover:bg-orange-500/25 hover:text-orange-200",
          "active:bg-orange-500/35",
        )}
        data-testid="quick-actions-menu"
      >
        <LuKeyboard className="h-3.5 w-3.5" />
        <LuChevronDown className="h-3 w-3" />
      </MenuButton>

      <MenuItems
        anchor="bottom start"
        transition
        className={cx(
          "z-30 mt-1 min-w-[160px] origin-top-left rounded-md",
          "border border-slate-700 bg-slate-800 shadow-lg",
          "transition duration-150 ease-out data-closed:scale-95 data-closed:opacity-0",
        )}
      >
        {ACTION_GROUPS.map(group => (
          <div key={group.title}>
            <div className="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400">
              {group.title}
            </div>
            <div className="px-1 pb-1">
              {group.actions.map(action => (
                <MenuItem key={action.testId}>
                  <button
                    type="button"
                    className={cx(
                      "flex w-full items-center justify-between gap-4 rounded-sm px-2 py-1.5 text-xs",
                      "text-slate-200 data-focus:bg-slate-700",
                    )}
                    data-testid={`quick-action-${action.testId}`}
                    title={action.title()}
                    aria-label={action.title()}
                    onClick={() => handleClick(action.steps)}
                  >
                    <span className="tracking-widest">{action.label}</span>
                    <span className="text-[10px] text-slate-500">{action.title()}</span>
                  </button>
                </MenuItem>
              ))}
            </div>
          </div>
        ))}
      </MenuItems>
    </Menu>
  );
}
