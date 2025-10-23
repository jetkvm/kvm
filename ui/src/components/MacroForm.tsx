import { useState } from "react";
import { LuPlus } from "react-icons/lu";

import { KeySequence } from "@hooks/stores";
import useKeyboardLayout from "@hooks/useKeyboardLayout";
import { Button } from "@components/Button";
import FieldLabel from "@components/FieldLabel";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel, FieldError } from "@components/InputField";
import { MacroStepCard } from "@components/MacroStepCard";
import {
  DEFAULT_DELAY,
  MAX_STEPS_PER_MACRO,
  MAX_KEYS_PER_STEP,
} from "@/constants/macros";
import { m } from "@localizations/messages.js";

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
  const { selectedKeyboard } = useKeyboardLayout();

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

    const steps = (macro.steps || []);
  
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
        showTemporaryError(m.macro_save_failed_error({error: error.message || m.unknown_error()}));
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
            <FieldLabel
              label={m.macro_steps_label()}
              description={m.macro_steps_description()}
            />
          </div>
          <span className="text-slate-500 dark:text-slate-400">
            {m.macro_step_count({ steps: macro.steps?.length || 0, max: MAX_STEPS_PER_MACRO })}
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
                onModifierChange={modifiers =>
                  handleModifierChange(stepIndex, modifiers)
                }
                onDelayChange={delay => handleDelayChange(stepIndex, delay)}
                isLastStep={stepIndex === (macro.steps?.length || 0) - 1}
                keyboard={selectedKeyboard}
              />
            ))}
          </div>
        </Fieldset>

        <div className="mt-4">
          <Button
            size="MD"
            theme="light"
            fullWidth
            LeadingIcon={LuPlus}
            text={m.macro_add_step({ maxed_out: isMaxStepsReached ? m.macro_max_steps_reached({ max: MAX_STEPS_PER_MACRO }) : "" })}
            onClick={() => {
              if (isMaxStepsReached) {
                showTemporaryError(m.macro_max_steps_error({ max: MAX_STEPS_PER_MACRO }));
                return;
              }

              setMacro(prev => ({
                ...prev,
                steps: [
                  ...(prev.steps || []),
                  { keys: [], modifiers: [], delay: DEFAULT_DELAY },
                ],
              }));
              setErrors({});
            }}
            disabled={isMaxStepsReached}
          />
        </div>

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
    </div>
  );
}
