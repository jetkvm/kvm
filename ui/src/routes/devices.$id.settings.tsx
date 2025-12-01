import React, { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useResizeObserver } from "usehooks-ts";
import {
  LuSettings,
  LuMouse,
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

import { cx } from "@/cva.config";
import { useUiStore, useFailsafeModeStore } from "@hooks/stores";
import Card from "@components/Card";
import { FailsafeModeBanner } from "@components/FailSafeModeBanner";
import { FeatureFlag } from "@components/FeatureFlag";
import { LinkButton } from "@components/Button";
import { m } from "@localizations/messages.js";

/* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
export default function SettingsRoute() {
  const location = useLocation();
  const { setDisableVideoFocusTrap } = useUiStore();
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [showLeftGradient, setShowLeftGradient] = useState(false);
  const [showRightGradient, setShowRightGradient] = useState(false);
  const { width = 0 } = useResizeObserver({ ref: scrollContainerRef as React.RefObject<HTMLDivElement> });
  const { isFailsafeMode: isFailsafeMode, reason: failsafeReason } = useFailsafeModeStore();
  const isVideoDisabled = isFailsafeMode && failsafeReason === "video";

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
    setTimeout(() => {
      setDisableVideoFocusTrap(true);
    }, 500);

    return () => {
      setDisableVideoFocusTrap(false);
    };
  }, [setDisableVideoFocusTrap]);

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
                  text={m.settings_back_to_kvm()}
                  LeadingIcon={LuArrowLeft}
                  textAlign="left"
                />
              </div>
              <div className="hidden md:block">
                <LinkButton
                  to=".."
                  size="SM"
                  theme="blank"
                  text={m.settings_back_to_kvm()}
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
                  "pointer-events-none absolute inset-y-0 left-0 z-10 w-8 bg-linear-to-r from-white to-transparent transition-opacity duration-300 ease-in-out md:hidden dark:from-slate-900",
                  {
                    "opacity-0": !showLeftGradient,
                    "opacity-100": showLeftGradient,
                  },
                )}
              ></div>
              {/* Gradient overlay for right side - only visible on mobile when there's more content */}
              <div
                className={cx(
                  "pointer-events-none absolute inset-y-0 right-0 z-10 w-8 bg-linear-to-l from-white to-transparent transition duration-300 ease-in-out md:hidden dark:from-slate-900",
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
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuSettings className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_general()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="mouse"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuMouse className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_mouse()}</h1>
                    </div>
                  </NavLink>
                </div>
                <FeatureFlag minAppVersion="0.4.0" name="Paste text">
                  <div className="shrink-0">
                    <NavLink
                      to="keyboard"
                      className={({ isActive }) => (isActive ? "active" : "")}
                    >
                      <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                        <LuKeyboard className="h-4 w-4 shrink-0" />
                        <h1>{m.settings_keyboard()}</h1>
                      </div>
                    </NavLink>
                  </div>
                </FeatureFlag>
                <div className={cx("shrink-0", {
                  "opacity-50 cursor-not-allowed": isVideoDisabled
                })}>
                  <NavLink
                    to="video"
                    className={({ isActive }) => cx(isActive ? "active" : "", {
                      "pointer-events-none": isVideoDisabled
                    })}                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuVideo className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_video()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className={cx("shrink-0", {
                  "opacity-50 cursor-not-allowed": isVideoDisabled
                })}>
                  <NavLink
                    to="hardware"
                    className={({ isActive }) => cx(isActive ? "active" : "", {
                      "pointer-events-none": isVideoDisabled
                    })}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuCpu className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_hardware()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="access"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuShieldCheck className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_access()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="appearance"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuPalette className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_appearance()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="macros"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuCommand className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_keyboard_macros()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="network"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuNetwork className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_network()}</h1>
                    </div>
                  </NavLink>
                </div>
                <div className="shrink-0">
                  <NavLink
                    to="advanced"
                    className={({ isActive }) => (isActive ? "active" : "")}
                  >
                    <div className="flex items-center gap-x-2 rounded-md px-2.5 py-2.5 text-sm transition-colors hover:bg-slate-100 dark:hover:bg-slate-700 in-[.active]:bg-blue-50 in-[.active]:text-blue-700! md:in-[.active]:bg-transparent dark:in-[.active]:bg-blue-900 dark:in-[.active]:text-blue-200! dark:md:in-[.active]:bg-transparent">
                      <LuWrench className="h-4 w-4 shrink-0" />
                      <h1>{m.settings_advanced()}</h1>
                    </div>
                  </NavLink>
                </div>
              </div>
            </Card>
          </div>
          <div className="w-full md:col-span-6 space-y-4">
            {isFailsafeMode && failsafeReason && <FailsafeModeBanner reason={failsafeReason} />}
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
