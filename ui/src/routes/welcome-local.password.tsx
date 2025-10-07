import { useState, useRef, useEffect } from "react";
import { Form, redirect, useActionData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction } from "react-router";
import { LuEye, LuEyeOff } from "react-icons/lu";

import LogoBlueIcon from "@assets/logo-blue.png";
import LogoWhiteIcon from "@assets/logo-white.svg";
import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@components/InputField";
import { Button } from "@components/Button";
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
  const password = formData.get("password");
  const confirmPassword = formData.get("confirmPassword");

  if (password !== confirmPassword) {
    return { error: m.local_auth_error_passwords_not_match() };
  }

  try {
    const response = await api.POST(`${DEVICE_API}/device/setup`, {
      localAuthMode: "password",
      password,
    });

    if (response.ok) {
      return redirect("/");
    } else {
      return { error: m.auth_mode_local_password_failed_set({ error: response.statusText }) };
    }
  } catch (error) {
    console.error("Error setting password:", error);
    return { error: m.auth_mode_local_password_failed_set({ error: String(error) }) };
  }
};

export default function WelcomeLocalPasswordRoute() {
  const actionData = useActionData() as { error?: string };
  const [showPassword, setShowPassword] = useState(false);
  const passwordInputRef = useRef<HTMLInputElement>(null);

  // Don't focus immediately, let the animation finish
  useEffect(() => {
    const timer = setTimeout(() => {
      passwordInputRef.current?.focus();
    }, 1000); // 1 second delay

    return () => clearTimeout(timer);
  }, []);

  return (
    <>
      <GridBackground />
      <div className="grid min-h-screen">
        <Container>
          <div className="isolate flex h-full w-full items-center justify-center">
            <div className="max-w-2xl space-y-8">
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
                  {m.auth_mode_local_password_set()}
                </h1>
                <p className="font-medium text-slate-600 dark:text-slate-400">
                  {m.auth_mode_local_password_set_description()}
                </p>
              </div>

              <Fieldset className="space-y-12">
                <Form method="POST" className="mx-auto max-w-sm space-y-4">
                  <div className="space-y-4">
                    <div
                      className="animate-fadeIn opacity-0"
                      style={{ animationDelay: "400ms" }}
                    >
                      <InputFieldWithLabel
                        label={m.auth_mode_local_password()}
                        type={showPassword ? "text" : "password"}
                        name="password"
                        placeholder={m.auth_mode_local_password_set_label()}
                        autoComplete="new-password"
                        ref={passwordInputRef}
                        TrailingElm={
                          showPassword ? (
                            <div
                              onClick={() => setShowPassword(false)}
                              className="pointer-events-auto"
                              role="switch"
                              aria-checked={showPassword}
                            >
                              <LuEye className="h-4 w-4 cursor-pointer text-slate-500 dark:text-slate-400" />
                            </div>
                          ) : (
                            <div
                              onClick={() => setShowPassword(true)}
                              className="pointer-events-auto"
                              role="switch"
                              aria-checked={!showPassword}
                            >
                              <LuEyeOff className="h-4 w-4 cursor-pointer text-slate-500 dark:text-slate-400" />
                            </div>
                          )
                        }
                      />
                    </div>
                    <div
                      className="animate-fadeIn opacity-0"
                      style={{ animationDelay: "400ms" }}
                    >
                      <InputFieldWithLabel
                        label={m.auth_mode_local_password_confirm_label()}
                        autoComplete="new-password"
                        type={showPassword ? "text" : "password"}
                        name="confirmPassword"
                        placeholder={m.auth_mode_local_password_confirm_description()}
                        error={actionData?.error}
                      />
                    </div>
                  </div>

                  {actionData?.error && <p className="text-sm text-red-600">{ }</p>}

                  <div
                    className="animate-fadeIn opacity-0"
                    style={{ animationDelay: "600ms" }}
                  >
                    <Button
                      size="LG"
                      theme="primary"
                      fullWidth
                      type="submit"
                      text={m.auth_mode_local_password_set_button()}
                      textAlign="center"
                    />
                  </div>
                </Form>
              </Fieldset>

              <p
                className="animate-fadeIn max-w-md text-center text-xs text-slate-500 opacity-0 dark:text-slate-400"
                style={{ animationDelay: "800ms" }}
              >
                {m.auth_mode_local_password_note()}&nbsp;<span className="font-bold">{m.auth_mode_local_password_note_local()}</span>
              </p>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}

WelcomeLocalPasswordRoute.loader = loader;
WelcomeLocalPasswordRoute.action = action;
