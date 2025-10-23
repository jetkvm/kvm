import { useCallback, useState } from "react";

import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { m } from "@localizations/messages.js";

export default function SettingsAppearanceRoute() {
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

  const themeOptions = [
    { value: "system", label: m.appearance_theme_system() },
    { value: "light", label: m.appearance_theme_light() },
    { value: "dark", label: m.appearance_theme_dark() },
  ];

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={m.appearance_title()}
        description={m.appearance_page_description()}
      />
      <SettingsItem title={m.appearance_theme()} description={m.appearance_description()}>
        <SelectMenuBasic
          size="SM"
          label=""
          value={currentTheme}
          options={themeOptions}
          onChange={e => {
            setCurrentTheme(e.target.value);
            handleThemeChange(e.target.value);
          }}
        />
      </SettingsItem>
    </div>
  );
}
