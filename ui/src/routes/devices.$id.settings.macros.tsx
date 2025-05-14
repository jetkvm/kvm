import { useEffect, Fragment, useMemo, useState, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import {
  LuPenLine,
  LuCopy,
  LuMoveRight,
  LuCornerDownRight,
  LuArrowUp,
  LuArrowDown,
  LuTrash2,
  LuCommand,
} from "react-icons/lu";

import { KeySequence, useMacrosStore, generateMacroId } from "@/hooks/stores";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { Button } from "@/components/Button";
import EmptyCard from "@/components/EmptyCard";
import Card from "@/components/Card";
import { MAX_TOTAL_MACROS, COPY_SUFFIX, DEFAULT_DELAY } from "@/constants/macros";
import { keyDisplayMap, modifierDisplayMap } from "@/keyboardMappings";
import notifications from "@/notifications";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import LoadingSpinner from "@/components/LoadingSpinner";

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
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [macroToDelete, setMacroToDelete] = useState<KeySequence | null>(null);

  const isMaxMacrosReached = useMemo(
    () => macros.length >= MAX_TOTAL_MACROS,
    [macros.length],
  );

  useEffect(() => {
    if (!initialized) {
      loadMacros();
    }
  }, [initialized, loadMacros]);

  const handleDuplicateMacro = useCallback(
    async (macro: KeySequence) => {
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
    },
    [isMaxMacrosReached, macros, saveMacros, setActionLoadingId],
  );

  const handleMoveMacro = useCallback(
    async (index: number, direction: "up" | "down", macroId: string) => {
      if (!Array.isArray(macros) || macros.length === 0) {
        notifications.error("No macros available");
        return;
      }

      const newIndex = direction === "up" ? index - 1 : index + 1;
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
    },
    [macros, saveMacros, setActionLoadingId],
  );

  const handleDeleteMacro = useCallback(async () => {
    if (!macroToDelete?.id) return;

    setActionLoadingId(macroToDelete.id);
    try {
      const updatedMacros = normalizeSortOrders(
        macros.filter(m => m.id !== macroToDelete.id),
      );
      await saveMacros(updatedMacros);
      notifications.success(`Macro "${macroToDelete.name}" deleted successfully`);
      setShowDeleteConfirm(false);
      setMacroToDelete(null);
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(`Failed to delete macro: ${error.message}`);
      } else {
        notifications.error("Failed to delete macro");
      }
    } finally {
      setActionLoadingId(null);
    }
  }, [macroToDelete, macros, saveMacros]);

  const MacroList = useMemo(
    () => (
      <div className="space-y-2">
        {macros.map((macro, index) => (
          <Card key={macro.id} className="bg-white p-2 dark:bg-slate-800">
            <div className="flex items-center justify-between">
              <div className="flex flex-col gap-1 px-2">
                <Button
                  size="XS"
                  theme="light"
                  onClick={() => handleMoveMacro(index, "up", macro.id)}
                  disabled={index === 0 || actionLoadingId === macro.id}
                  LeadingIcon={LuArrowUp}
                  aria-label={`Move ${macro.name} up`}
                />
                <Button
                  size="XS"
                  theme="light"
                  onClick={() => handleMoveMacro(index, "down", macro.id)}
                  disabled={index === macros.length - 1 || actionLoadingId === macro.id}
                  LeadingIcon={LuArrowDown}
                  aria-label={`Move ${macro.name} down`}
                />
              </div>

              <div className="ml-2 flex min-w-0 flex-1 flex-col justify-center">
                <h3 className="truncate text-sm font-semibold text-black dark:text-white">
                  {macro.name}
                </h3>
                <p className="ml-4 mt-1 overflow-hidden text-xs text-slate-500 dark:text-slate-400">
                  <span className="flex flex-col items-start gap-1">
                    {macro.steps.map((step, stepIndex) => {
                      const StepIcon = stepIndex === 0 ? LuMoveRight : LuCornerDownRight;

                      return (
                        <span key={stepIndex} className="inline-flex items-center">
                          <StepIcon className="mr-1 h-3 w-3 shrink-0 text-slate-400 dark:text-slate-500" />
                          <span className="px-2 py-0.5 rounded-md border border-slate-200/50 dark:border-slate-700/50 bg-slate-50 dark:bg-slate-800">
                            {(Array.isArray(step.modifiers) &&
                              step.modifiers.length > 0) ||
                            (Array.isArray(step.keys) && step.keys.length > 0) ? (
                              <>
                                {Array.isArray(step.modifiers) &&
                                  step.modifiers.map((modifier, idx) => (
                                    <Fragment key={`mod-${idx}`}>
                                      <span className="font-medium text-slate-600 dark:text-slate-200">
                                        {modifierDisplayMap[modifier] || modifier}
                                      </span>
                                      {idx < step.modifiers.length - 1 && (
                                        <span className="text-slate-400 dark:text-slate-600">
                                          {" "}
                                          +{" "}
                                        </span>
                                      )}
                                    </Fragment>
                                  ))}

                                {Array.isArray(step.modifiers) &&
                                  step.modifiers.length > 0 &&
                                  Array.isArray(step.keys) &&
                                  step.keys.length > 0 && (
                                    <span className="text-slate-400 dark:text-slate-600">
                                      {" "}
                                      +{" "}
                                    </span>
                                  )}

                                {Array.isArray(step.keys) &&
                                  step.keys.map((key, idx) => (
                                    <Fragment key={`key-${idx}`}>
                                      <span className="font-medium text-blue-600 dark:text-blue-400">
                                        {keyDisplayMap[key] || key}
                                      </span>
                                      {idx < step.keys.length - 1 && (
                                        <span className="text-slate-400 dark:text-slate-600">
                                          {" "}
                                          +{" "}
                                        </span>
                                      )}
                                    </Fragment>
                                  ))}
                              </>
                            ) : (
                              <span className="font-medium text-slate-500 dark:text-slate-400">
                                Delay only
                              </span>
                            )}
                            {step.delay !== DEFAULT_DELAY && (
                              <span className="ml-1 text-slate-400 dark:text-slate-500">
                                ({step.delay}ms)
                              </span>
                            )}
                          </span>
                        </span>
                      );
                    })}
                  </span>
                </p>
              </div>

              <div className="ml-4 flex items-center gap-1">
                <Button
                  size="XS"
                  className="text-red-500 dark:text-red-400"
                  theme="light"
                  LeadingIcon={LuTrash2}
                  onClick={() => {
                    setMacroToDelete(macro);
                    setShowDeleteConfirm(true);
                  }}
                  disabled={actionLoadingId === macro.id}
                  aria-label={`Delete macro ${macro.name}`}
                />
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
                  text="Edit"
                  onClick={() => navigate(`${macro.id}/edit`)}
                  disabled={actionLoadingId === macro.id}
                  aria-label={`Edit macro ${macro.name}`}
                />
              </div>
            </div>
          </Card>
        ))}

        <ConfirmDialog
          open={showDeleteConfirm}
          onClose={() => {
            setShowDeleteConfirm(false);
            setMacroToDelete(null);
          }}
          title="Delete Macro"
          description={`Are you sure you want to delete "${macroToDelete?.name}"? This action cannot be undone.`}
          variant="danger"
          confirmText={actionLoadingId === macroToDelete?.id ? "Deleting..." : "Delete"}
          onConfirm={handleDeleteMacro}
          isConfirming={actionLoadingId === macroToDelete?.id}
        />
      </div>
    ),
    [
      macros,
      showDeleteConfirm,
      macroToDelete?.name,
      macroToDelete?.id,
      actionLoadingId,
      handleDeleteMacro,
      handleMoveMacro,
      handleDuplicateMacro,
      navigate,
    ],
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SettingsPageHeader
          title="Keyboard Macros"
          description={`Combine keystrokes into a single action for faster workflows.`}
        />
        {macros.length > 0 && (
          <div className="flex items-center pl-2">
            <Button
              size="SM"
              theme="primary"
              text={isMaxMacrosReached ? `Max Reached` : "Add New Macro"}
              onClick={() => navigate("add")}
              disabled={isMaxMacrosReached}
              aria-label="Add new macro"
            />
          </div>
        )}
      </div>

      <div className="space-y-4">
        {loading && macros.length === 0 ? (
          <EmptyCard
            IconElm={LuCommand}
            headline="Loading macros..."
            BtnElm={
              <div className="my-2 flex flex-col items-center space-y-2 text-center">
                <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
              </div>
            }
          />
        ) : macros.length === 0 ? (
          <EmptyCard
            IconElm={LuCommand}
            headline="Create Your First Macro"
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
        ) : (
          MacroList
        )}
      </div>
    </div>
  );
}
