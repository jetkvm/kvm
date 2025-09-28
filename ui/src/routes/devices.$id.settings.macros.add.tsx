import { useNavigate } from "react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { KeySequence, useMacrosStore, generateMacroId } from "@/hooks/stores";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { MacroForm } from "@/components/MacroForm";
import { DEFAULT_DELAY } from "@/constants/macros";
import notifications from "@/notifications";

export default function SettingsMacrosAddRoute() {
  const { t } = useTranslation();
  const { macros, saveMacros } = useMacrosStore();
  const [isSaving, setIsSaving] = useState(false);
  const navigate = useNavigate();

  const normalizeSortOrders = (macros: KeySequence[]): KeySequence[] => {
    return macros.map((macro, index) => ({
      ...macro,
      sortOrder: index + 1,
    }));
  };

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
      notifications.success(t('Macro_name_created_successfully',{name:newMacro.name}));
      navigate("../");
    } catch (error: unknown) {
      if (error instanceof Error) {
        notifications.error(t('Failed_to_create_macro_msg',{msg:error.message}));
      } else {
        notifications.error(t('Failed_to_create_macro'));
      }
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('Add_New_Macro')}
        description={t('Create_a_new_keyboard_macro')}
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