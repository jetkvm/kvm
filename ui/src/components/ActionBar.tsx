import { Button } from "@components/Button";
import {
  useHidStore,
  useMountMediaStore,
  useUiStore,
  useSettingsStore,
} from "@/hooks/stores";
import { MdOutlineContentPasteGo } from "react-icons/md";
import Container from "@components/Container";
import { LuHardDrive, LuMaximize, LuSettings, LuSignal } from "react-icons/lu";
import { cx } from "@/cva.config";
import PasteModal from "@/components/popovers/PasteModal";
import { FaKeyboard } from "react-icons/fa6";
import WakeOnLanModal from "@/components/popovers/WakeOnLan/Index";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";
import MountPopopover from "./popovers/MountPopover";
import { Fragment, useCallback, useRef } from "react";
import { CommandLineIcon } from "@heroicons/react/20/solid";

export default function Actionbar({
  requestFullscreen,
}: {
  requestFullscreen: () => Promise<void>;
}) {
  const virtualKeyboard = useHidStore(state => state.isVirtualKeyboardEnabled);

  const setVirtualKeyboard = useHidStore(state => state.setVirtualKeyboardEnabled);
  const toggleSidebarView = useUiStore(state => state.toggleSidebarView);
  const setDisableFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const enableTerminal = useUiStore(state => state.enableTerminal);
  const setEnableTerminal = useUiStore(state => state.setEnableTerminal);
  const remoteVirtualMediaState = useMountMediaStore(
    state => state.remoteVirtualMediaState,
  );
  const developerMode = useSettingsStore(state => state.developerMode);

  // This is the only way to get a reliable state change for the popover
  // at time of writing this there is no mount, or unmount event for the popover
  const isOpen = useRef<boolean>(false);
  const checkIfStateChanged = useCallback(
    (open: boolean) => {
      if (open !== isOpen.current) {
        isOpen.current = open;
        if (!open) {
          setTimeout(() => {
            setDisableFocusTrap(false);
            console.log("Popover is closing. Returning focus trap to video");
          }, 0);
        }
      }
    },
    [setDisableFocusTrap],
  );

  return (
    <Container className="bg-white border-b border-b-slate-800/20 dark:bg-slate-900 dark:border-b-slate-300/20">
      <div
        onKeyUp={e => e.stopPropagation()}
        onKeyDown={e => e.stopPropagation()}
        className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-1.5"
      >
        <div className="relative flex flex-wrap items-center gap-x-2 gap-y-2">
          {developerMode && (
            <Button
              size="XS"
              theme="light"
              text="Web Terminal"
              LeadingIcon={({ className }) => <CommandLineIcon className={className} />}
              onClick={() => setEnableTerminal(!enableTerminal)}
            />
          )}
          <Popover>
            <PopoverButton as={Fragment}>
              <Button
                size="XS"
                theme="light"
                text="Paste text"
                LeadingIcon={MdOutlineContentPasteGo}
                onClick={() => {
                  setDisableFocusTrap(true);
                }}
              />
            </PopoverButton>
            <PopoverPanel
              anchor="bottom start"
              transition
              className={cx(
                "z-10 flex w-[420px] origin-top flex-col !overflow-visible",
                "flex origin-top flex-col transition duration-300 ease-out data-[closed]:translate-y-8 data-[closed]:opacity-0",
              )}
            >
              {({ open }) => {
                checkIfStateChanged(open);
                return (
                  <div className="w-full max-w-xl mx-auto">
                    <PasteModal />
                  </div>
                );
              }}
            </PopoverPanel>
          </Popover>
          <div className="relative">
            <Popover>
              <PopoverButton as={Fragment}>
                <Button
                  size="XS"
                  theme="light"
                  text="Virtual Media"
                  LeadingIcon={({ className }) => {
                    return (
                      <>
                        <LuHardDrive className={className} />
                        <div
                          className={cx(className, "h-2 w-2 rounded-full bg-blue-700", {
                            hidden: !remoteVirtualMediaState,
                          })}
                        />
                      </>
                    );
                  }}
                  onClick={() => {
                    setDisableFocusTrap(true);
                  }}
                />
              </PopoverButton>
              <PopoverPanel
                anchor="bottom start"
                transition
                className={cx(
                  "z-10 flex w-[420px] origin-top flex-col !overflow-visible",
                  "flex origin-top flex-col transition duration-300 ease-out data-[closed]:translate-y-8 data-[closed]:opacity-0",
                )}
              >
                {({ open }) => {
                  checkIfStateChanged(open);
                  return (
                    <div className="w-full max-w-xl mx-auto">
                      <MountPopopover />
                    </div>
                  );
                }}
              </PopoverPanel>
            </Popover>
          </div>
          <div>
            <Popover>
              <PopoverButton as={Fragment}>
                <Button
                  size="XS"
                  theme="light"
                  text="Wake on Lan"
                  onClick={() => {
                    setDisableFocusTrap(true);
                  }}
                  LeadingIcon={({ className }) => (
                    <svg
                      className={className}
                      xmlns="http://www.w3.org/2000/svg"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <path d="m15 20 3-3h2a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h2l3 3z" />
                      <path d="M6 8v1" />
                      <path d="M10 8v1" />
                      <path d="M14 8v1" />
                      <path d="M18 8v1" />
                    </svg>
                  )}
                />
              </PopoverButton>
              <PopoverPanel
                anchor="bottom start"
                transition
                style={{
                  transitionProperty: "opacity",
                }}
                className={cx(
                  "z-10 flex w-[420px] origin-top flex-col !overflow-visible",
                  "flex origin-top flex-col transition duration-300 ease-out data-[closed]:translate-y-8 data-[closed]:opacity-0",
                )}
              >
                {({ open }) => {
                  checkIfStateChanged(open);
                  return (
                    <div className="w-full max-w-xl mx-auto">
                      <WakeOnLanModal />
                    </div>
                  );
                }}
              </PopoverPanel>
            </Popover>
          </div>
          <div className="hidden lg:block">
            <Button
              size="XS"
              theme="light"
              text="Virtual Keyboard"
              LeadingIcon={FaKeyboard}
              onClick={() => setVirtualKeyboard(!virtualKeyboard)}
            />
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-x-2 gap-y-2">
          <div className="block lg:hidden">
            <Button
              size="XS"
              theme="light"
              text="Virtual Keyboard"
              LeadingIcon={FaKeyboard}
              onClick={() => setVirtualKeyboard(!virtualKeyboard)}
            />
          </div>
          <div className="hidden md:block">
            <Button
              size="XS"
              theme="light"
              text="Connection Stats"
              LeadingIcon={({ className }) => (
                <LuSignal
                  className={cx(className, "mb-0.5 text-green-500")}
                  strokeWidth={4}
                />
              )}
              onClick={() => {
                toggleSidebarView("connection-stats");
              }}
            />
          </div>

          <div className="hidden xs:block ">
            <Button
              size="XS"
              theme="light"
              text="Settings"
              LeadingIcon={LuSettings}
              onClick={() => toggleSidebarView("system")}
            />
          </div>
          <div className="items-center hidden gap-x-2 lg:flex">
            <div className="h-4 w-[1px] bg-slate-300 dark:bg-slate-600" />
            <Button
              size="XS"
              theme="light"
              text="Fullscreen"
              LeadingIcon={LuMaximize}
              onClick={() => requestFullscreen()}
            />
          </div>
        </div>
      </div>
    </Container>
  );
}
