import { Form, redirect, useActionData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction } from "react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.png";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import { DEVICE_API } from "@/ui.config";

import { GridCard } from "../components/Card";
import { cx } from "../cva.config";
import api from "../api";

import { DeviceStatus } from "./welcome-local";

const loader: LoaderFunction = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (res.isSetup) return redirect("/login-local");
  return null;
};

const action: ActionFunction = async ({ request }: ActionFunctionArgs) => {
  const { t } = useTranslation();
  const formData = await request.formData();
  const localAuthMode = formData.get("localAuthMode");
  if (!localAuthMode) return { error: t('Please_select_an_authentication_mode') };

  if (localAuthMode === "password") {
    return redirect("/welcome/password");
  }

  if (localAuthMode === "noPassword") {
    try {
      await api.POST(`${DEVICE_API}/device/setup`, {
        localAuthMode,
      });
      return redirect("/");
    } catch (error) {
      console.error("Error setting authentication mode:", error);
      return { error: t('An_error_occurred_while_setting_the_authentication_mode') };
    }
  }

  return { error: t('Invalid_authentication_mode') };
};

export default function WelcomeLocalModeRoute() {
  const { t } = useTranslation();
  const actionData = useActionData() as { error?: string };
  const [selectedMode, setSelectedMode] = useState<"password" | "noPassword" | null>(
    null,
  );

  return (
    <>
      <GridBackground />
      <div className="grid min-h-screen">
        <Container>
          <div className="isolate flex h-full w-full items-center justify-center">
            <div className="max-w-xl space-y-8">
              <div className="animate-fadeIn flex items-center justify-center opacity-0">
                <img
                  src={LogoWhiteIcon}
                  alt=""
                  className="-ml-4 hidden h-[32px] dark:block"
                />
                <img src={LogoBlueIcon} alt="" className="-ml-4 h-[32px] dark:hidden" />
              </div>

              <div
                className="animate-fadeIn space-y-2 text-center opacity-0"
                style={{ animationDelay: "200ms" }}
              >
                <h1 className="text-4xl font-semibold text-black dark:text-white">
                    {t('Local_Authentication_Method')}
                </h1>
                <p className="font-medium text-slate-600 dark:text-slate-400">
                    {t('Select_how_you_d_like_to_secure_your_JetKVM_device_locally')}
                </p>
              </div>

              <Form method="POST" className="space-y-8">
                <div
                  className="animate-fadeIn grid grid-cols-1 gap-6 opacity-0 sm:grid-cols-2"
                  style={{ animationDelay: "400ms" }}
                >
                  {["password", "noPassword"].map(mode => (
                    <GridCard
                      key={mode}
                      cardClassName={cx("transition-all duration-100", {
                        "outline-blue-700! outline-2!": selectedMode === mode,
                        "hover:outline-blue-700!": selectedMode !== mode,
                      })}
                    >
                      <div
                        className="relative flex cursor-pointer flex-col items-center p-6 select-none"
                        onClick={() => setSelectedMode(mode as "password" | "noPassword")}
                      >
                        <div className="space-y-0 text-center">
                          <h3 className="text-base font-bold text-black dark:text-white">
                            {mode === "password" ? t('Password_protected') : t('No_password')}
                          </h3>
                          <p className="mt-2 text-center text-sm text-gray-600 dark:text-gray-400">
                            {mode === "password"
                              ? t('Secure_your_device_with_a_password_for_added_protection')
                              : t('Quick_access_without_password_authentication')}
                          </p>
                        </div>
                        <input
                          type="radio"
                          name="localAuthMode"
                          value={mode}
                          checked={selectedMode === mode}
                          onChange={() => {
                            setSelectedMode(mode as "password" | "noPassword");
                          }}
                          className="form-radio absolute top-2 right-2 h-4 w-4 text-blue-600"
                        />
                      </div>
                    </GridCard>
                  ))}
                </div>

                {actionData?.error && (
                  <p
                    className="animate-fadeIn text-center text-sm text-red-600 opacity-0 dark:text-red-400"
                    style={{ animationDelay: "500ms" }}
                  >
                    {actionData.error}
                  </p>
                )}

                <div
                  className="animate-fadeIn mx-auto max-w-sm opacity-0"
                  style={{ animationDelay: "500ms" }}
                >
                  <Button
                    size="LG"
                    theme="primary"
                    fullWidth
                    type="submit"
                    text={t('Continue')}
                    textAlign="center"
                    disabled={!selectedMode}
                  />
                </div>
              </Form>

              <p
                className="animate-fadeIn mx-auto max-w-md text-center text-xs text-slate-500 opacity-0 dark:text-slate-400"
                style={{ animationDelay: "600ms" }}
              >
                  {t('You_can_always_change_your_authentication_method_later_in_the_settings')}
              </p>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}

WelcomeLocalModeRoute.loader = loader;
WelcomeLocalModeRoute.action = action;
