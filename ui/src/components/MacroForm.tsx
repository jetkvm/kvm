import { useState } from "react";
import { LuPlus } from "react-icons/lu";

import { Button } from "@/components/Button";
import FieldLabel from "@/components/FieldLabel";
import Fieldset from "@/components/Fieldset";
import { InputFieldWithLabel, FieldError } from "@/components/InputField";
import { MacroStepCard } from "@/components/MacroStepCard";
import {
  DEFAULT_DELAY,
  MAX_STEPS_PER_MACRO,
  MAX_KEYS_PER_STEP,
} from "@/constants/macros";
import { KeySequence } from "@/hooks/stores";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";

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
  submitText?: string;
}

export function MacroForm({
  initialData,
  onSubmit,
  onCancel,
  isSubmitting = false,
  submitText = "Save Macro",
}: MacroFormProps) {
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
      newErrors.name = "Name is required";
    } else if (macro.name.trim().length > 50) {
      newErrors.name = "Name must be less than 50 characters";
    }

    if (!macro.steps?.length) {
      newErrors.steps = { 0: { keys: "At least one step is required" } };
    } else {
      const hasKeyOrModifier = macro.steps.some(
        step => (step.text && step.text.length > 0) || (step.keys?.length || 0) > 0 || (step.modifiers?.length || 0) > 0,
      );

      if (!hasKeyOrModifier) {
        newErrors.steps = {
          0: { keys: "At least one step must have keys or modifiers" },
        };
      }
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) {
      showTemporaryError("Please fix the validation errors");
      return;
    }

    try {
      await onSubmit(macro);
    } catch (error) {
      if (error instanceof Error) {
        showTemporaryError(error.message);
      } else {
        showTemporaryError("An error occurred while saving");
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
      newSteps[stepIndex].keys = option.keys;
    } else if (option.value) {
      if (!newSteps[stepIndex].keys) {
        newSteps[stepIndex].keys = [];
      }
      const keysArray = Array.isArray(newSteps[stepIndex].keys)
        ? newSteps[stepIndex].keys
        : [];
      if (keysArray.length >= MAX_KEYS_PER_STEP) {
        showTemporaryError(`Maximum of ${MAX_KEYS_PER_STEP} keys per step allowed`);
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

  const handleStepTypeChange = (stepIndex: number, type: "keys" | "text" | "wait") => {
    const newSteps = [...(macro.steps || [])];
    const prev = newSteps[stepIndex] || { keys: [], modifiers: [], delay: DEFAULT_DELAY };
    if (type === "text") {
      newSteps[stepIndex] = { keys: [], modifiers: [], delay: prev.delay, text: prev.text || "" } as any;
    } else if (type === "wait") {
      newSteps[stepIndex] = { keys: [], modifiers: [], delay: prev.delay, wait: true } as any;
    } else {
      // switch back to keys; drop text
      const { text, wait, ...rest } = prev as any;
      newSteps[stepIndex] = { ...rest } as any;
    }
    setMacro({ ...macro, steps: newSteps });
  };

  const handleTextChange = (stepIndex: number, text: string) => {
    const newSteps = [...(macro.steps || [])];
    // Ensure this step is of text type
    newSteps[stepIndex] = { ...(newSteps[stepIndex] || { keys: [], modifiers: [], delay: DEFAULT_DELAY }), text } as any;
    setMacro({ ...macro, steps: newSteps });
  };

  const insertStepAfter = (index: number) => {
    if (isMaxStepsReached) {
      showTemporaryError(
        `You can only add a maximum of ${MAX_STEPS_PER_MACRO} steps per macro.`,
      );
      return;
    }
    const newSteps = [...(macro.steps || [])];
    newSteps.splice(index + 1, 0, { keys: [], modifiers: [], delay: DEFAULT_DELAY });
    setMacro(prev => ({ ...prev, steps: newSteps }));
  };

  const handleStepMove = (stepIndex: number, direction: "up" | "down") => {
    const newSteps = [...(macro.steps || [])];
    const newIndex = direction === "up" ? stepIndex - 1 : stepIndex + 1;
    [newSteps[stepIndex], newSteps[newIndex]] = [newSteps[newIndex], newSteps[stepIndex]];
    setMacro({ ...macro, steps: newSteps });
  };

  const isMaxStepsReached = (macro.steps?.length || 0) >= MAX_STEPS_PER_MACRO;

  return (
    <>
      <div className="space-y-4">
        <Fieldset>
          <InputFieldWithLabel
            type="text"
            label="Macro Name"
            placeholder="Macro Name"
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
                label="Steps"
                description={`Keys/modifiers executed in sequence with a delay between each step.`}
              />
            </div>
            <span className="text-slate-500 dark:text-slate-400">
              {macro.steps?.length || 0}/{MAX_STEPS_PER_MACRO} steps
            </span>
          </div>
          {errors.steps && errors.steps[0]?.keys && (
            <div className="mt-2">
              <FieldError error={errors.steps[0].keys} />
            </div>
          )}
          <Fieldset>
            <div className="mt-2 space-y-4">
              {(macro.steps || []).map((step, stepIndex) => (
                <div key={stepIndex} className="space-y-3">
                  <MacroStepCard
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
                    onStepTypeChange={type => handleStepTypeChange(stepIndex, type)}
                    onTextChange={text => handleTextChange(stepIndex, text)}
                  />
                  {stepIndex < (macro.steps?.length || 0) - 1 && (
                    <div className="flex justify-center">
                      <Button
                        size="XS"
                        theme="light"
                        LeadingIcon={LuPlus}
                        text="Insert step here"
                        onClick={() => insertStepAfter(stepIndex)}
                        disabled={isMaxStepsReached}
                      />
                    </div>
                  )}
                </div>
              ))}
            </div>
          </Fieldset>

          <div className="mt-4">
            <Button
              size="MD"
              theme="light"
              fullWidth
              LeadingIcon={LuPlus}
              text={`Add Step ${isMaxStepsReached ? `(${MAX_STEPS_PER_MACRO} max)` : ""}`}
              onClick={() => {
                if (isMaxStepsReached) {
                  showTemporaryError(
                    `You can only add a maximum of ${MAX_STEPS_PER_MACRO} steps per macro.`,
                  );
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
              text={isSubmitting ? "Saving..." : submitText}
              onClick={handleSubmit}
              disabled={isSubmitting}
            />
            <Button size="SM" theme="light" text="Cancel" onClick={onCancel} />
          </div>
        </div>
      </div>
    </>
  );
}
