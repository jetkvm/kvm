import { useState, useEffect } from "react";
import { useNavigate, useParams } from "react-router";
import { LuTrash2 } from "react-icons/lu";

import { KeySequence, useMacrosStore } from "@hooks/stores";
import { Button } from "@components/Button";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { MacroForm } from "@components/MacroForm";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import notifications from "@/notifications";
import { normalizeSortOrders } from "@/utils";
import { m } from "@localizations/messages.js";

export default function SettingsMacrosEditRoute() {
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
      notifications.success(m.macros_updated_success({ name: updatedMacro.name || "" }));
      navigate("../");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(m.macros_failed_update_error({ error: error.message || m.unknown_error() }));
      } else {
        notifications.error(m.macros_failed_update());
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
      notifications.success(m.macros_deleted_success({ name: macro.name }));
      navigate("../macros");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(m.macros_failed_delete_error({ error: error.message }));
      } else {
        notifications.error(m.macros_failed_delete());
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
          title={m.macros_edit_title()}
          description={m.macros_edit_description()}
        />
        <Button
          size="SM"
          theme="light"
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
      />

      <ConfirmDialog
        open={showDeleteConfirm}
        onClose={() => setShowDeleteConfirm(false)}
        title={m.macros_delete_macro()}
        description={m.macros_delete_confirm()}
        variant="danger"
        confirmText={isDeleting ? m.macros_deleting() : m.delete()}
        onConfirm={() => {
          handleDeleteMacro();
          setShowDeleteConfirm(false);
        }}
        isConfirming={isDeleting}
      />
    </div>
  );
}