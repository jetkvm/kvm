import { useState, useEffect, useRef, useCallback } from "react";
import { LuPlus, LuTrash, LuX, LuPenLine, LuLoader, LuGripVertical, LuInfo, LuCopy, LuArrowUp, LuArrowDown } from "react-icons/lu";

import { KeySequence, useMacrosStore } from "@/hooks/stores";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { Button } from "@/components/Button";
import Checkbox from "@/components/Checkbox";
import { keys, modifiers, keyDisplayMap, modifierDisplayMap } from "@/keyboardMappings";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { SettingsItem } from "@/routes/devices.$id.settings";
import { InputFieldWithLabel, FieldError } from "@/components/InputField";
import Fieldset from "@/components/Fieldset";
import { SelectMenuBasic } from "@/components/SelectMenuBasic";
import EmptyCard from "@/components/EmptyCard";
import { Combobox } from "@/components/Combobox";
import { CardHeader } from "@/components/CardHeader";
import Card from "@/components/Card";

const DEFAULT_DELAY = 50;

interface MacroStep {
  keys: string[];
  modifiers: string[];
  delay: number;
}

interface KeyOption {
  value: string;
  label: string;
}

interface KeyOptionData {
  value: string | null;
  keys?: string[];
  label?: string;
}

const generateId = () => {
  return `macro-${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
};

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

const groupedModifiers = {
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

const MAX_STEPS_PER_MACRO = 10;
const MAX_TOTAL_MACROS = 25;
const MAX_KEYS_PER_STEP = 10;

const ensureArray = <T,>(arr: T[] | null | undefined): T[] => {
  return Array.isArray(arr) ? arr : [];
};

// Helper function to normalize sort orders, ensuring they start at 1 and have no gaps
const normalizeSortOrders = (macros: KeySequence[]): KeySequence[] => {
  return macros.map((macro, index) => ({
    ...macro,
    sortOrder: index + 1,
  }));
};

interface MacroStepCardProps {
  step: MacroStep;
  stepIndex: number;
  onDelete?: () => void;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  isDesktop: boolean;
  onKeySelect: (option: KeyOptionData) => void;
  onKeyQueryChange: (query: string) => void;
  keyQuery: string;
  getFilteredKeys: () => KeyOption[];
  onModifierChange: (modifiers: string[]) => void;
  onDelayChange: (delay: number) => void;
  isLastStep: boolean;
}

function MacroStepCard({
  step,
  stepIndex,
  onDelete,
  onMoveUp,
  onMoveDown,
  onKeySelect,
  onKeyQueryChange,
  keyQuery,
  getFilteredKeys,
  onModifierChange,
  onDelayChange,
  isLastStep
}: MacroStepCardProps) {
  return (
    <div className="macro-step-card rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-800 p-4 shadow-sm">
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
          <span className="macro-step-number flex h-5 w-5 items-center justify-center rounded-full bg-blue-100 text-xs font-semibold text-blue-700 dark:bg-blue-900/40 dark:text-blue-200">
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
              LeadingIcon={LuTrash}
              />
          )}
        </div>
      </div>
      
      <div className="space-y-4 mt-2">
        <div className="w-full flex flex-col gap-2">
          <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
            Modifiers:
          </label>
          <div className="macro-modifiers-container inline-flex flex-wrap gap-3">
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
                      <Checkbox
                        className="sr-only"
                        size="SM"
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
          <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
            Keys:
          </label>
          
          <div className="macro-key-group flex flex-wrap gap-1 pb-2">
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
            <Combobox<KeyOption>
              onChange={(value: KeyOption) => onKeySelect(value)}
              displayValue={() => keyQuery}
              onInputChange={onKeyQueryChange}
              options={getFilteredKeys}
              disabledMessage="Max keys reached"
              size="SM"
              disabled={ensureArray(step.keys).length >= MAX_KEYS_PER_STEP}
              placeholder={ensureArray(step.keys).length >= MAX_KEYS_PER_STEP ? "Max keys reached" : "Search for key..."}
              emptyMessage="No matching keys found"
            />
          </div>
        </div>
        
        <div className="w-full flex flex-col gap-1">
          <div className="flex items-center gap-1">
            <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
              Step Duration:
            </label>
            <div className="group relative">
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
    </div>
  );
}

// Helper to update step keys used by both new and edit flows
const updateStepKeys = (
  steps: MacroStep[],
  stepIndex: number,
  keyOption: { value: string | null; keys?: string[] },
  showTemporaryError: (msg: string) => void
) => {
  const newSteps = [...steps];
  
  // Check if the step at stepIndex exists
  if (!newSteps[stepIndex]) {
    console.error(`Step at index ${stepIndex} does not exist`);
    return steps; // Return original steps to avoid mutation
  }
  
  if (keyOption.keys) {
    newSteps[stepIndex].keys = keyOption.keys;
  } else if (keyOption.value) {
    // Initialize keys array if it doesn't exist
    if (!newSteps[stepIndex].keys) {
      newSteps[stepIndex].keys = [];
    }
    const keysArray = ensureArray(newSteps[stepIndex].keys);
    if (keysArray.length >= MAX_KEYS_PER_STEP) {
      showTemporaryError(`Maximum of ${MAX_KEYS_PER_STEP} keys per step allowed`);
      return newSteps;
    }
    newSteps[stepIndex].keys = [...keysArray, keyOption.value];
  }
  return newSteps;
};

const useTouchSort = (items: KeySequence[], onSort: (newItems: KeySequence[]) => void) => {
  const [touchStartY, setTouchStartY] = useState<number | null>(null);
  const [touchedIndex, setTouchedIndex] = useState<number | null>(null);

  const handleTouchStart = useCallback((e: React.TouchEvent, index: number) => {
    const touch = e.touches[0];
    setTouchStartY(touch.clientY);
    setTouchedIndex(index);
    
    const element = e.currentTarget as HTMLElement;
    const rect = element.getBoundingClientRect();
    
    // Create ghost element
    const ghost = element.cloneNode(true) as HTMLElement;
    ghost.id = 'ghost-macro';
    ghost.className = 'macro-sortable ghost';
    ghost.style.height = `${rect.height}px`;
    element.parentNode?.insertBefore(ghost, element);
    
    // Set up dragged element
    element.style.position = 'fixed';
    element.style.left = `${rect.left}px`;
    element.style.top = `${rect.top}px`;
    element.style.width = `${rect.width}px`;
    element.style.zIndex = '50';
  }, []);

  const handleTouchMove = useCallback((e: React.TouchEvent) => {
    if (touchStartY === null || touchedIndex === null) return;
    
    const touch = e.touches[0];
    const deltaY = touch.clientY - touchStartY;
    const element = e.currentTarget as HTMLElement;
    
    // Smooth movement of dragged element
    element.style.transform = `translateY(${deltaY}px)`;
    
    const macroElements = document.querySelectorAll('[data-macro-item]');
    const draggedRect = element.getBoundingClientRect();
    const draggedMiddle = draggedRect.top + draggedRect.height / 2;
    
    macroElements.forEach((el, i) => {
      if (i === touchedIndex) return;
      
      const rect = el.getBoundingClientRect();
      const elementMiddle = rect.top + rect.height / 2;
      const distance = Math.abs(draggedMiddle - elementMiddle);
      
      if (distance < rect.height) {
        const direction = draggedMiddle > elementMiddle ? -1 : 1;
        (el as HTMLElement).style.transform = `translateY(${direction * rect.height}px)`;
        (el as HTMLElement).style.transition = 'transform 0.15s ease-out';
      } else {
        (el as HTMLElement).style.transform = '';
        (el as HTMLElement).style.transition = 'transform 0.15s ease-out';
      }
    });
  }, [touchStartY, touchedIndex]);

  const handleTouchEnd = useCallback(async (e: React.TouchEvent) => {
    if (touchedIndex === null) return;
    
    const element = e.currentTarget as HTMLElement;
    const touch = e.changedTouches[0];
    
    // Remove ghost element
    const ghost = document.getElementById('ghost-macro');
    ghost?.parentNode?.removeChild(ghost);
    
    // Reset dragged element styles
    element.style.position = '';
    element.style.left = '';
    element.style.top = '';
    element.style.width = '';
    element.style.zIndex = '';
    element.style.transform = '';
    element.style.boxShadow = '';
    element.style.transition = '';
    
    const macroElements = document.querySelectorAll('[data-macro-item]');
    let targetIndex = touchedIndex;
    
    // Find the closest element to the final touch position
    const finalY = touch.clientY;
    let closestDistance = Infinity;
    
    macroElements.forEach((el, i) => {
      if (i === touchedIndex) return;
      
      const rect = el.getBoundingClientRect();
      const distance = Math.abs(finalY - (rect.top + rect.height / 2));
      
      if (distance < closestDistance) {
        closestDistance = distance;
        targetIndex = i;
      }
      
      // Reset other elements
      (el as HTMLElement).style.transform = '';
      (el as HTMLElement).style.transition = '';
    });
    
    if (targetIndex !== touchedIndex && closestDistance < 50) {
      const itemsCopy = [...items];
      const [draggedItem] = itemsCopy.splice(touchedIndex, 1);
      itemsCopy.splice(targetIndex, 0, draggedItem);
      onSort(itemsCopy);
    }
    
    setTouchStartY(null);
    setTouchedIndex(null);
  }, [touchedIndex, items, onSort]);

  return { handleTouchStart, handleTouchMove, handleTouchEnd };
};

interface StepError {
  keys?: string;
  modifiers?: string;
  delay?: string;
}

interface ValidationErrors {
  name?: string;
  description?: string;
  steps?: Record<number, StepError>;
}

export default function SettingsMacrosRoute() {
  const { macros, loading, initialized, loadMacros, saveMacros, setSendFn } = useMacrosStore();
  const [editingMacro, setEditingMacro] = useState<KeySequence | null>(null);
  const [newMacro, setNewMacro] = useState<Partial<KeySequence>>({
    name: "",
    description: "",
    steps: [{ keys: [], modifiers: [], delay: DEFAULT_DELAY }],
  });
  const [isDragging, setIsDragging] = useState(false);
  const dragItem = useRef<number | null>(null);
  const dragOverItem = useRef<number | null>(null);
  
  const [macroToDelete, setMacroToDelete] = useState<string | null>(null);
  
  const [keyQueries, setKeyQueries] = useState<Record<number, string>>({});
  const [editKeyQueries, setEditKeyQueries] = useState<Record<number, string>>({});
  
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isDesktop, setIsDesktop] = useState(window.innerWidth >= 768);
  
  const [send] = useJsonRpc();

  const isMaxMacrosReached = macros.length >= MAX_TOTAL_MACROS;
  const isMaxStepsReachedForNewMacro = (newMacro.steps?.length || 0) >= MAX_STEPS_PER_MACRO;
  
  const showTemporaryError = useCallback((message: string) => {
    setErrorMessage(message);
    setTimeout(() => setErrorMessage(null), 3000);
  }, []);
  
  // Helper for both new and edit key select
  const handleKeySelectUpdate = (stepIndex: number, option: KeyOptionData, isEditing = false) => {
    if (isEditing && editingMacro) {
      const updatedSteps = updateStepKeys(editingMacro.steps, stepIndex, option, showTemporaryError);
      setEditingMacro({ ...editingMacro, steps: updatedSteps });
    } else {
      const updatedSteps = updateStepKeys(newMacro.steps || [], stepIndex, option, showTemporaryError);
      setNewMacro({ ...newMacro, steps: updatedSteps });
    }
  };
  
  const handleKeySelect = (stepIndex: number, option: KeyOptionData) => {
    handleKeySelectUpdate(stepIndex, option, false);
  };
  
  const handleEditKeySelect = (stepIndex: number, option: KeyOptionData) => {
    handleKeySelectUpdate(stepIndex, option, true);
  };
  
  const handleKeyQueryChange = (stepIndex: number, query: string) => {
    setKeyQueries(prev => ({ ...prev, [stepIndex]: query }));
  };
  
  const handleEditKeyQueryChange = (stepIndex: number, query: string) => {
    setEditKeyQueries(prev => ({ ...prev, [stepIndex]: query }));
  };
  
  const getFilteredKeys = (stepIndex: number, isEditing = false) => {
    const query = isEditing 
      ? (editKeyQueries[stepIndex] || '')
      : (keyQueries[stepIndex] || '');
    
    const currentStep = isEditing 
      ? editingMacro?.steps[stepIndex] 
      : newMacro.steps?.[stepIndex];
    
    const selectedKeys = ensureArray(currentStep?.keys);
    
    const availableKeys = keyOptions.filter(option => !selectedKeys.includes(option.value));
    
    if (query === '') {
      return availableKeys;
    } else {
      return availableKeys.filter(option => option.label.toLowerCase().includes(query.toLowerCase()));
    }
  };
  
  useEffect(() => {
    setSendFn(send);
    if (!initialized) {
      loadMacros();
    }
  }, [initialized, loadMacros, setSendFn, send]);
  
  const [errors, setErrors] = useState<ValidationErrors>({});
  
  const clearErrors = useCallback(() => {
    setErrors({});
  }, []);
  
  const validateMacro = (macro: Partial<KeySequence>): ValidationErrors => {
    const errors: ValidationErrors = {};

    // Name validation
    if (!macro.name?.trim()) {
      errors.name = "Name is required";
    } else if (macro.name.trim().length > 50) {
      errors.name = "Name must be less than 50 characters";
    }

    // Description validation (optional)
    if (macro.description && macro.description.trim().length > 200) {
      errors.description = "Description must be less than 200 characters";
    }

    // Steps validation
    if (!macro.steps?.length) {
      errors.steps = { 0: { keys: "At least one step is required" } };
      return errors;
    }

    // Check if at least one step has keys or modifiers
    const hasKeyOrModifier = macro.steps.some(step => 
      (step.keys?.length || 0) > 0 || (step.modifiers?.length || 0) > 0
    );

    if (!hasKeyOrModifier) {
      errors.steps = { 0: { keys: "At least one step must have keys or modifiers" } };
      return errors;
    }

    const stepErrors: Record<number, StepError> = {};
    
    macro.steps.forEach((step, index) => {
      const stepError: StepError = {};

      // Keys validation (only if keys are present)
      if (step.keys?.length && step.keys.length > MAX_KEYS_PER_STEP) {
        stepError.keys = `Maximum ${MAX_KEYS_PER_STEP} keys allowed`;
      }

      // Delay validation
      if (typeof step.delay !== 'number' || step.delay < 0) {
        stepError.delay = "Invalid delay value";
      }

      if (Object.keys(stepError).length > 0) {
        stepErrors[index] = stepError;
      }
    });

    if (Object.keys(stepErrors).length > 0) {
      errors.steps = stepErrors;
    }

    return errors;
  };

  const resetNewMacro = () => {
    setNewMacro({
      name: "",
      description: "",
      steps: [{ keys: [], modifiers: [], delay: DEFAULT_DELAY }],
    });
    setKeyQueries({});
    setErrors({});
  };

  const [isSaving, setIsSaving] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const handleAddMacro = useCallback(async () => {
    if (isMaxMacrosReached) {
      showTemporaryError(`Maximum of ${MAX_TOTAL_MACROS} macros allowed`);
      return;
    }

    const validationErrors = validateMacro(newMacro);
    if (Object.keys(validationErrors).length > 0) {
      setErrors(validationErrors);
      return;
    }

    setIsSaving(true);
    try {
      const macro: KeySequence = {
        id: generateId(),
        name: newMacro.name!.trim(),
        description: newMacro.description?.trim() || "",
        steps: newMacro.steps || [],
        sortOrder: macros.length + 1,
      };

      await saveMacros(normalizeSortOrders([...macros, macro]));
      resetNewMacro();
      setShowAddMacro(false);
      notifications.success(`Macro "${macro.name}" created successfully`);
    } catch (error) {
      if (error instanceof Error) {
        notifications.error(`Failed to create macro: ${error.message}`);
        showTemporaryError(error.message);
      } else {
        notifications.error("Failed to create macro");
        showTemporaryError("Failed to save macro");
      }
    } finally {
      setIsSaving(false);
    }
  }, [isMaxMacrosReached, newMacro, macros, saveMacros, showTemporaryError]);

  const handleDragStart = (index: number) => {
    dragItem.current = index;
    setIsDragging(true);
    
    const allItems = document.querySelectorAll('[data-macro-item]');
    const draggedElement = allItems[index];
    if (draggedElement) {
      draggedElement.classList.add('dragging');
    }
  };
  
  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    dragOverItem.current = index;
    
    const allItems = document.querySelectorAll('[data-macro-item]');
    allItems.forEach(el => el.classList.remove('drop-target'));
    
    const targetElement = allItems[index];
    if (targetElement) {
      targetElement.classList.add('drop-target');
    }
  };
  
  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    if (dragItem.current === null || dragOverItem.current === null) return;
    
    const macroCopy = [...macros];
    const draggedItem = macroCopy.splice(dragItem.current, 1)[0];
    macroCopy.splice(dragOverItem.current, 0, draggedItem);
    const updatedMacros = normalizeSortOrders(macroCopy);

    try {
      await saveMacros(updatedMacros);
      notifications.success("Macro order updated successfully");
    } catch (error) {
      if (error instanceof Error) {
        notifications.error(`Failed to reorder macros: ${error.message}`);
        showTemporaryError(error.message);
      } else {
        notifications.error("Failed to reorder macros");
        showTemporaryError("Failed to save reordered macros");
      }
    }
    
    const allItems = document.querySelectorAll('[data-macro-item]');
    allItems.forEach(el => {
      el.classList.remove('drop-target');
      el.classList.remove('dragging');
    });
    
    dragItem.current = null;
    dragOverItem.current = null;
    setIsDragging(false);
  };

  const handleUpdateMacro = useCallback(async () => {
    if (!editingMacro) return;

    const validationErrors = validateMacro(editingMacro);
    if (Object.keys(validationErrors).length > 0) {
      setErrors(validationErrors);
      return;
    }

    setIsUpdating(true);
    try {
      const newMacros = macros.map(m => 
        m.id === editingMacro.id ? {
          ...editingMacro,
          name: editingMacro.name.trim(),
          description: editingMacro.description?.trim() || "",
        } : m
      );

      await saveMacros(normalizeSortOrders(newMacros));
      setEditingMacro(null);
      clearErrors();
      notifications.success(`Macro "${editingMacro.name}" updated successfully`);
    } catch (error) {
      if (error instanceof Error) {
        notifications.error(`Failed to update macro: ${error.message}`);
        showTemporaryError(error.message);
      } else {
        notifications.error("Failed to update macro");
        showTemporaryError("Failed to update macro");
      }
    } finally {
      setIsUpdating(false);
    }
  }, [editingMacro, macros, saveMacros, showTemporaryError, clearErrors]);

  const handleEditMacro = (macro: KeySequence) => {
    setEditingMacro({
      ...macro,
      description: macro.description || "",
      steps: macro.steps.map(step => ({
        ...step,
        keys: ensureArray(step.keys),
        modifiers: ensureArray(step.modifiers),
        delay: typeof step.delay === 'number' ? step.delay : DEFAULT_DELAY
      }))
    });
    clearErrors();
    setEditKeyQueries({});
  };

  const handleDeleteMacro = async (id: string) => {
    const macroToBeDeleted = macros.find(m => m.id === id);
    if (!macroToBeDeleted) return;

    setIsDeleting(true);
    try {
      const updatedMacros = normalizeSortOrders(macros.filter(macro => macro.id !== id));
      await saveMacros(updatedMacros);
      if (editingMacro?.id === id) {
        setEditingMacro(null);
      }
      setMacroToDelete(null);
      notifications.success(`Macro "${macroToBeDeleted.name}" deleted successfully`);
    } catch (error) {
      if (error instanceof Error) {
        notifications.error(`Failed to delete macro: ${error.message}`);
        showTemporaryError(error.message);
      } else {
        notifications.error("Failed to delete macro");
        showTemporaryError("Failed to delete macro");
      }
    } finally {
      setIsDeleting(false);
    }
  };

  const handleDuplicateMacro = async (macro: KeySequence) => {
    if (isMaxMacrosReached) {
      showTemporaryError(`Maximum of ${MAX_TOTAL_MACROS} macros allowed`);
      return;
    }

    const newMacroCopy: KeySequence = {
      ...JSON.parse(JSON.stringify(macro)),
      id: generateId(),
      name: `${macro.name} (copy)`,
      sortOrder: macros.length + 1,
    };

    newMacroCopy.steps = newMacroCopy.steps.map(step => ({
      ...step,
      keys: ensureArray(step.keys),
      modifiers: ensureArray(step.modifiers),
      delay: step.delay || DEFAULT_DELAY
    }));

    try {
      await saveMacros(normalizeSortOrders([...macros, newMacroCopy]));
      notifications.success(`Macro "${newMacroCopy.name}" duplicated successfully`);
    } catch (error) {
      if (error instanceof Error) {
        notifications.error(`Failed to duplicate macro: ${error.message}`);
        showTemporaryError(error.message);
      } else {
        notifications.error("Failed to duplicate macro");
        showTemporaryError("Failed to duplicate macro");
      }
    }
  };

  const handleStepMove = (stepIndex: number, direction: 'up' | 'down', steps: MacroStep[]) => {
    const newSteps = [...steps];
    const newIndex = direction === 'up' ? stepIndex - 1 : stepIndex + 1;
    [newSteps[stepIndex], newSteps[newIndex]] = [newSteps[newIndex], newSteps[stepIndex]];
    return newSteps;
  };

  useEffect(() => {
    const handleResize = () => {
      setIsDesktop(window.innerWidth >= 768);
    };
    
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  const { handleTouchStart, handleTouchMove, handleTouchEnd } = useTouchSort(
    macros,
    async (newMacros) => {
      const updatedMacros = normalizeSortOrders(newMacros);
      try {
        await saveMacros(updatedMacros);
        notifications.success("Macro order updated successfully");
      } catch (error) {
        if (error instanceof Error) {
          notifications.error(`Failed to reorder macros: ${error.message}`);
          showTemporaryError(error.message);
        } else {
          notifications.error("Failed to reorder macros");
          showTemporaryError("Failed to save reordered macros");
        }
      }
    }
  );

  const [showClearConfirm, setShowClearConfirm] = useState(false);
  const [showAddMacro, setShowAddMacro] = useState(false);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && editingMacro) {
        setEditingMacro(null);
        setErrors({});
      }
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
        if (editingMacro) {
          handleUpdateMacro();
        } else if (!isMaxMacrosReached) {
          handleAddMacro();
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [editingMacro, isMaxMacrosReached, handleAddMacro, handleUpdateMacro]);

  const handleModifierChange = (stepIndex: number, modifiers: string[]) => {
    if (editingMacro) {
      const newSteps = [...editingMacro.steps];
      newSteps[stepIndex].modifiers = modifiers;
      setEditingMacro({ ...editingMacro, steps: newSteps });
    } else {
      const newSteps = [...(newMacro.steps || [])];
      newSteps[stepIndex].modifiers = modifiers;
      setNewMacro({ ...newMacro, steps: newSteps });
    }
  };

  const handleDelayChange = (stepIndex: number, delay: number) => {
    if (editingMacro) {
      const newSteps = [...editingMacro.steps];
      newSteps[stepIndex].delay = delay;
      setEditingMacro({ ...editingMacro, steps: newSteps });
    } else {
      const newSteps = [...(newMacro.steps || [])];
      newSteps[stepIndex].delay = delay;
      setNewMacro({ ...newMacro, steps: newSteps });
    }
  };

  const ErrorMessage = ({ error }: { error?: string }) => {
    if (!error) return null;
    return (
      <FieldError error={error} />
    );
  };

  return (
    <div className="space-y-4">
          <SettingsPageHeader
            title="Keyboard Macros"
            description="Create and manage keyboard macros for quick actions"
        />
        {macros.length > 0 && (
          <div className="flex items-center justify-between mb-4">
            <SettingsItem 
              title="Macros"
              description={`${macros.length}/${MAX_TOTAL_MACROS}`}
            >
              <div className="flex items-center gap-2">
                {!showAddMacro && (
                  <Button
                    size="SM"
                    theme="primary"
                    text={isMaxMacrosReached ? `Max Reached` : "Add New Macro"}
                    onClick={() => setShowAddMacro(true)}
                    disabled={isMaxMacrosReached}
                  />
                )}
              </div>
            </SettingsItem>
          </div>
        )}

      {errorMessage && (<FieldError error={errorMessage} />)}

      {loading && (
        <div className="flex items-center justify-center p-8">
          <LuLoader className="h-6 w-6 animate-spin text-blue-500" />
        </div>
      )}
      <div className={`space-y-4 ${loading ? 'hidden' : ''}`}>
        {showAddMacro && (
          <Card className="p-3">
            <CardHeader
              headline="Add New Macro"
            />
            <Fieldset className="mt-4">
              <InputFieldWithLabel
                type="text"
                label="Macro Name"
                placeholder="Macro Name"
                value={newMacro.name}
                error={errors.name}
                onChange={e => {
                  setNewMacro(prev => ({ ...prev, name: e.target.value }));
                  if (errors.name) {
                    const newErrors = { ...errors };
                    delete newErrors.name;
                    setErrors(newErrors);
                  }
                }}
              />
            </Fieldset>
            
            <div className="mt-4">
              <div className="macro-section-header">
                <label className="macro-section-title">
                  Steps:
                </label>
                <span className="macro-section-subtitle">
                  {newMacro.steps?.length || 0}/{MAX_STEPS_PER_MACRO} steps
                </span>
              </div>
              {errors.steps && errors.steps[0]?.keys && (
                <div className="mt-2">
                  <ErrorMessage error={errors.steps[0].keys} />
                </div>
              )}
              <div className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                You can add up to {MAX_STEPS_PER_MACRO} steps per macro
              </div>
              <Fieldset>
                <div className="mt-2 space-y-4">
                  {(newMacro.steps || []).map((step, stepIndex) => (
                    <MacroStepCard
                      key={stepIndex}
                      step={step}
                      stepIndex={stepIndex}
                      onDelete={newMacro.steps && newMacro.steps.length > 1 ? () => {
                        const newSteps = [...(newMacro.steps || [])];
                        newSteps.splice(stepIndex, 1);
                        setNewMacro(prev => ({ ...prev, steps: newSteps }));
                      } : undefined}
                      onMoveUp={() => {
                        const newSteps = handleStepMove(stepIndex, 'up', newMacro.steps || []);
                        setNewMacro(prev => ({ ...prev, steps: newSteps }));
                      }}
                      onMoveDown={() => {
                        const newSteps = handleStepMove(stepIndex, 'down', newMacro.steps || []);
                        setNewMacro(prev => ({ ...prev, steps: newSteps }));
                      }}
                      isDesktop={isDesktop}
                      onKeySelect={(option) => handleKeySelect(stepIndex, option)}
                      onKeyQueryChange={(query) => handleKeyQueryChange(stepIndex, query)}
                      keyQuery={keyQueries[stepIndex] || ''}
                      getFilteredKeys={() => getFilteredKeys(stepIndex)}
                      onModifierChange={(modifiers) => handleModifierChange(stepIndex, modifiers)}
                      onDelayChange={(delay) => handleDelayChange(stepIndex, delay)}
                      isLastStep={stepIndex === (newMacro.steps?.length || 0) - 1}
                    />
                  ))}
                </div>
              </Fieldset>

              <div className="mt-4 border-t border-slate-200 pt-4 dark:border-slate-700">
                <Button
                  size="MD"
                  theme="light"
                  fullWidth
                  LeadingIcon={LuPlus}
                  text={`Add Step ${isMaxStepsReachedForNewMacro ? `(${MAX_STEPS_PER_MACRO} max)` : ''}`}
                  onClick={() => {
                    if (isMaxStepsReachedForNewMacro) {
                      showTemporaryError(`You can only add a maximum of ${MAX_STEPS_PER_MACRO} steps per macro.`);
                      return;
                    }
                    
                    setNewMacro(prev => ({
                      ...prev,
                      steps: [
                        ...(prev.steps || []), 
                        { keys: [], modifiers: [], delay: DEFAULT_DELAY }
                      ],
                    }));
                    clearErrors();
                  }}
                  disabled={isMaxStepsReachedForNewMacro}
                />
              </div>

              <div className="mt-6 flex items-center justify-between border-t border-slate-200 pt-4 dark:border-slate-700">
                {showClearConfirm ? (
                  <div className="flex items-center gap-2">
                    <span className="text-sm text-slate-600 dark:text-slate-400">
                      Cancel changes?
                    </span>
                    <Button
                      size="SM"
                      theme="danger"
                      text="Yes"
                      onClick={() => {
                        resetNewMacro();
                        setShowAddMacro(false);
                        setShowClearConfirm(false);
                      }}
                    />
                    <Button
                      size="SM"
                      theme="light"
                      text="No"
                      onClick={() => setShowClearConfirm(false)}
                    />
                  </div>
                ) : (
                  <div className="flex gap-x-2">
                    <Button
                      size="SM"
                      theme="primary"
                      text={isSaving ? "Saving..." : "Save Macro"}
                      onClick={handleAddMacro}
                      disabled={isSaving}
                    />
                    <Button
                      size="SM"
                      theme="light"
                      text="Cancel"
                      onClick={() => {
                        if (newMacro.name || newMacro.description || newMacro.steps?.some(s => s.keys?.length || s.modifiers?.length)) {
                          setShowClearConfirm(true);
                        } else {
                          resetNewMacro();
                          setShowAddMacro(false);
                        }
                      }}
                    />
                  </div>
                )}
              </div>
            </div>
          </Card>
        )}
        {macros.length === 0 && !showAddMacro && (
          <EmptyCard
            headline="No macros created yet"
            BtnElm={
              <Button
                size="SM"
                theme="primary"
                text="Add New Macro"
                onClick={() => setShowAddMacro(true)}
                disabled={isMaxMacrosReached}
              />
            }
          />
        )}
        {macros.length > 0 && (
          <div className="space-y-1">
            {macros.map((macro, index) => 
              editingMacro && editingMacro.id === macro.id ? (
                <Card key={macro.id} className="border-blue-300 bg-blue-50 p-3 dark:border-blue-700 dark:bg-blue-900/20">
                  <CardHeader
                    headline="Edit Macro"
                  />
                  <Fieldset className="mt-4">
                    <InputFieldWithLabel
                      type="text"
                      label="Macro Name"
                      placeholder="Macro Name"
                      value={editingMacro.name}
                      error={errors.name}
                      onChange={e => {
                        setEditingMacro({ ...editingMacro, name: e.target.value });
                        if (errors.name) {
                          const newErrors = { ...errors };
                          delete newErrors.name;
                          setErrors(newErrors);
                        }
                      }}
                    />
                  </Fieldset>
                  
                  <div className="mt-4">
                    <div className="flex items-center justify-between">
                      <label className="text-sm font-medium text-slate-700 dark:text-slate-300">
                        Steps:
                      </label>
                      <span className="text-sm text-slate-500 dark:text-slate-400">
                        {editingMacro.steps.length}/{MAX_STEPS_PER_MACRO} steps
                      </span>
                    </div>
                    {errors.steps && errors.steps[0]?.keys && (
                      <div className="mt-2">
                        <ErrorMessage error={errors.steps[0].keys} />
                      </div>
                    )}
                    <div className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                      You can add up to {MAX_STEPS_PER_MACRO} steps per macro
                    </div>
                    <Fieldset>
                      <div className="mt-2 space-y-4">
                        {editingMacro.steps.map((step, stepIndex) => (
                          <MacroStepCard
                            key={stepIndex}
                            step={step}
                            stepIndex={stepIndex}
                            onDelete={editingMacro.steps.length > 1 ? () => {
                              const newSteps = [...editingMacro.steps];
                              newSteps.splice(stepIndex, 1);
                              setEditingMacro({ ...editingMacro, steps: newSteps });
                            } : undefined}
                            onMoveUp={() => {
                              const newSteps = handleStepMove(stepIndex, 'up', editingMacro.steps);
                              setEditingMacro({ ...editingMacro, steps: newSteps });
                            }}
                            onMoveDown={() => {
                              const newSteps = handleStepMove(stepIndex, 'down', editingMacro.steps);
                              setEditingMacro({ ...editingMacro, steps: newSteps });
                            }}
                            isDesktop={isDesktop}
                            onKeySelect={(option) => handleEditKeySelect(stepIndex, option)}
                            onKeyQueryChange={(query) => handleEditKeyQueryChange(stepIndex, query)}
                            keyQuery={editKeyQueries[stepIndex] || ''}
                            getFilteredKeys={() => getFilteredKeys(stepIndex, true)}
                            onModifierChange={(modifiers) => handleModifierChange(stepIndex, modifiers)}
                            onDelayChange={(delay) => handleDelayChange(stepIndex, delay)}
                            isLastStep={stepIndex === editingMacro.steps.length - 1}
                          />
                        ))}
                      </div>
                    </Fieldset>
                    
                    <div className="mt-4 border-t border-slate-200 pt-4 dark:border-slate-700">
                      <Button
                        size="MD"
                        theme="light"
                        fullWidth
                        LeadingIcon={LuPlus}
                        text={`Add Step ${editingMacro.steps.length >= MAX_STEPS_PER_MACRO ? `(${MAX_STEPS_PER_MACRO} max)` : ''}`}
                        onClick={() => {
                          if (editingMacro.steps.length >= MAX_STEPS_PER_MACRO) {
                            showTemporaryError(`You can only add a maximum of ${MAX_STEPS_PER_MACRO} steps per macro.`);
                            return;
                          }
                          
                          setEditingMacro({
                            ...editingMacro,
                            steps: [
                              ...editingMacro.steps, 
                              { keys: [], modifiers: [], delay: DEFAULT_DELAY }
                            ],
                          });
                          clearErrors();
                        }}
                        disabled={editingMacro.steps.length >= MAX_STEPS_PER_MACRO}
                      />
                    </div>

                    <div className="mt-4 flex items-center justify-between border-t border-slate-200 pt-4 dark:border-slate-700">
                      <div className="flex gap-x-2">
                        <Button
                          size="SM"
                          theme="primary"
                          text={isUpdating ? "Saving..." : "Save Changes"}
                          onClick={handleUpdateMacro}
                          disabled={isUpdating}
                        />
                        <Button
                          size="SM"
                          theme="light"
                          text="Cancel"
                          onClick={() => {
                            setEditingMacro(null);
                            setErrors({});
                          }}
                        />
                      </div>
                    </div>
                  </div>
                </Card>
              ) : (
                <div
                  key={macro.id}
                  data-macro-item={index}
                  draggable={!editingMacro}
                  onDragStart={() => handleDragStart(index)}
                  onDragOver={e => handleDragOver(e, index)}
                  onDragEnd={() => {
                    const allItems = document.querySelectorAll('[data-macro-item]');
                    allItems.forEach(el => {
                      el.classList.remove('drop-target');
                      el.classList.remove('dragging');
                    });
                    setIsDragging(false);
                  }}
                  onDrop={handleDrop}
                  onTouchStart={(e) => handleTouchStart(e, index)}
                  onTouchMove={handleTouchMove}
                  onTouchEnd={handleTouchEnd}
                  className={`macro-sortable flex items-center justify-between rounded-md border border-slate-200 p-2 dark:border-slate-700 ${
                    isDragging && dragItem.current === index
                      ? "bg-blue-50 dark:bg-blue-900/20"
                      : "bg-white dark:bg-slate-800"
                  }`}
                >
                  <div className="macro-sortable-handle">
                    <LuGripVertical className="h-4 w-4" />
                  </div>
                  
                  <div className="flex-1 overflow-hidden">
                    <h4 className="truncate text-sm font-medium text-black dark:text-white">
                      {macro.name}
                    </h4>
                    <p className="mt-1 text-xs text-slate-500 dark:text-slate-400 overflow-hidden">
                      <span className="flex flex-wrap items-center">
                        {macro.steps.slice(0, 3).map((step, stepIndex) => {
                          const keysText = ensureArray(step.keys).length > 0 
                            ? ensureArray(step.keys).map(key => keyDisplayMap[key] || key).join(' + ') 
                            : '';
                          const modifiersDisplayText = ensureArray(step.modifiers).length > 0
                            ? ensureArray(step.modifiers).map(m => modifierDisplayMap[m] || m).join(' + ')
                            : '';
                          const combinedText = (modifiersDisplayText || keysText) 
                            ? [modifiersDisplayText, keysText].filter(Boolean).join(' + ')
                            : 'Delay only';
                          return (
                            <span key={stepIndex} className="inline-flex items-center my-0.5">
                              {stepIndex > 0 && <span className="mx-1 text-blue-400 dark:text-blue-500">→</span>}
                              <span className="whitespace-nowrap">
                                <span className="font-medium text-slate-600 dark:text-slate-300">{combinedText}</span>
                                <span className="ml-1 text-slate-400 dark:text-slate-500">({step.delay}ms)</span>
                              </span>
                            </span>
                          );
                        })}
                        {macro.steps.length > 3 && (
                          <span className="ml-1 text-slate-400 dark:text-slate-500">
                            + {macro.steps.length - 3} more steps
                          </span>
                        )}
                      </span>
                    </p>
                  </div>
                  
                  <div className="flex gap-1 ml-2 flex-shrink-0">
                    {macroToDelete === macro.id ? (
                      <div className="flex items-center gap-2">
                        <span className="text-sm text-slate-600 dark:text-slate-400">
                          Delete macro?
                        </span>
                        <div className="flex items-center gap-x-2">
                          <Button
                            size="XS"
                            theme="danger"
                            text="Yes"
                            onClick={() => {
                              handleDeleteMacro(macro.id);
                            }}
                            disabled={isDeleting}
                          />
                          <Button
                            size="XS"
                            theme="light"
                            text="No"
                            onClick={() => setMacroToDelete(null)}
                          />
                        </div>
                      </div>
                    ) : (
                      <>
                        <Button
                          size="XS"
                          theme="light"
                          LeadingIcon={LuPenLine}
                          onClick={() => handleEditMacro(macro)}
                        />
                        <Button
                          size="XS"
                          theme="light"
                          LeadingIcon={LuCopy}
                          onClick={() => handleDuplicateMacro(macro)}
                        />
                        <Button
                          size="XS"
                          theme="light"
                          LeadingIcon={LuTrash}
                          onClick={() => setMacroToDelete(macro.id)}
                          className="text-red-500 dark:text-red-400"
                        />
                      </>
                    )}
                  </div>
                </div>
              )
            )}
          </div>
        )}
      </div>
    </div>
  );
}
