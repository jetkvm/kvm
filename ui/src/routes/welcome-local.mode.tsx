import { useState } from "react";
import { Form, redirect, useActionData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction } from "react-router";

import { cx } from "@/cva.config";
import LogoBlueIcon from "@assets/logo-blue.png";
import LogoWhiteIcon from "@assets/logo-white.svg";
import { Button } from "@components/Button";
import Container from "@components/Container";
import GridBackground from "@components/GridBackground";
import { GridCard } from "@components/Card";
import { DEVICE_API } from "@/ui.config";
import api from "@/api";
import { m } from "@localizations/messages.js";

import { DeviceStatus } from "./welcome-local";

const loader: LoaderFunction = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (res.isSetup) return redirect("/login-local");
  return null;
};

const action: ActionFunction = async ({ request }: ActionFunctionArgs) => {
  const formData = await request.formData();
  const localAuthMode = formData.get("localAuthMode");
  if (!localAuthMode) return { error: m.auth_authentication_mode() };

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
      return { error: m.auth_authentication_mode_error() };
    }
  }

  return { error: m.auth_authentication_mode_invalid() };
};

export default function WelcomeLocalModeRoute() {
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
                  {m.auth_mode_local()}
                </h1>
                <p className="font-medium text-slate-600 dark:text-slate-400">
                  {m.auth_mode_local_description()}
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
                        role="switch"
                        aria-checked={selectedMode === "password"}
                      >
                        <div className="space-y-0 text-center">
                          <h3 className="text-base font-bold text-black dark:text-white">
                            {mode === "password" ? m.auth_mode_local_password() : m.auth_mode_local_no_password()}
                          </h3>
                          <p className="mt-2 text-center text-sm text-gray-600 dark:text-gray-400">
                            {mode === "password"
                              ? m.auth_mode_local_password_description()
                              : m.auth_mode_local_no_password_description()}
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
                    text={m.continue()}
                    textAlign="center"
                    disabled={!selectedMode}
                  />
                </div>
              </Form>

              <p
                className="animate-fadeIn mx-auto max-w-md text-center text-xs text-slate-500 opacity-0 dark:text-slate-400"
                style={{ animationDelay: "600ms" }}
              >
                {m.auth_mode_local_change_later()}
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
