import { useMemo } from "react";
import { LuArrowUp, LuArrowDown, LuX, LuTrash2 } from "react-icons/lu";

import { Button } from "@/components/Button";
import { Combobox } from "@/components/Combobox";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import Card from "@/components/Card";
import FieldLabel from "@/components/FieldLabel";
import { MAX_KEYS_PER_STEP, DEFAULT_DELAY } from "@/constants/macros";
import { KeyboardLayout } from "@/keyboardLayouts";
import { keys, modifiers } from "@/keyboardMappings";

// Filter out modifier keys since they're handled in the modifiers section
const modifierKeyPrefixes = ['Alt', 'Control', 'Shift', 'Meta'];

const modifierOptions = Object.keys(modifiers).map(modifier => ({
  value: modifier,
  label: modifier.replace(/^(Control|Alt|Shift|Meta)(Left|Right)$/, "$1 $2"),
}));

const groupedModifiers: Record<string, typeof modifierOptions> = {
  Control: modifierOptions.filter(mod => mod.value.startsWith('Control')),
  Shift: modifierOptions.filter(mod => mod.value.startsWith('Shift')),
  Alt: modifierOptions.filter(mod => mod.value.startsWith('Alt')),
  Meta: modifierOptions.filter(mod => mod.value.startsWith('Meta')),
};

const basePresetDelays = [
  { value: "50", label: "50ms" },
  { value: "100", label: "100ms" },
  { value: "200", label: "200ms" },
  { value: "300", label: "300ms" },
  { value: "500", label: "500ms" },
  { value: "750", label: "750ms" },
  { value: "1000", label: "1000ms" },
  { value: "1500", label: "1500ms" },
  { value: "2000", label: "2000ms" },
];

const PRESET_DELAYS = basePresetDelays.map(delay => {
  if (parseInt(delay.value, 10) === DEFAULT_DELAY) {
    return { ...delay, label: "Default" };
  }
  return delay;
});

interface MacroStep {
  keys: string[];
  modifiers: string[];
  delay: number;
}

interface MacroStepCardProps {
  step: MacroStep;
  stepIndex: number;
  onDelete?: () => void;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  onKeySelect: (option: { value: string | null; keys?: string[] }) => void;
  onKeyQueryChange: (query: string) => void;
  keyQuery: string;
  onModifierChange: (modifiers: string[]) => void;
  onDelayChange: (delay: number) => void;
  isLastStep: boolean;
  keyboard: KeyboardLayout
}

const ensureArray = <T,>(arr: T[] | null | undefined): T[] => {
  return Array.isArray(arr) ? arr : [];
};

export function MacroStepCard({
  step,
  stepIndex,
  onDelete,
  onMoveUp,
  onMoveDown,
  onKeySelect,
  onKeyQueryChange,
  keyQuery,
  onModifierChange,
  onDelayChange,
  isLastStep,
  keyboard
}: MacroStepCardProps) {
  const { keyDisplayMap } = keyboard;

  const keyOptions = useMemo(() =>
    Object.keys(keys)
      .filter(key => !modifierKeyPrefixes.some(prefix => key.startsWith(prefix)))
      .map(key => ({
        value: key,
        label: keyDisplayMap[key] || key,
      })),
    [keyDisplayMap]
  );
  
  const filteredKeys = useMemo(() => {
    const selectedKeys = ensureArray(step.keys);
    const availableKeys = keyOptions.filter(option => !selectedKeys.includes(option.value));
    
    if (keyQuery === '') {
      return availableKeys;
    } else {
      return availableKeys.filter(option => option.label.toLowerCase().includes(keyQuery.toLowerCase()));
    }
  }, [keyOptions, keyQuery, step.keys]);

  return (
    <Card className="p-4">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="flex h-6 w-5 items-center justify-center rounded-full bg-blue-100 text-xs font-semibold text-blue-700 dark:bg-blue-900/40 dark:text-blue-200">
            {stepIndex + 1}
          </span>
        </div>

        <div className="flex items-center space-x-2">
          <div className="flex items-center gap-1">
            <Button
              size="XS"
              theme="light"
              onClick={onMoveUp}
              disabled={stepIndex === 0}
              LeadingIcon={LuArrowUp}
            />
            <Button
              size="XS"
              theme="light"
              onClick={onMoveDown}
              disabled={isLastStep}
              LeadingIcon={LuArrowDown}
            />
          </div>
          {onDelete && (
            <Button
              size="XS"
              theme="light"
              className="text-red-500 dark:text-red-400"
              text="Delete"
              LeadingIcon={LuTrash2}
              onClick={onDelete}
            />
          )}
        </div>
      </div>

      <div className="space-y-4 mt-2">
        <div className="w-full flex flex-col gap-2">
          <FieldLabel label="Modifiers" />
          <div className="inline-flex flex-wrap gap-3">
            {Object.entries(groupedModifiers).map(([group, mods]) => (
              <div key={group} className="relative min-w-[120px] rounded-md border border-slate-200 dark:border-slate-700 p-2">
                <span className="absolute -top-2.5 left-2 px-1 text-xs font-medium bg-white dark:bg-slate-800 text-slate-500 dark:text-slate-400">
                  {group}
                </span>
                <div className="flex flex-wrap gap-4 pt-1">
                  {mods.map(option => (
                    <Button
                      key={option.value}
                      size="XS"
                      theme={ensureArray(step.modifiers).includes(option.value) ? "primary" : "light"}
                      text={option.label.split(' ')[1] || option.label}
                      onClick={() => {
                        const modifiersArray = ensureArray(step.modifiers);
                        const isSelected = modifiersArray.includes(option.value);
                        const newModifiers = isSelected
                          ? modifiersArray.filter(m => m !== option.value)
                          : [...modifiersArray, option.value];
                        onModifierChange(newModifiers);
                      }}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
        
        <div className="w-full flex flex-col gap-1">
          <div className="flex items-center gap-1">
            <FieldLabel label="Keys" description={`Maximum ${MAX_KEYS_PER_STEP} keys per step.`} />
          </div>
          {ensureArray(step.keys) && step.keys.length > 0 && (
            <div className="flex flex-wrap gap-1 pb-2">
              {step.keys.map((key, keyIndex) => (
                <span
                  key={keyIndex}
                  className="inline-flex items-center py-0.5 rounded-md bg-blue-100 px-1 text-xs font-medium text-blue-800 dark:bg-blue-900/40 dark:text-blue-200"
                >
                  <span className="px-1">
                    {keyDisplayMap[key] || key}
                  </span>
                  <Button
                    size="XS"
                    className=""
                    theme="blank"
                    onClick={() => {
                      const newKeys = ensureArray(step.keys).filter((_, i) => i !== keyIndex);
                      onKeySelect({ value: null, keys: newKeys });
                    }}
                    LeadingIcon={LuX}
                  />
                </span>
              ))}
            </div>
          )}
          <div className="relative w-full">
            <Combobox
              onChange={(value) => {
                onKeySelect({ value: value as string | null });
                onKeyQueryChange('');
              }}
              displayValue={() => keyQuery}
              onInputChange={onKeyQueryChange}
              options={() => filteredKeys}
              disabledMessage="Max keys reached"
              size="SM"
              immediate
              disabled={ensureArray(step.keys).length >= MAX_KEYS_PER_STEP}
              placeholder={ensureArray(step.keys).length >= MAX_KEYS_PER_STEP ? "Max keys reached" : "Search for key..."}
              emptyMessage="No matching keys found"
            />
          </div>
        </div>
        
        <div className="w-full flex flex-col gap-1">
          <div className="flex items-center gap-1">
            <FieldLabel label="Step Duration" description="Time to wait before executing the next step." />
          </div>
          <div className="flex items-center gap-3">
            <SelectMenuBasic
              size="SM"
              fullWidth
              value={step.delay.toString()}
              onChange={(e) => onDelayChange(parseInt(e.target.value, 10))}
              options={PRESET_DELAYS}
            />
          </div>
        </div>
      </div>
    </Card>
  );
} 