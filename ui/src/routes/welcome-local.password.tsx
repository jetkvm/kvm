import { ActionFunctionArgs, Form, redirect, useActionData } from "react-router-dom";
import { useState, useRef, useEffect } from "react";
import { LuEye, LuEyeOff } from "react-icons/lu";

import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@components/InputField";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.png";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import { DEVICE_API } from "@/ui.config";

import api from "../api";

import { DeviceStatus } from "./welcome-local";

const loader = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (res.isSetup) return redirect("/login-local");
  return null;
};

const action = async ({ request }: ActionFunctionArgs) => {
  const formData = await request.formData();
  const password = formData.get("password");
  const confirmPassword = formData.get("confirmPassword");

  if (password !== confirmPassword) {
    return { error: "Passwords do not match" };
  }

  try {
    const response = await api.POST(`${DEVICE_API}/device/setup`, {
      localAuthMode: "password",
      password,
    });

    if (response.ok) {
      return redirect("/");
    } else {
      return { error: "Failed to set password" };
    }
  } catch (error) {
    console.error("Error setting password:", error);
    return { error: "An error occurred while setting the password" };
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
          <div className="flex items-center justify-center w-full h-full isolate">
            <div className="max-w-2xl space-y-8">
              <div className="flex items-center justify-center opacity-0 animate-fadeIn">
                <img src={LogoWhiteIcon} alt="" className="-ml-4 h-[32px] hidden dark:block" />
                <img src={LogoBlueIcon} alt="" className="-ml-4 h-[32px] dark:hidden" />
              </div>

              <div
                className="space-y-2 text-center opacity-0 animate-fadeIn"
                style={{ animationDelay: "200ms" }}
              >
                <h1 className="text-4xl font-semibold text-black dark:text-white">Set a Password</h1>
                <p className="font-medium text-slate-600 dark:text-slate-400">
                  Create a strong password to secure your JetKVM device locally.
                </p>
              </div>

              <Fieldset className="space-y-12">
                <Form method="POST" className="max-w-sm mx-auto space-y-4">
                  <div className="space-y-4">
                    <div
                      className="opacity-0 animate-fadeIn"
                      style={{ animationDelay: "400ms" }}
                    >
                      <InputFieldWithLabel
                        label="Password"
                        type={showPassword ? "text" : "password"}
                        name="password"
                        placeholder="Enter a password"
                        autoComplete="new-password"
                        ref={passwordInputRef}
                        TrailingElm={
                          showPassword ? (
                            <div
                              onClick={() => setShowPassword(false)}
                              className="pointer-events-auto"
                            >
                              <LuEye className="w-4 h-4 cursor-pointer text-slate-500 dark:text-slate-400" />
                            </div>
                          ) : (
                            <div
                              onClick={() => setShowPassword(true)}
                              className="pointer-events-auto"
                            >
                              <LuEyeOff className="w-4 h-4 cursor-pointer text-slate-500 dark:text-slate-400" />
                            </div>
                          )
                        }
                      />
                    </div>
                    <div
                      className="opacity-0 animate-fadeIn"
                      style={{ animationDelay: "400ms" }}
                    >
                      <InputFieldWithLabel
                        label="Confirm Password"
                        autoComplete="new-password"
                        type={showPassword ? "text" : "password"}
                        name="confirmPassword"
                        placeholder="Confirm your password"
                        error={actionData?.error}
                      />
                    </div>
                  </div>

                  {actionData?.error && <p className="text-sm text-red-600">{}</p>}

                  <div
                    className="opacity-0 animate-fadeIn"
                    style={{ animationDelay: "600ms" }}
                  >
                    <Button
                      size="LG"
                      theme="primary"
                      fullWidth
                      type="submit"
                      text="Set Password"
                      textAlign="center"
                    />
                  </div>
                </Form>
              </Fieldset>

              <p
                className="max-w-md text-xs text-center opacity-0 animate-fadeIn text-slate-500 dark:text-slate-400"
                style={{ animationDelay: "800ms" }}
              >
                This password will be used to secure your device data and protect against
                unauthorized access.{" "}
                <span className="font-bold">All data remains on your local device.</span>
              </p>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}

WelcomeLocalPasswordRoute.action = action;
WelcomeLocalPasswordRoute.loader = loader;
