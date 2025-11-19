import { Page } from "@playwright/test";

import { m } from "../localization/paraglide/messages.js";

type I18nKey = keyof typeof m;
type I18nOptions = Record<string, string>;

export const translate = (i18nKey: I18nKey, options?: I18nOptions) => {
  const func = m[i18nKey as keyof typeof m] as (options?: I18nOptions) => string
  const message = func(options);
  // older version uses "..." instead of "…" for the ellipsis
  // to avoid breaking the tests, we just remove the ellipsis
  if (message.includes("…")) {
    return message.replace("…", "");
  }
  return message;
};

export const getByText = (page: Page, i18nKey: I18nKey, options?: I18nOptions) => (
  page.getByText(translate(i18nKey, options))
);

export const getByRole = (page: Page, role: Parameters<typeof page.getByRole>[0], i18nKey: I18nKey) => (
  page.getByRole(role, { name: translate(i18nKey) })
);

export const sleep = (ms = 1000) => new Promise(resolve => setTimeout(resolve, ms));