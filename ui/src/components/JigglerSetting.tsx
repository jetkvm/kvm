import { useState } from "react";
import { LuExternalLink } from "react-icons/lu";

import { Button, LinkButton } from "@components/Button";

import { InputFieldWithLabel } from "./InputField";
import { SelectMenuBasic } from "./SelectMenuBasic";

export interface JigglerConfig {
  inactivity_limit_seconds: number;
  jitter_percentage: number;
  schedule_cron_tab: string;
  timezone?: string;
}

export function JigglerSetting({
  onSave,
  defaultJigglerState,
}: {
  onSave: (jigglerConfig: JigglerConfig) => void;
  defaultJigglerState?: JigglerConfig;
}) {
  const [jigglerConfigState, setJigglerConfigState] = useState<JigglerConfig>(
    defaultJigglerState || {
      inactivity_limit_seconds: 20,
      jitter_percentage: 0,
      schedule_cron_tab: "*/20 * * * * *",
      timezone: "UTC",
    }
  );

  const timezoneOptions = [
    { value: "UTC", label: "UTC" },

    // Americas - North America
    { value: "America/New_York", label: "Eastern Time (US/Canada)" },
    { value: "America/Chicago", label: "Central Time (US/Canada)" },
    { value: "America/Denver", label: "Mountain Time (US/Canada)" },
    { value: "America/Los_Angeles", label: "Pacific Time (US/Canada)" },
    { value: "America/Anchorage", label: "Alaska Time" },
    { value: "Pacific/Honolulu", label: "Hawaii Time" },
    { value: "America/Phoenix", label: "Arizona (No DST)" },
    { value: "America/Toronto", label: "Toronto" },
    { value: "America/Vancouver", label: "Vancouver" },
    { value: "America/Montreal", label: "Montreal" },
    { value: "America/Halifax", label: "Halifax (Atlantic)" },
    { value: "America/St_Johns", label: "Newfoundland" },

    // Americas - Central & South America
    { value: "America/Mexico_City", label: "Mexico City" },
    { value: "America/Cancun", label: "Cancun" },
    { value: "America/Guatemala", label: "Guatemala" },
    { value: "America/Costa_Rica", label: "Costa Rica" },
    { value: "America/Panama", label: "Panama" },
    { value: "America/Bogota", label: "Bogotá" },
    { value: "America/Lima", label: "Lima" },
    { value: "America/Santiago", label: "Santiago" },
    { value: "America/Buenos_Aires", label: "Buenos Aires" },
    { value: "America/Montevideo", label: "Montevideo" },
    { value: "America/Sao_Paulo", label: "São Paulo" },
    { value: "America/Rio_Branco", label: "Rio de Janeiro" },
    { value: "America/Caracas", label: "Caracas" },
    { value: "America/La_Paz", label: "La Paz" },

    // Europe - Western Europe
    { value: "Europe/London", label: "London (GMT/BST)" },
    { value: "Europe/Dublin", label: "Dublin (GMT/IST)" },
    { value: "Europe/Lisbon", label: "Lisbon (WET/WEST)" },
    { value: "Atlantic/Reykjavik", label: "Reykjavik (GMT)" },

    // Europe - Central Europe
    { value: "Europe/Paris", label: "Paris (CET/CEST)" },
    { value: "Europe/Berlin", label: "Berlin (CET/CEST)" },
    { value: "Europe/Rome", label: "Rome (CET/CEST)" },
    { value: "Europe/Madrid", label: "Madrid (CET/CEST)" },
    { value: "Europe/Amsterdam", label: "Amsterdam (CET/CEST)" },
    { value: "Europe/Brussels", label: "Brussels (CET/CEST)" },
    { value: "Europe/Vienna", label: "Vienna (CET/CEST)" },
    { value: "Europe/Zurich", label: "Zurich (CET/CEST)" },
    { value: "Europe/Prague", label: "Prague (CET/CEST)" },
    { value: "Europe/Warsaw", label: "Warsaw (CET/CEST)" },
    { value: "Europe/Budapest", label: "Budapest (CET/CEST)" },
    { value: "Europe/Stockholm", label: "Stockholm (CET/CEST)" },
    { value: "Europe/Copenhagen", label: "Copenhagen (CET/CEST)" },
    { value: "Europe/Oslo", label: "Oslo (CET/CEST)" },
    { value: "Europe/Helsinki", label: "Helsinki (EET/EEST)" },

    // Europe - Eastern Europe
    { value: "Europe/Athens", label: "Athens (EET/EEST)" },
    { value: "Europe/Istanbul", label: "Istanbul (TRT)" },
    { value: "Europe/Bucharest", label: "Bucharest (EET/EEST)" },
    { value: "Europe/Sofia", label: "Sofia (EET/EEST)" },
    { value: "Europe/Kiev", label: "Kiev (EET/EEST)" },
    { value: "Europe/Moscow", label: "Moscow (MSK)" },
    { value: "Europe/Minsk", label: "Minsk (MSK)" },

    // Asia - East Asia
    { value: "Asia/Tokyo", label: "Tokyo (JST)" },
    { value: "Asia/Seoul", label: "Seoul (KST)" },
    { value: "Asia/Shanghai", label: "Shanghai (CST)" },
    { value: "Asia/Hong_Kong", label: "Hong Kong (HKT)" },
    { value: "Asia/Taipei", label: "Taipei (CST)" },
    { value: "Asia/Manila", label: "Manila (PST)" },

    // Asia - Southeast Asia
    { value: "Asia/Singapore", label: "Singapore (SGT)" },
    { value: "Asia/Bangkok", label: "Bangkok (ICT)" },
    { value: "Asia/Ho_Chi_Minh", label: "Ho Chi Minh City (ICT)" },
    { value: "Asia/Jakarta", label: "Jakarta (WIB)" },
    { value: "Asia/Kuala_Lumpur", label: "Kuala Lumpur (MYT)" },

    // Asia - South Asia
    { value: "Asia/Kolkata", label: "India (IST)" },
    { value: "Asia/Dhaka", label: "Dhaka (BST)" },
    { value: "Asia/Karachi", label: "Karachi (PKT)" },
    { value: "Asia/Colombo", label: "Colombo (IST)" },
    { value: "Asia/Kathmandu", label: "Kathmandu (NPT)" },

    // Asia - Central Asia
    { value: "Asia/Tashkent", label: "Tashkent (UZT)" },
    { value: "Asia/Almaty", label: "Almaty (ALMT)" },
    { value: "Asia/Yekaterinburg", label: "Yekaterinburg (YEKT)" },
    { value: "Asia/Novosibirsk", label: "Novosibirsk (NOVT)" },

    // Asia - Middle East
    { value: "Asia/Dubai", label: "Dubai (GST)" },
    { value: "Asia/Qatar", label: "Doha (AST)" },
    { value: "Asia/Kuwait", label: "Kuwait (AST)" },
    { value: "Asia/Riyadh", label: "Riyadh (AST)" },
    { value: "Asia/Baghdad", label: "Baghdad (AST)" },
    { value: "Asia/Tehran", label: "Tehran (IRST)" },
    { value: "Asia/Jerusalem", label: "Jerusalem (IST)" },
    { value: "Asia/Beirut", label: "Beirut (EET)" },

    // Africa
    { value: "Africa/Cairo", label: "Cairo (EET)" },
    { value: "Africa/Lagos", label: "Lagos (WAT)" },
    { value: "Africa/Nairobi", label: "Nairobi (EAT)" },
    { value: "Africa/Johannesburg", label: "Johannesburg (SAST)" },
    { value: "Africa/Cape_Town", label: "Cape Town (SAST)" },
    { value: "Africa/Casablanca", label: "Casablanca (WET)" },
    { value: "Africa/Tunis", label: "Tunis (CET)" },
    { value: "Africa/Algiers", label: "Algiers (CET)" },
    { value: "Africa/Addis_Ababa", label: "Addis Ababa (EAT)" },

    // Australia/Oceania
    { value: "Australia/Sydney", label: "Sydney (AEDT/AEST)" },
    { value: "Australia/Melbourne", label: "Melbourne (AEDT/AEST)" },
    { value: "Australia/Brisbane", label: "Brisbane (AEST)" },
    { value: "Australia/Perth", label: "Perth (AWST)" },
    { value: "Australia/Adelaide", label: "Adelaide (ACDT/ACST)" },
    { value: "Australia/Darwin", label: "Darwin (ACST)" },
    { value: "Pacific/Auckland", label: "Auckland (NZDT/NZST)" },
    { value: "Pacific/Fiji", label: "Fiji (FJT)" },
    { value: "Pacific/Guam", label: "Guam (ChST)" },
    { value: "Pacific/Tahiti", label: "Tahiti (TAHT)" },
  ];

  const exampleConfigs = [
    {
      name: "Business Hours 9-17",
      config: {
        inactivity_limit_seconds: 60,
        jitter_percentage: 25,
        schedule_cron_tab: "0 * 9-17 * * 1-5",
        timezone: jigglerConfigState.timezone || "UTC",
      },
    },
    {
      name: "Business Hours 8-17",
      config: {
        inactivity_limit_seconds: 60,
        jitter_percentage: 25,
        schedule_cron_tab: "0 * 8-17 * * 1-5",
        timezone: jigglerConfigState.timezone || "UTC",
      },
    },
  ];

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <h4 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
          Examples
        </h4>
        <div className="flex gap-2 flex-wrap">
          {exampleConfigs.map((example, index) => (
            <Button
              key={index}
              size="XS"
              theme="light"
              text={example.name}
              onClick={() => setJigglerConfigState(example.config)}
            />
          ))}
          <LinkButton
            to="https://crontab.guru/examples.html"
            size="XS"
            theme="light"
            text="More examples"
            LeadingIcon={LuExternalLink}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 items-end gap-4">
        <InputFieldWithLabel
          required
          size="SM"
          label="Cron Schedule"
          description="Cron expression for scheduling"
          placeholder="*/20 * * * * *"
          value={jigglerConfigState.schedule_cron_tab}
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              schedule_cron_tab: e.target.value,
            })
          }
        />

        <InputFieldWithLabel
          size="SM"
          label="Inactivity Limit Seconds"
          description="Inactivity time before jiggle"
          value={jigglerConfigState.inactivity_limit_seconds}
          type="number"
          min="1"
          max="100"
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              inactivity_limit_seconds: Number(e.target.value),
            })
          }
        />

        <InputFieldWithLabel
          required
          size="SM"
          label="Random delay"
          description="To avoid recognizable patterns"
          placeholder="25"
          TrailingElm={<span className="px-2 text-xs text-slate-500">%</span>}
          value={jigglerConfigState.jitter_percentage}
          type="number"
          min="0"
          max="100"
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              jitter_percentage: Number(e.target.value),
            })
          }
        />

        <SelectMenuBasic
          size="SM"
          label="Timezone"
          description="Timezone for cron schedule"
          value={jigglerConfigState.timezone || "UTC"}
          onChange={e =>
            setJigglerConfigState({
              ...jigglerConfigState,
              timezone: e.target.value,
            })
          }
          options={timezoneOptions}
        />
      </div>

      <div className="flex gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text="Save Jiggler Config"
          onClick={() => onSave(jigglerConfigState)}
        />
      </div>
    </div>
  );
}
