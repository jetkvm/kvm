import { useEffect, Fragment, useMemo, useState, useCallback } from "react";
import { useNavigate } from "react-router";
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
import notifications from "@/notifications";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import LoadingSpinner from "@/components/LoadingSpinner";
import useKeyboardLayout from "@/hooks/useKeyboardLayout";
import {useTranslation} from "react-i18next";

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
  const { selectedKeyboard }  = useKeyboardLayout();
  const { t } = useTranslation();
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
        notifications.error(t('Invalid_macro_data'));
        return;
      }

      if (isMaxMacrosReached) {
        notifications.error(t('Maximum_of_max_macros_allowed',{max:MAX_TOTAL_MACROS}));
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
        notifications.success(t('Macro_name_duplicated_successfully',{name:newMacroCopy.name}));
      } catch (error: unknown) {
        if (error instanceof Error) {
          notifications.error(t('Failed_to_duplicate_macro_msg',{msg:error.message}));
        } else {
          notifications.error(t('Failed_to_duplicate_macro'));
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
        notifications.error(t('No_macros_available'));
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
        notifications.success(t('Macro_order_updated_successfully'));
      } catch (error: unknown) {
        if (error instanceof Error) {
          notifications.error(t('Failed_to_reorder_macros_msg',{msg:error.message}));
        } else {
          notifications.error(t('Failed_to_reorder_macros'));
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
      notifications.success(t('Macro_name_deleted_successfully',{name:macroToDelete.name}));
      setShowDeleteConfirm(false);
      setMacroToDelete(null);
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(t('Failed_to_delete_macro_msg',{msg:error.message}));
      } else {
        notifications.error(t('Failed_to_delete_macro'));
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
                  aria-label={t('Move_name_up',{name:macro.name})}
                />
                <Button
                  size="XS"
                  theme="light"
                  onClick={() => handleMoveMacro(index, "down", macro.id)}
                  disabled={index === macros.length - 1 || actionLoadingId === macro.id}
                  LeadingIcon={LuArrowDown}
                  aria-label={t('Move_name_down',{name:macro.name})}
                />
              </div>

              <div className="ml-2 flex min-w-0 flex-1 flex-col justify-center">
                <h3 className="truncate text-sm font-semibold text-black dark:text-white">
                  {macro.name}
                </h3>
                <p className="mt-1 ml-4 overflow-hidden text-xs text-slate-500 dark:text-slate-400">
                  <span className="flex flex-col items-start gap-1">
                    {macro.steps.map((step, stepIndex) => {
                      const StepIcon = stepIndex === 0 ? LuMoveRight : LuCornerDownRight;

                      return (
                        <span key={stepIndex} className="inline-flex items-center">
                          <StepIcon className="mr-1 h-3 w-3 shrink-0 text-slate-400 dark:text-slate-500" />
                          <span className="rounded-md border border-slate-200/50 bg-slate-50 px-2 py-0.5 dark:border-slate-700/50 dark:bg-slate-800">
                            {(Array.isArray(step.modifiers) &&
                              step.modifiers.length > 0) ||
                            (Array.isArray(step.keys) && step.keys.length > 0) ? (
                              <>
                                {Array.isArray(step.modifiers) &&
                                  step.modifiers.map((modifier, idx) => (
                                    <Fragment key={`mod-${idx}`}>
                                      <span className="font-medium text-slate-600 dark:text-slate-200">
                                        {selectedKeyboard.modifierDisplayMap[modifier] || modifier}
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
                                        {selectedKeyboard.keyDisplayMap[key] || key}
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
                  aria-label={t('Delete_macro_name',{name:macro.name})}
                />
                <Button
                  size="XS"
                  theme="light"
                  LeadingIcon={LuCopy}
                  onClick={() => handleDuplicateMacro(macro)}
                  disabled={actionLoadingId === macro.id}
                  aria-label={t('Duplicate_macro_name',{name:macro.name})}
                />
                <Button
                  size="XS"
                  theme="light"
                  LeadingIcon={LuPenLine}
                  text={t('Edit')}
                  onClick={() => navigate(`${macro.id}/edit`)}
                  disabled={actionLoadingId === macro.id}
                  aria-label={t('Edit_macro_name',{name:macro.name})}
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
          title={t('Delete_Macro')}
          description={t('Are_you_sure_you_want_to_delete_name_This_action_cannot_be_undone',{name:macroToDelete?.name})}
          variant="danger"
          confirmText={actionLoadingId === macroToDelete?.id ? t('Deleting...') : t('Delete')}
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
      selectedKeyboard.modifierDisplayMap,
      selectedKeyboard.keyDisplayMap,
      handleDuplicateMacro,
      navigate
    ],
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SettingsPageHeader
          title={t('Keyboard_Macros')}
          description={t('Combine_keystrokes_into_a_single_action_for_faster_workflows')}
        />
        {macros.length > 0 && (
          <div className="flex items-center pl-2">
            <Button
              size="SM"
              theme="primary"
              text={isMaxMacrosReached ? t('Max_Reached') : t('Add_New_Macro')}
              onClick={() => navigate("add")}
              disabled={isMaxMacrosReached}
              aria-label={t('Add_New_Macro')}
            />
          </div>
        )}
      </div>

      <div className="space-y-4">
        {loading && macros.length === 0 ? (
          <EmptyCard
            IconElm={LuCommand}
            headline={t("Loading macros...")}
            BtnElm={
              <div className="my-2 flex flex-col items-center space-y-2 text-center">
                <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
              </div>
            }
          />
        ) : macros.length === 0 ? (
          <EmptyCard
            IconElm={LuCommand}
            headline={t('Create_Your_First_Macro')}
            description={t('Combine_keystrokes_into_a_single_action')}
            BtnElm={
              <Button
                size="SM"
                theme="primary"
                text={t('Add_New_Macro')}
                onClick={() => navigate("add")}
                disabled={isMaxMacrosReached}
                aria-label={t('Add_New_Macro')}
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
