import { useCallback, useEffect, useMemo, useState } from "react";
import { LuKeyboard, LuPlus, LuType } from "react-icons/lu";
import { ExclamationCircleIcon } from "@heroicons/react/16/solid";

import { KeySequence, useSettingsStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import type { KeyboardLayout } from "@components/keyboard/types/schema";
import { textToMacroSteps, scancodeToKeyName } from "@components/textToMacroSteps";
import { VirtualKeyboard } from "@components/keyboard/VirtualKeyboard";
import { buildKeyDisplayMap } from "@/keyDisplayNames";
import Modal from "@components/Modal";
import { Button } from "@components/Button";
import FieldLabel from "@components/FieldLabel";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel, FieldError } from "@components/InputField";
import { TextAreaWithLabel } from "@components/TextArea";
import { MacroStepCard } from "@components/MacroStepCard";
import { keys, isModifierScancode, modifierKeyNames } from "@/keyboardMappings";
import { DEFAULT_DELAY, MAX_STEPS_PER_MACRO, MAX_KEYS_PER_STEP } from "@/constants/macros";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

import "@components/keyboard/virtual-keyboard.css";

interface ValidationErrors {
  name?: string;
  steps?: Record<
    number,
    {
      keys?: string;
      modifiers?: string;
      delay?: string;
    }
  >;
}

interface MacroFormProps {
  initialData: Partial<KeySequence>;
  onSubmit: (macro: Partial<KeySequence>) => Promise<void>;
  onCancel: () => void;
  isSubmitting?: boolean;
}

export function MacroForm({
  initialData,
  onSubmit,
  onCancel,
  isSubmitting = false,
}: Readonly<MacroFormProps>) {
  const [macro, setMacro] = useState<Partial<KeySequence>>(initialData);
  const [keyQueries, setKeyQueries] = useState<Record<number, string>>({});
  const [errors, setErrors] = useState<ValidationErrors>({});
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const { send } = useJsonRpc();
  const { keyboardLayout } = useSettingsStore();
  const [kleLayout, setKleLayout] = useState<KeyboardLayout | null>(null);

  useEffect(() => {
    if (!keyboardLayout) return;
    void send("getKeyboardLayoutData", { id: keyboardLayout }, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setKleLayout(resp.result as KeyboardLayout);
    });
  }, [send, keyboardLayout]);

  const keyDisplayMap = useMemo(() => buildKeyDisplayMap(kleLayout), [kleLayout]);

  // Text-to-macro
  const [textInput, setTextInput] = useState("");
  const [textInvalidChars, setTextInvalidChars] = useState<string[]>([]);
  const [showTextInput, setShowTextInput] = useState(false);

  const handleGenerateFromText = useCallback(() => {
    if (!kleLayout) {
      showTemporaryError(m.macro_add_from_text_no_layout());
      return;
    }
    if (!textInput.trim()) {
      showTemporaryError(m.macro_add_from_text_empty());
      return;
    }

    const { steps: newSteps, invalidChars } = textToMacroSteps(textInput, kleLayout);
    setTextInvalidChars(invalidChars);

    if (newSteps.length === 0) return;

    // Check step limit
    const currentCount = macro.steps?.length ?? 0;
    const available = MAX_STEPS_PER_MACRO - currentCount;
    const stepsToAdd = newSteps.slice(0, available);

    setMacro(prev => ({
      ...prev,
      steps: [...(prev.steps || []), ...stepsToAdd],
    }));
    setErrors({});
    setTextInput("");

    notifications.success(m.macro_add_from_text_generated({ count: stepsToAdd.length }));

    if (stepsToAdd.length < newSteps.length) {
      showTemporaryError(m.macro_max_steps_error({ max: MAX_STEPS_PER_MACRO }));
    }
  }, [kleLayout, textInput, macro.steps]);

  const showTemporaryError = (message: string) => {
    setErrorMessage(message);
    setTimeout(() => setErrorMessage(null), 3000);
  };

  const validateForm = (): boolean => {
    const newErrors: ValidationErrors = {};

    // Name validation
    if (!macro.name?.trim()) {
      newErrors.name = m.macro_name_required();
    } else if (macro.name.trim().length > 50) {
      newErrors.name = m.macro_name_too_long();
    }

    const steps = macro.steps || [];

    if (steps.length) {
      const hasKeyOrModifier = steps.some(
        step => step.keys.length > 0 || step.modifiers.length > 0,
      );

      if (!hasKeyOrModifier) {
        newErrors.steps = {
          0: { keys: m.macro_at_least_one_step_keys_or_modifiers() },
        };
      }
    } else {
      newErrors.steps = { 0: { keys: m.macro_at_least_one_step_required() } };
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) {
      showTemporaryError(m.macro_please_fix_validation_errors());
      return;
    }

    try {
      await onSubmit(macro);
    } catch (error) {
      if (error instanceof Error) {
        showTemporaryError(
          m.macro_save_failed_error({ error: error.message || m.unknown_error() }),
        );
      } else {
        showTemporaryError(m.macro_save_failed());
      }
    }
  };

  const handleKeySelect = (
    stepIndex: number,
    option: { value: string | null; keys?: string[] },
  ) => {
    const newSteps = [...(macro.steps || [])];
    if (!newSteps[stepIndex]) return;

    if (option.keys) {
      // they gave us a full set of keys (e.g. from deleting one)
      newSteps[stepIndex].keys = option.keys;
    } else if (option.value) {
      // they gave us a single key to add
      if (!newSteps[stepIndex].keys) {
        newSteps[stepIndex].keys = [];
      }
      const keysArray = newSteps[stepIndex].keys;
      if (keysArray.length >= MAX_KEYS_PER_STEP) {
        showTemporaryError(m.macro_max_steps_error({ max: MAX_KEYS_PER_STEP }));
        return;
      }
      newSteps[stepIndex].keys = [...keysArray, option.value];
    }
    setMacro({ ...macro, steps: newSteps });

    if (errors.steps?.[stepIndex]?.keys) {
      const newErrors = { ...errors };
      delete newErrors.steps?.[stepIndex].keys;
      if (Object.keys(newErrors.steps?.[stepIndex] || {}).length === 0) {
        delete newErrors.steps?.[stepIndex];
      }
      if (Object.keys(newErrors.steps || {}).length === 0) {
        delete newErrors.steps;
      }
      setErrors(newErrors);
    }
  };

  // Keyboard picker — modifier latching + sequential key steps
  const [keyboardPickerOpen, setKeyboardPickerOpen] = useState(false);
  const [latchedModifiers, setLatchedModifiers] = useState<Set<number>>(new Set());

  const handleKeyboardPick = (scancode: number) => {
    // Toggle modifier latch
    if (isModifierScancode(scancode)) {
      setLatchedModifiers(prev => {
        const next = new Set(prev);
        if (next.has(scancode)) {
          next.delete(scancode);
        } else {
          next.add(scancode);
        }
        return next;
      });
      return;
    }

    const keyName = scancodeToKeyName.get(scancode);
    if (!keyName) return;

    const currentCount = macro.steps?.length ?? 0;
    if (currentCount >= MAX_STEPS_PER_MACRO) {
      showTemporaryError(m.macro_max_steps_error({ max: MAX_STEPS_PER_MACRO }));
      return;
    }

    // Build modifier list from latched modifiers using the keys mapping
    const mods: string[] = [];
    for (const name of modifierKeyNames) {
      if (latchedModifiers.has(keys[name])) {
        mods.push(name);
      }
    }

    setMacro(prev => ({
      ...prev,
      steps: [...(prev.steps || []), { keys: [keyName], modifiers: mods, delay: DEFAULT_DELAY }],
    }));
    setErrors({});
  };

  // Visual highlight for latched modifiers on the picker keyboard
  const pickerPressedScancodes = useMemo(() => latchedModifiers, [latchedModifiers]);

  // Clear latched modifiers when picker closes
  useEffect(() => {
    if (!keyboardPickerOpen) {
      setLatchedModifiers(new Set());
    }
  }, [keyboardPickerOpen]);

  const handleKeyQueryChange = (stepIndex: number, query: string) => {
    setKeyQueries(prev => ({ ...prev, [stepIndex]: query }));
  };

  const handleModifierChange = (stepIndex: number, modifiers: string[]) => {
    const newSteps = [...(macro.steps || [])];
    newSteps[stepIndex].modifiers = modifiers;
    setMacro({ ...macro, steps: newSteps });

    // Clear step errors when modifiers are added
    if (errors.steps?.[stepIndex]?.keys && modifiers.length > 0) {
      const newErrors = { ...errors };
      delete newErrors.steps?.[stepIndex].keys;
      if (Object.keys(newErrors.steps?.[stepIndex] || {}).length === 0) {
        delete newErrors.steps?.[stepIndex];
      }
      if (Object.keys(newErrors.steps || {}).length === 0) {
        delete newErrors.steps;
      }
      setErrors(newErrors);
    }
  };

  const handleDelayChange = (stepIndex: number, delay: number) => {
    const newSteps = [...(macro.steps || [])];
    newSteps[stepIndex].delay = delay;
    setMacro({ ...macro, steps: newSteps });
  };

  const handleStepMove = (stepIndex: number, direction: "up" | "down") => {
    const newSteps = [...(macro.steps || [])];
    const newIndex = direction === "up" ? stepIndex - 1 : stepIndex + 1;
    [newSteps[stepIndex], newSteps[newIndex]] = [newSteps[newIndex], newSteps[stepIndex]];
    setMacro({ ...macro, steps: newSteps });
  };

  const isMaxStepsReached = (macro.steps?.length || 0) >= MAX_STEPS_PER_MACRO;

  return (
    <div className="space-y-4">
      <Fieldset>
        <InputFieldWithLabel
          type="text"
          label={m.macro_name_label()}
          placeholder={m.macro_name_label()}
          value={macro.name}
          error={errors.name}
          onChange={e => {
            setMacro(prev => ({ ...prev, name: e.target.value }));
            if (errors.name) {
              const newErrors = { ...errors };
              delete newErrors.name;
              setErrors(newErrors);
            }
          }}
        />
      </Fieldset>

      <div>
        <div className="flex items-center justify-between text-sm">
          <div className="flex items-center gap-1">
            <FieldLabel label={m.macro_steps_label()} description={m.macro_steps_description()} />
          </div>
          <span className="text-slate-500 dark:text-slate-400">
            {m.macro_step_count({
              steps: macro.steps?.length || 0,
              max: MAX_STEPS_PER_MACRO,
            })}
          </span>
        </div>
        {errors.steps?.[0]?.keys && (
          <div className="mt-2">
            <FieldError error={errors.steps?.[0]?.keys} />
          </div>
        )}
        <Fieldset>
          <div className="mt-2 space-y-4">
            {(macro.steps || []).map((step, stepIndex) => (
              <MacroStepCard
                key={`step-${stepIndex}`}
                step={step}
                stepIndex={stepIndex}
                onDelete={
                  macro.steps && macro.steps.length > 1
                    ? () => {
                        const newSteps = [...(macro.steps || [])];
                        newSteps.splice(stepIndex, 1);
                        setMacro(prev => ({ ...prev, steps: newSteps }));
                      }
                    : undefined
                }
                onMoveUp={() => handleStepMove(stepIndex, "up")}
                onMoveDown={() => handleStepMove(stepIndex, "down")}
                onKeySelect={option => handleKeySelect(stepIndex, option)}
                onKeyQueryChange={query => handleKeyQueryChange(stepIndex, query)}
                keyQuery={keyQueries[stepIndex] || ""}
                onModifierChange={modifiers => handleModifierChange(stepIndex, modifiers)}
                onDelayChange={delay => handleDelayChange(stepIndex, delay)}
                isLastStep={stepIndex === (macro.steps?.length || 0) - 1}
                keyDisplayMap={keyDisplayMap}
              />
            ))}
          </div>
        </Fieldset>

        <div className="mt-4 flex gap-2">
          <Button
            size="MD"
            theme="light"
            fullWidth
            LeadingIcon={LuPlus}
            text={m.macro_add_step({
              maxed_out: isMaxStepsReached
                ? m.macro_max_steps_reached({ max: MAX_STEPS_PER_MACRO })
                : "",
            })}
            onClick={() => {
              if (isMaxStepsReached) {
                showTemporaryError(m.macro_max_steps_error({ max: MAX_STEPS_PER_MACRO }));
                return;
              }

              setMacro(prev => ({
                ...prev,
                steps: [...(prev.steps || []), { keys: [], modifiers: [], delay: DEFAULT_DELAY }],
              }));
              setErrors({});
            }}
            disabled={isMaxStepsReached}
          />
          <Button
            size="MD"
            theme="light"
            fullWidth
            LeadingIcon={LuType}
            text={m.macro_add_from_text()}
            onClick={() => setShowTextInput(!showTextInput)}
            disabled={isMaxStepsReached}
            data-testid="macro-add-from-text-toggle"
          />
          {kleLayout && (
            <Button
              size="MD"
              theme="light"
              fullWidth
              LeadingIcon={LuKeyboard}
              text={m.macro_step_type_on_keyboard()}
              onClick={() => setKeyboardPickerOpen(true)}
              disabled={isMaxStepsReached}
              data-testid="macro-open-keyboard-picker"
            />
          )}
        </div>

        {showTextInput && (
          <div className="mt-4 space-y-2 rounded-md border border-slate-200 p-3 dark:border-slate-700">
            <FieldLabel
              label={m.macro_add_from_text()}
              description={m.macro_add_from_text_description()}
            />
            <div
              onKeyUp={e => e.stopPropagation()}
              onKeyDown={e => e.stopPropagation()}
              onKeyDownCapture={e => e.stopPropagation()}
              onKeyUpCapture={e => e.stopPropagation()}
            >
              <TextAreaWithLabel
                rows={3}
                value={textInput}
                placeholder={m.macro_add_from_text_placeholder()}
                onChange={e => {
                  setTextInput(e.target.value);
                  setTextInvalidChars([]);
                }}
              />
            </div>
            {textInvalidChars.length > 0 && (
              <div className="flex items-center gap-x-2">
                <ExclamationCircleIcon className="h-4 w-4 shrink-0 text-amber-500" />
                <span className="text-xs text-amber-600 dark:text-amber-400">
                  {m.macro_add_from_text_invalid_chars({ chars: textInvalidChars.join(", ") })}
                </span>
              </div>
            )}
            <Button
              size="SM"
              theme="primary"
              text={m.macro_add_from_text_generate()}
              onClick={handleGenerateFromText}
              disabled={!kleLayout || !textInput.trim() || isMaxStepsReached}
              data-testid="macro-generate-from-text"
            />
          </div>
        )}

        {errorMessage && (
          <div className="mt-4">
            <FieldError error={errorMessage} />
          </div>
        )}

        <div className="mt-6 flex items-center gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text={isSubmitting ? m.saving() : m.macro_save()}
            onClick={handleSubmit}
            disabled={isSubmitting}
          />
          <Button size="SM" theme="light" text={m.cancel()} onClick={onCancel} />
        </div>
      </div>

      {kleLayout && (
        <Modal
          open={keyboardPickerOpen}
          onClose={() => setKeyboardPickerOpen(false)}
        >
          <div className="mx-auto max-w-7xl px-4">
            <div className="pointer-events-auto relative w-full overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
              <div className="flex items-center justify-between border-b border-slate-200 px-6 py-3 dark:border-slate-700">
                <span className="text-sm font-medium text-slate-700 dark:text-slate-300">
                  {m.macro_step_type_on_keyboard_title()}
                </span>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-slate-400">
                    {m.macro_step_count({
                      steps: macro.steps?.length || 0,
                      max: MAX_STEPS_PER_MACRO,
                    })}
                  </span>
                  <Button
                    size="SM"
                    theme="blank"
                    text={m.keyboard_layout_preview_close()}
                    onClick={() => setKeyboardPickerOpen(false)}
                  />
                </div>
              </div>
              <div className="overflow-x-auto bg-slate-50 p-4 dark:bg-slate-800/50">
                <div className="flex justify-center">
                  <VirtualKeyboard
                    keyboard={kleLayout}
                    isMetaActive={false}
                    onKeySend={handleKeyboardPick}
                    pressedScancodes={pickerPressedScancodes}
                  />
                </div>
              </div>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
