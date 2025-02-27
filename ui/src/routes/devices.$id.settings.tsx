import { NavLink, Outlet, useLocation } from "react-router-dom";
import Card from "@/components/Card";
import {
  LuSettings,
  LuKeyboard,
  LuVideo,
  LuCpu,
  LuShieldCheck,
  LuWrench,
  LuArrowLeft,
  LuPalette,
} from "react-icons/lu";
import { LinkButton } from "../components/Button";
import React, { useEffect } from "react";
import { cx } from "../cva.config";
import { useUiStore } from "../hooks/stores";
import useKeyboard from "../hooks/useKeyboard";

/* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
export default function SettingsRoute() {
  const location = useLocation();
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const { sendKeyboardEvent } = useKeyboard();

  useEffect(() => {
    // disable focus trap
    setTimeout(() => {
      // Reset keyboard state. Incase the user is pressing a key while enabling the sidebar
      sendKeyboardEvent([], []);
      setDisableVideoFocusTrap(true);
      // For some reason, the focus trap is not disabled immediately
      // so we need to blur the active element
      (document.activeElement as HTMLElement)?.blur();
      console.log("Just disabled focus trap");
    }, 300);

    return () => {
      setDisableVideoFocusTrap(false);
    };
  }, [setDisableVideoFocusTrap, sendKeyboardEvent]);

  return (
    <div className="pointer-events-auto relative mx-auto max-w-4xl translate-x-0 transform text-left dark:text-white">
      <div className="h-full">
        <div className="grid w-full gap-x-8 gap-y-4 md:grid-cols-8">
          <div className="w-full select-none space-y-4 md:col-span-2">
            <Card className="flex w-full gap-x-4 p-2 md:flex-col dark:bg-slate-800">
              <LinkButton
                to=".."
                size="SM"
                theme="blank"
                text="Back to KVM"
                LeadingIcon={LuArrowLeft}
                textAlign="left"
                fullWidth
              />
            </Card>
            <Card className="flex w-full gap-x-4 p-2 md:flex-col dark:bg-slate-800">
              <div>
                <NavLink
                  to="general"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuSettings className="h-4 w-4 shrink-0" />
                    <h1>General</h1>
                  </div>
                </NavLink>
              </div>

              <div>
                <NavLink
                  to="mouse"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuKeyboard className="h-4 w-4 shrink-0" />
                    <h1>Mouse</h1>
                  </div>
                </NavLink>
              </div>
              <div>
                <NavLink
                  to="video"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuVideo className="h-4 w-4 shrink-0" />
                    <h1>Video</h1>
                  </div>
                </NavLink>
              </div>
              <div>
                <NavLink
                  to="hardware"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuCpu className="h-4 w-4 shrink-0" />
                    <h1>Hardware</h1>
                  </div>
                </NavLink>
              </div>
              <div>
                <NavLink
                  to="security"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuShieldCheck className="h-4 w-4 shrink-0" />
                    <h1>Security</h1>
                  </div>
                </NavLink>
              </div>
              <div>
                <NavLink
                  to="appearance"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuPalette className="h-4 w-4 shrink-0" />
                    <h1>Appearance</h1>
                  </div>
                </NavLink>
              </div>
              <div>
                <NavLink
                  to="advanced"
                  className={({ isActive }) => (isActive ? "active" : "")}
                >
                  <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                    <LuWrench className="h-4 w-4 shrink-0" />
                    <h1>Advanced</h1>
                  </div>
                </NavLink>
              </div>
            </Card>
          </div>
          <div className="w-full md:col-span-5">
            {/* <AutoHeight> */}
            <Card className="dark:bg-slate-800">
              <div
                className="space-y-4 px-8 py-6"
                style={{ animationDuration: "0.7s" }}
                key={location.pathname} // This is a workaround to force the animation to run when the route changes
              >
                <Outlet />
              </div>
            </Card>
            {/* </AutoHeight> */}
          </div>
        </div>
      </div>
    </div>
  );
}

export function SettingsItem({
  title,
  description,
  children,
  className,
}: {
  title: string;
  description: string | React.ReactNode;
  children?: React.ReactNode;
  className?: string;
  name?: string;
}) {
  return (
    <label
      className={cx(
        "flex select-none items-center justify-between gap-x-8 rounded",
        className,
      )}
    >
      <div className="space-y-0.5">
        <h3 className="text-base font-semibold text-black dark:text-white">{title}</h3>
        <p className="text-sm text-slate-700 dark:text-slate-300">{description}</p>
      </div>
      {children ? <div>{children}</div> : null}
    </label>
  );
}
