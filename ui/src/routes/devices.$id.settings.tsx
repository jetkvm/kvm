import { NavLink, Outlet, useLocation } from "react-router-dom";
import {
  LuSettings,
  LuKeyboard,
  LuVideo,
  LuCpu,
  LuShieldCheck,
  LuWrench,
  LuArrowLeft,
  LuPalette,
  LuCommand,
  LuNetwork,
} from "react-icons/lu";
import React, { useEffect, useRef, useState } from "react";

import Card from "@/components/Card";

import { LinkButton } from "../components/Button";
import { cx } from "../cva.config";
import { useUiStore } from "../hooks/stores";
import useKeyboard from "../hooks/useKeyboard";
import { useResizeObserver } from "../hooks/useResizeObserver";
import LoadingSpinner from "../components/LoadingSpinner";

/* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
export default function SettingsRoute() {
  const location = useLocation();
  const setDisableVideoFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const { sendKeyboardEvent } = useKeyboard();
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [showLeftGradient, setShowLeftGradient] = useState(false);
  const [showRightGradient, setShowRightGradient] = useState(false);
  const { width } = useResizeObserver({ ref: scrollContainerRef });

  // Handle scroll position to show/hide gradients
  const handleScroll = () => {
    if (scrollContainerRef.current) {
      const { scrollLeft, scrollWidth, clientWidth } = scrollContainerRef.current;
      // Show left gradient only if scrolled to the right
      setShowLeftGradient(scrollLeft > 0);
      // Show right gradient only if there's more content to scroll to the right
      setShowRightGradient(scrollLeft < scrollWidth - clientWidth - 1); // -1 for rounding errors
    }
  };

  useEffect(() => {
    // Check initial scroll position
    handleScroll();

    // Add scroll event listener to the container
    const scrollContainer = scrollContainerRef.current;
    if (scrollContainer) {
      scrollContainer.addEventListener("scroll", handleScroll);
    }

    return () => {
      // Clean up event listener
      if (scrollContainer) {
        scrollContainer.removeEventListener("scroll", handleScroll);
      }
    };
  }, [width]);

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
        <div className="w-full gap-x-8 gap-y-4 space-y-4 md:grid md:grid-cols-8 md:space-y-0">
          <div className="w-full select-none space-y-4 md:col-span-2">
            <Card className="flex w-full gap-x-4 overflow-hidden p-2 md:flex-col dark:bg-slate-800">
              <div className="md:hidden">
                <LinkButton
                  to=".."
                  size="SM"
                  theme="blank"
                  text="Back to KVM"
                  LeadingIcon={LuArrowLeft}
                  textAlign="left"
                />
              </div>
              <div className="hidden md:block">
                <LinkButton
                  to=".."
                  size="SM"
                  theme="blank"
                  text="Back to KVM"
                  LeadingIcon={LuArrowLeft}
                  textAlign="left"
                  fullWidth
                />
              </div>
            </Card>
            <Card className="relative overflow-hidden">
              {/* Gradient overlay for left side - only visible on mobile when scrolled */}
              <div
                className={cx(
                  "pointer-events-none absolute inset-y-0 left-0 z-10 w-8 bg-gradient-to-r from-white to-transparent transition-opacity duration-300 ease-in-out md:hidden dark:from-slate-900",
                  {
                    "opacity-0": !showLeftGradient,
                    "opacity-100": showLeftGradient,
                  },
                )}
              ></div>
              {/* Gradient overlay for right side - only visible on mobile when there's more content */}
              <div
                className={cx(
                  "pointer-events-none absolute inset-y-0 right-0 z-10 w-8 bg-gradient-to-l from-white to-transparent transition duration-300 ease-in-out md:hidden dark:from-slate-900",
                  {
                    "opacity-0": !showRightGradient,
                    "opacity-100": showRightGradient,
                  },
                )}
              ></div>
              <div
                ref={scrollContainerRef}
                className="hide-scrollbar relative flex w-full gap-x-4 overflow-x-auto whitespace-nowrap p-2 md:flex-col md:overflow-visible md:whitespace-normal dark:bg-slate-800"
              >
                <div className="shrink-0">
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
                <div className="shrink-0">
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
                <div className="shrink-0">
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
                <div className="shrink-0">
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
                <div className="shrink-0">
                  <NavLink
                    to="access"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                      <LuShieldCheck className="h-4 w-4 shrink-0" />
                      <h1>Access</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
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
                <div className="shrink-0">
                  <NavLink
                    to="macros"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                      <LuCommand className="h-4 w-4 shrink-0" />
                      <h1>Keyboard Macros</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="network"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 [.active_&]:bg-blue-50 [.active_&]:!text-blue-700 md:[.active_&]:bg-transparent dark:[.active_&]:bg-blue-900 dark:[.active_&]:!text-blue-200 dark:md:[.active_&]:bg-transparent">
                      <LuNetwork className="h-4 w-4 shrink-0" />
                      <h1>Network</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
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
              </div>
            </Card>
          </div>
          <div className="w-full md:col-span-6">
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
  loading,
  badge,
}: {
  title: string;
  description: string | React.ReactNode;
  children?: React.ReactNode;
  className?: string;
  name?: string;
  loading?: boolean;
  badge?: string;
}) {
  return (
    <label
      className={cx(
        "flex select-none items-center justify-between gap-x-8 rounded",
        className,
      )}
    >
      <div className="space-y-0.5">
        <div className="flex items-center gap-x-2">
          <div className="flex items-center text-base font-semibold text-black dark:text-white">
            {title}
            {badge && (
              <span className="ml-2 rounded-full bg-red-500 px-2 py-1 text-[10px] font-medium leading-none text-white dark:border dark:border-red-700 dark:bg-red-800 dark:text-red-50">
                {badge}
              </span>
            )}
          </div>
          {loading && <LoadingSpinner className="h-4 w-4 text-blue-500" />}
        </div>
        <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>
      </div>
      {children ? <div>{children}</div> : null}
    </label>
  );
}
