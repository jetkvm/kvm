import { useState } from "react";
import { useNavigate } from "react-router";

import { KeySequence, useMacrosStore, generateMacroId } from "@hooks/stores";
import { MacroForm } from "@components/MacroForm";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { DEFAULT_DELAY } from "@/constants/macros";
import notifications from "@/notifications";
import { normalizeSortOrders } from "@/utils";
import { m } from "@localizations/messages.js";

export default function SettingsMacrosAddRoute() {
  const { macros, saveMacros } = useMacrosStore();
  const [isSaving, setIsSaving] = useState(false);
  const navigate = useNavigate();

  const handleAddMacro = async (macro: Partial<KeySequence>) => {
    setIsSaving(true);
    try {
      const newMacro: KeySequence = {
        id: generateMacroId(),
        name: macro.name!.trim(),
        steps: macro.steps || [],
        sortOrder: macros.length + 1,
      };

      await saveMacros(normalizeSortOrders([...macros, newMacro]));
      notifications.success(m.macros_created_success({ name: newMacro.name }));
      navigate("../");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(m.macros_failed_create_error({ error: error.message || m.unknown_error() }));
      } else {
        notifications.error(m.macros_failed_create());
      }
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.macros_add_new()}
        description={m.macros_add_description()}
      />
      <MacroForm
        initialData={{
          name: "",
          steps: [{ keys: [], modifiers: [], delay: DEFAULT_DELAY }],
        }}
        onSubmit={handleAddMacro}
        onCancel={() => navigate("../")}
        isSubmitting={isSaving}
      />
    </div>
  );
}