import { Page } from "@playwright/test";

import { m } from "../localization/paraglide/messages.js";

type I18nKey = keyof typeof m;
type I18nOptions = Record<string, string>;

export const translate = (i18nKey: I18nKey, options?: I18nOptions) => (
  m[i18nKey as keyof typeof m] as (options?: I18nOptions) => string
)(options);

export const getByText = (page: Page, i18nKey: I18nKey, options?: I18nOptions) => (
  page.getByText(translate(i18nKey, options))
);

export const getByRole = (page: Page, role: Parameters<typeof page.getByRole>[0], i18nKey: I18nKey) => (
  page.getByRole(role, { name: translate(i18nKey) })
);

