import { LuArrowUp, LuArrowDown, LuX, LuInfo } from "react-icons/lu";

import { Button } from "@/components/Button";
import { Combobox } from "@/components/Combobox";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import Card from "@/components/Card";
import { keys, modifiers, keyDisplayMap } from "@/keyboardMappings";
import { MAX_KEYS_PER_STEP } from "@/constants/macros";

// Filter out modifier keys since they're handled in the modifiers section
const modifierKeyPrefixes = ['Alt', 'Control', 'Shift', 'Meta'];

const keyOptions = Object.keys(keys)
  .filter(key => !modifierKeyPrefixes.some(prefix => key.startsWith(prefix)))
  .map(key => ({
    value: key,
    label: keyDisplayMap[key] || key,
  }));

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

const PRESET_DELAYS = [
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
  isLastStep
}: MacroStepCardProps) {
  const getFilteredKeys = () => {
    const selectedKeys = ensureArray(step.keys);
    const availableKeys = keyOptions.filter(option => !selectedKeys.includes(option.value));
    
    if (keyQuery === '') {
      return availableKeys;
    } else {
      return availableKeys.filter(option => option.label.toLowerCase().includes(keyQuery.toLowerCase()));
    }
  };

  return (
    <Card className="p-4">
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-1.5">
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
          <span className="flex h-5 w-5 items-center justify-center rounded-full bg-blue-100 text-xs font-semibold text-blue-700 dark:bg-blue-900/40 dark:text-blue-200">
            {stepIndex + 1}
          </span>
        </div>
        
        <div className="flex items-center space-x-2">
          {onDelete && (
            <Button
              size="XS"
              theme="danger"
              text="Delete"
              onClick={onDelete}
            />
          )}
        </div>
      </div>
      
      <div className="space-y-4 mt-2">
        <div className="w-full flex flex-col gap-2">
          <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
            Modifiers
          </label>
          <div className="inline-flex flex-wrap gap-3">
            {Object.entries(groupedModifiers).map(([group, mods]) => (
              <div key={group} className="relative min-w-[120px] rounded-md border border-slate-200 dark:border-slate-700 p-2">
                <span className="absolute -top-2.5 left-2 px-1 text-xs font-medium bg-white dark:bg-slate-800 text-slate-500 dark:text-slate-400">
                  {group}
                </span>
                <div className="flex flex-wrap gap-1">
                  {mods.map(option => (
                    <label 
                      key={option.value} 
                      className={`flex items-center px-2 py-1 rounded border cursor-pointer text-xs font-medium transition-colors ${
                        ensureArray(step.modifiers).includes(option.value) 
                          ? 'bg-blue-100 border-blue-300 text-blue-700 dark:bg-blue-900/40 dark:border-blue-600 dark:text-blue-200' 
                          : 'bg-slate-100 border-slate-200 text-slate-600 hover:bg-slate-200 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-700'
                      }`}
                    >
                      <input
                        type="checkbox"
                        className="sr-only"
                        checked={ensureArray(step.modifiers).includes(option.value)}
                        onChange={e => {
                          const modifiersArray = ensureArray(step.modifiers);
                          const newModifiers = e.target.checked
                            ? [...modifiersArray, option.value]
                            : modifiersArray.filter(m => m !== option.value);
                          onModifierChange(newModifiers);
                        }}
                      />
                      {option.label.split(' ')[1] || option.label}
                    </label>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
        
        <div className="w-full flex flex-col gap-1">
          <div className="flex items-center gap-1">
            <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
              Keys
            </label>
            <div className="group relative cursor-pointer">
              <LuInfo className="h-4 w-4 text-slate-400 hover:text-slate-600 dark:text-slate-500 dark:hover:text-slate-400" />
              <div className="absolute left-1/2 top-full z-10 mt-1 hidden w-64 -translate-x-1/2 rounded-md bg-slate-800 px-3 py-2 text-xs text-white shadow-lg group-hover:block dark:bg-slate-700">
                <p>You can add up to a maximum of {MAX_KEYS_PER_STEP} keys to press per step.</p>
              </div>
            </div>
          </div>
          <div className="flex flex-wrap gap-1 pb-2">
            {ensureArray(step.keys).map((key, keyIndex) => (
              <span
                key={keyIndex}
                className="inline-flex items-center rounded-md bg-blue-100 px-1 text-xs font-medium text-blue-800 dark:bg-blue-900/40 dark:text-blue-200"
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
          <div className="relative w-full">
            <Combobox
              onChange={(value: { value: string; label: string }) => onKeySelect(value)}
              displayValue={() => keyQuery}
              onInputChange={onKeyQueryChange}
              options={getFilteredKeys}
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
            <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
              Step Duration
            </label>
            <div className="group relative cursor-pointer">
              <LuInfo className="h-4 w-4 text-slate-400 hover:text-slate-600 dark:text-slate-500 dark:hover:text-slate-400" />
              <div className="absolute left-1/2 top-full z-10 mt-1 hidden w-64 -translate-x-1/2 rounded-md bg-slate-800 px-3 py-2 text-xs text-white shadow-lg group-hover:block dark:bg-slate-700">
                <p>The time to wait after pressing the keys in this step before moving to the next step. This helps ensure reliable key presses when automating keyboard input.</p>
              </div>
            </div>
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