import { Form, redirect, useActionData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction } from "react-router";
import { useState } from "react";
import { LuEye, LuEyeOff } from "react-icons/lu";

import SimpleNavbar from "@components/SimpleNavbar";
import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@components/InputField";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.png";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import { DEVICE_API } from "@/ui.config";

import api from "../api";
import ExtLink from "../components/ExtLink";

import { DeviceStatus } from "./welcome-local";

const loader: LoaderFunction = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (!res.isSetup) return redirect("/welcome");

  const deviceRes = await api.GET(`${DEVICE_API}/device`);
  if (deviceRes.ok) return redirect("/");
  return null;
};

const action: ActionFunction = async ({ request }: ActionFunctionArgs) => {
  const formData = await request.formData();
  const password = formData.get("password");

  try {
    const response = await api.POST(`${DEVICE_API}/auth/login-local`, {
      password,
    });

    if (response.ok) {
      return redirect("/");
    } else {
      return { error: "Invalid password" };
    }
  } catch (error) {
    console.error(error);
    return { error: "An error occurred while logging in" };
  }
};

export default function LoginLocalRoute() {
  const actionData = useActionData() as { error?: string; success?: boolean };
  const [showPassword, setShowPassword] = useState(false);

  return (
    <>
      <GridBackground />
      <div className="grid min-h-screen grid-rows-(--grid-layout)">
        <SimpleNavbar />
        <Container>
          <div className="isolate flex h-full w-full items-center justify-center">
            <div className="-mt-32 max-w-2xl space-y-8">
              <div className="flex items-center justify-center">
                <img
                  src={LogoWhiteIcon}
                  alt=""
                  className="-ml-4 hidden h-[32px] dark:block"
                />
                <img src={LogoBlueIcon} alt="" className="-ml-4 h-[32px] dark:hidden" />
              </div>

              <div className="space-y-2 text-center">
                <h1 className="text-4xl font-semibold text-black dark:text-white">
                  Welcome back to JetKVM
                </h1>
                <p className="font-medium text-slate-600 dark:text-slate-400">
                  Enter your password to access your JetKVM.
                </p>
              </div>

              <Fieldset className="space-y-12">
                <Form method="POST" className="mx-auto max-w-sm space-y-4">
                  <div className="space-y-4">
                    <InputFieldWithLabel
                      label="Password"
                      type={showPassword ? "text" : "password"}
                      name="password"
                      autoComplete="current-password"
                      placeholder="Enter your password"
                      autoFocus
                      error={actionData?.error}
                      TrailingElm={
                        showPassword ? (
                          <div
                            onClick={() => setShowPassword(false)}
                            className="pointer-events-auto"
                          >
                            <LuEye className="h-4 w-4 cursor-pointer text-slate-500 dark:text-slate-400" />
                          </div>
                        ) : (
                          <div
                            onClick={() => setShowPassword(true)}
                            className="pointer-events-auto"
                          >
                            <LuEyeOff className="h-4 w-4 cursor-pointer text-slate-500 dark:text-slate-400" />
                          </div>
                        )
                      }
                    />
                  </div>

                  <Button
                    size="LG"
                    theme="primary"
                    fullWidth
                    type="submit"
                    text="Log In"
                    textAlign="center"
                  />

                  <div className="mt-4 flex justify-start text-xs text-slate-500 dark:text-slate-400">
                    <ExtLink
                      href="https://jetkvm.com/docs/networking/local-access#reset-password"
                      className="hover:underline"
                    >
                      Forgot password?
                    </ExtLink>
                  </div>
                </Form>
              </Fieldset>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}

LoginLocalRoute.loader = loader;
LoginLocalRoute.action = action;
