import { useEffect, Fragment, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { LuPenLine, LuLoader, LuCopy, LuMoveRight, LuCornerDownRight, LuArrowUp, LuArrowDown } from "react-icons/lu";

import { KeySequence, useMacrosStore, generateMacroId } from "@/hooks/stores";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { Button } from "@/components/Button";
import EmptyCard from "@/components/EmptyCard";
import Card from "@/components/Card";
import { MAX_TOTAL_MACROS, COPY_SUFFIX } from "@/constants/macros";
import { keyDisplayMap, modifierDisplayMap } from "@/keyboardMappings";
import notifications from "@/notifications";
import { SettingsItem } from "@/routes/devices.$id.settings";

const normalizeSortOrders = (macros: KeySequence[]): KeySequence[] => {
  return macros.map((macro, index) => ({
    ...macro,
    sortOrder: index + 1,
  }));
};

export default function SettingsMacrosRoute() {
  const { macros, loading, initialized, loadMacros, saveMacros } = useMacrosStore();
  const navigate = useNavigate();
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null);
  
  const isMaxMacrosReached = useMemo(() => 
    macros.length >= MAX_TOTAL_MACROS, 
    [macros.length]
  );

  useEffect(() => {
    if (!initialized) {
      loadMacros();
    }
  }, [initialized, loadMacros]);

  const handleDuplicateMacro = async (macro: KeySequence) => {
    if (!macro?.id || !macro?.name) {
      notifications.error("Invalid macro data");
      return;
    }

    if (isMaxMacrosReached) {
      notifications.error(`Maximum of ${MAX_TOTAL_MACROS} macros allowed`);
      return;
    }

    setActionLoadingId(macro.id);

    const newMacroCopy: KeySequence = {
      ...JSON.parse(JSON.stringify(macro)),
      id: generateMacroId(),
      name: `${macro.name} ${COPY_SUFFIX}`,
      sortOrder: macros.length + 1,
    };

    try {
      await saveMacros(normalizeSortOrders([...macros, newMacroCopy]));
      notifications.success(`Macro "${newMacroCopy.name}" duplicated successfully`);
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(`Failed to duplicate macro: ${error.message}`);
      } else {
        notifications.error("Failed to duplicate macro");
      }
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleMoveMacro = async (index: number, direction: 'up' | 'down', macroId: string) => {
    if (!Array.isArray(macros) || macros.length === 0) {
      notifications.error("No macros available");
      return;
    }

    const newIndex = direction === 'up' ? index - 1 : index + 1;
    if (newIndex < 0 || newIndex >= macros.length) return;

    setActionLoadingId(macroId);

    try {
      const newMacros = [...macros];
      [newMacros[index], newMacros[newIndex]] = [newMacros[newIndex], newMacros[index]];
      const updatedMacros = normalizeSortOrders(newMacros);

      await saveMacros(updatedMacros);
      notifications.success("Macro order updated successfully");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(`Failed to reorder macros: ${error.message}`);
      } else {
        notifications.error("Failed to reorder macros");
      }
    } finally {
      setActionLoadingId(null);
    }
  };

  const MacroList = useMemo(() => (
    <div className="space-y-2">
      {macros.map((macro, index) => (
        <Card key={macro.id} className="p-2 bg-white dark:bg-slate-800">
          <div className="flex items-center justify-between">
            <div className="flex flex-col gap-1 px-2">
              <Button
                size="XS"
                theme="light"
                onClick={() => handleMoveMacro(index, 'up', macro.id)}
                disabled={index === 0 || actionLoadingId === macro.id}
                LeadingIcon={LuArrowUp}
                aria-label={`Move ${macro.name} up`}
              />
              <Button
                size="XS"
                theme="light"
                onClick={() => handleMoveMacro(index, 'down', macro.id)}
                disabled={index === macros.length - 1 || actionLoadingId === macro.id}
                LeadingIcon={LuArrowDown}
                aria-label={`Move ${macro.name} down`}
              />
            </div>

            <div className="flex-1 min-w-0 flex flex-col justify-center ml-2">
              <h3 className="truncate text-sm font-semibold text-black dark:text-white">
                {macro.name}
              </h3>
              <p className="mt-1 ml-2 text-xs text-slate-500 dark:text-slate-400 overflow-hidden">
                <span className="flex flex-col items-start gap-1">
                  {macro.steps.map((step, stepIndex) => {
                    const StepIcon = stepIndex === 0 ? LuMoveRight : LuCornerDownRight;

                    return (
                      <span key={stepIndex} className="inline-flex items-center">
                        <StepIcon className="mr-1 text-slate-400 dark:text-slate-500 h-3 w-3 flex-shrink-0" />
                        <span className="bg-slate-50 dark:bg-slate-800 px-2 py-0.5 rounded-md border border-slate-200/50 dark:border-slate-700/50">
                          {(Array.isArray(step.modifiers) && step.modifiers.length > 0) || (Array.isArray(step.keys) && step.keys.length > 0) ? (
                            <>
                              {Array.isArray(step.modifiers) && step.modifiers.map((modifier, idx) => (
                                <Fragment key={`mod-${idx}`}>
                                  <span className="font-medium text-slate-600 dark:text-slate-200">
                                    {modifierDisplayMap[modifier] || modifier}
                                  </span>
                                  {idx < step.modifiers.length - 1 && (
                                    <span className="text-slate-400 dark:text-slate-600"> + </span>
                                  )}
                                </Fragment>
                              ))}

                              {Array.isArray(step.modifiers) && step.modifiers.length > 0 && Array.isArray(step.keys) && step.keys.length > 0 && (
                                <span className="text-slate-400 dark:text-slate-600"> + </span>
                              )}

                              {Array.isArray(step.keys) && step.keys.map((key, idx) => (
                                <Fragment key={`key-${idx}`}>
                                  <span className="font-medium text-blue-600 dark:text-blue-200">
                                    {keyDisplayMap[key] || key}
                                  </span>
                                  {idx < step.keys.length - 1 && (
                                    <span className="text-slate-400 dark:text-slate-600"> + </span>
                                  )}
                                </Fragment>
                              ))}
                            </>
                          ) : (
                            <span className="font-medium text-slate-500 dark:text-slate-400">Delay only</span>
                          )}
                          <span className="ml-1 text-slate-400 dark:text-slate-500">({step.delay}ms)</span>
                        </span>
                      </span>
                    );
                  })}
                </span>
              </p>
            </div>

            <div className="flex items-center gap-1 ml-4">
              <Button
                size="XS"
                theme="light"
                LeadingIcon={LuCopy}
                onClick={() => handleDuplicateMacro(macro)}
                disabled={actionLoadingId === macro.id}
                aria-label={`Duplicate macro ${macro.name}`}
              />
              <Button
                size="XS"
                theme="light"
                LeadingIcon={LuPenLine}
                onClick={() => navigate(`${macro.id}/edit`)}
                disabled={actionLoadingId === macro.id}
                aria-label={`Edit macro ${macro.name}`}
              />
            </div>
          </div>
        </Card>
      ))}
    </div>
  ), [macros, actionLoadingId]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="Keyboard Macros"
        description="Create and manage keyboard macros for quick actions"
      />
      <div className="flex items-center justify-between mb-4">
        <SettingsItem 
          title="Macros"
          description={`${loading ? '?' : macros.length}/${MAX_TOTAL_MACROS}`}
        >
          <div className="flex items-center gap-2">
            <Button
              size="SM"
              theme="primary"
              text={isMaxMacrosReached ? `Max Reached` : "Add New Macro"}
              onClick={() => navigate("add")}
              disabled={isMaxMacrosReached}
              aria-label="Add new macro"
            />
          </div>
        </SettingsItem>
      </div>

      <div className="space-y-4">
        {loading && macros.length === 0 ? (
          <EmptyCard
            headline="Loading macros..."
            description="Please wait while we fetch your macros"
            BtnElm={
              <LuLoader className="h-6 w-6 animate-spin text-blue-500" />
            }
          />
        ) : macros.length === 0 ? (
          <EmptyCard
            headline="No macros created yet"
            description="Create keyboard macros to automate repetitive tasks"
            BtnElm={
              <Button
                size="SM"
                theme="primary"
                text="Add New Macro"
                onClick={() => navigate("add")}
                disabled={isMaxMacrosReached}
                aria-label="Add new macro"
              />
            }
          />
        ) : MacroList}
      </div>
    </div>
  );
}
