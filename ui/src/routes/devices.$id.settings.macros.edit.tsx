import { useNavigate, useParams } from "react-router";
import { useState, useEffect } from "react";
import { LuTrash2 } from "react-icons/lu";
import { useTranslation } from "react-i18next";

import { KeySequence, useMacrosStore } from "@/hooks/stores";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { MacroForm } from "@/components/MacroForm";
import notifications from "@/notifications";
import { Button } from "@/components/Button";
import { ConfirmDialog } from "@/components/ConfirmDialog";

const normalizeSortOrders = (macros: KeySequence[]): KeySequence[] => {
  return macros.map((macro, index) => ({
    ...macro,
    sortOrder: index + 1,
  }));
};

export default function SettingsMacrosEditRoute() {
  const { t } = useTranslation();
  const { macros, saveMacros } = useMacrosStore();
  const [isUpdating, setIsUpdating] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const navigate = useNavigate();
  const { macroId } = useParams<{ macroId: string }>();
  const [macro, setMacro] = useState<KeySequence | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  useEffect(() => {
    const foundMacro = macros.find(m => m.id === macroId);
    if (foundMacro) {
      setMacro({
        ...foundMacro,
        steps: foundMacro.steps.map(step => ({
          ...step,
          keys: Array.isArray(step.keys) ? step.keys : [],
          modifiers: Array.isArray(step.modifiers) ? step.modifiers : [],
          delay: typeof step.delay === 'number' ? step.delay : 0
        }))
      });
    } else {
      navigate("../");
    }
  }, [macroId, macros, navigate]);

  const handleUpdateMacro = async (updatedMacro: Partial<KeySequence>) => {
    if (!macro) return;

    setIsUpdating(true);
    try {
      const newMacros = macros.map(m => 
        m.id === macro.id ? {
          ...macro,
          name: updatedMacro.name!.trim(),
          steps: updatedMacro.steps || [],
        } : m
      );

      await saveMacros(normalizeSortOrders(newMacros));
      notifications.success(t('Macro_name_updated_successfully',{name:updatedMacro.name}));
      navigate("../");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(t('Failed_to_update_macro_msg',{msg:error.message}));
      } else {
        notifications.error(t('Failed_to_update_macro'));
      }
    } finally {
      setIsUpdating(false);
    }
  };

  const handleDeleteMacro = async () => {
    if (!macro) return;

    setIsDeleting(true);
    try {
      const updatedMacros = normalizeSortOrders(macros.filter(m => m.id !== macro.id));
      await saveMacros(updatedMacros);
      notifications.success(t('Macro_name_deleted_successfully',{name:macro.name}));
      navigate("../macros");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(t('Failed_to_delete_macro_msg',{msg:error.message}));
      } else {
        notifications.error(t('Failed_to_delete_macro'));
      }
    } finally {
      setIsDeleting(false);
    }
  };

  if (!macro) return null;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <SettingsPageHeader
          title={t('Edit_Macro')}
          description={t('Modify_your_keyboard_macro')}
        />
        <Button
          size="SM"
          theme="light"
          text={t('Delete_Macro')}
          className="text-red-500 dark:text-red-400"
          LeadingIcon={LuTrash2}
          onClick={() => setShowDeleteConfirm(true)}
          disabled={isDeleting}
        />
      </div>
      <MacroForm
        initialData={macro}
        onSubmit={handleUpdateMacro}
        onCancel={() => navigate("../")}
        isSubmitting={isUpdating}
        submitText={t('Save_Changes')}
      />

      <ConfirmDialog
        open={showDeleteConfirm}
        onClose={() => setShowDeleteConfirm(false)}
        title={t('Delete_Macro')}
        description={t('Are_you_sure_you_want_to_delete_this_macro_This_action_cannot_be_undone')}
        variant="danger"
        confirmText={isDeleting ? t('Deleting') : t('Delete')}
        onConfirm={() => {
          handleDeleteMacro();
          setShowDeleteConfirm(false);
        }}
        isConfirming={isDeleting}
      />
    </div>
  );
} 