import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";

import { SettingsPageHeader } from "../components/SettingsPageheader";
import { SelectMenuBasic } from "../components/SelectMenuBasic";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsAppearanceRoute() {
  const { t } = useTranslation();
  const [currentTheme, setCurrentTheme] = useState(() => {
    return localStorage.theme || "system";
  });

  const handleThemeChange = useCallback((value: string) => {
    const root = document.documentElement;

    if (value === "system") {
      localStorage.removeItem("theme");
      // Check system preference
      const systemTheme = window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
      root.classList.remove("light", "dark");
      root.classList.add(systemTheme);
    } else {
      localStorage.theme = value;
      root.classList.remove("light", "dark");
      root.classList.add(value);
    }
  }, []);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={t('Appearance')}
        description={t('Customize_the_look_and_feel_of_your_JetKVM_interface')}
      />
      <SettingsItem title={t('Theme')} description={t('Choose_your_preferred_color_theme')}>
        <SelectMenuBasic
          size="SM"
          label=""
          value={currentTheme}
          options={[
            { value: "system", label: t('System_default') },
            { value: "light", label: t('Light') },
            { value: "dark", label: t('Dark') },
          ]}
          onChange={e => {
            setCurrentTheme(e.target.value);
            handleThemeChange(e.target.value);
          }}
        />
      </SettingsItem>
    </div>
  );
}
