import React from "react";
import { Transition } from "@headlessui/react";
import { ExclamationTriangleIcon } from "@heroicons/react/24/solid";
import { ArrowRightIcon } from "@heroicons/react/16/solid";
import { LinkButton } from "@components/Button";
import LoadingSpinner from "@components/LoadingSpinner";
import { GridCard } from "@components/Card";

interface OverlayContentProps {
  children: React.ReactNode;
}
function OverlayContent({ children }: OverlayContentProps) {
  return (
    <GridCard cardClassName="h-full pointer-events-auto !outline-none">
      <div className="flex flex-col items-center justify-center w-full h-full border rounded-md border-slate-800/30 dark:border-slate-300/20">
        {children}
      </div>
    </GridCard>
  );
}

interface LoadingOverlayProps {
  show: boolean;
}

export function LoadingOverlay({ show }: LoadingOverlayProps) {
  return (
    <Transition
      show={show}
      enter="transition-opacity duration-300"
      enterFrom="opacity-0"
      enterTo="opacity-100"
      leave="transition-opacity duration-100"
      leaveFrom="opacity-100"
      leaveTo="opacity-0"
    >
      <div className="absolute inset-0 w-full h-full aspect-video">
        <OverlayContent>
          <div className="flex flex-col items-center justify-center gap-y-1">
            <div className="flex items-center justify-center w-12 h-12 animate">
              <LoadingSpinner className="w-8 h-8 text-blue-800 dark:text-blue-200" />
            </div>
            <p className="text-sm text-center text-slate-700 dark:text-slate-300">
              Loading video stream...
            </p>
          </div>
        </OverlayContent>
      </div>
    </Transition>
  );
}

interface ConnectionErrorOverlayProps {
  show: boolean;
}

export function ConnectionErrorOverlay({ show }: ConnectionErrorOverlayProps) {
  return (
    <Transition
      show={show}
      enter="transition duration-300"
      enterFrom="opacity-0"
      enterTo="opacity-100"
      leave="transition duration-300"
      leaveFrom="opacity-100"
      leaveTo="opacity-0"
    >
      <div className="absolute inset-0 z-10 w-full h-full aspect-video">
        <OverlayContent>
          <div className="flex flex-col items-start gap-y-1">
            <ExclamationTriangleIcon className="w-12 h-12 text-yellow-500" />
            <div className="text-sm text-left text-slate-700 dark:text-slate-300">
              <div className="space-y-4">
                <div className="space-y-2 text-black dark:text-white">
                  <h2 className="text-xl font-bold">Connection Issue Detected</h2>
                  <ul className="pl-4 space-y-2 text-left list-disc">
                    <li>Verify that the device is powered on and properly connected</li>
                    <li>Check all cable connections for any loose or damaged wires</li>
                    <li>Ensure your network connection is stable and active</li>
                    <li>Try restarting both the device and your computer</li>
                  </ul>
                </div>
                <div>
                  <LinkButton
                    to={"https://jetkvm.com/docs/getting-started/troubleshooting"}
                    theme="light"
                    text="Troubleshooting Guide"
                    TrailingIcon={ArrowRightIcon}
                    size="SM"
                  />
                </div>
              </div>
            </div>
          </div>
        </OverlayContent>
      </div>
    </Transition>
  );
}

interface HDMIErrorOverlayProps {
  show: boolean;
  hdmiState: string;
}

export function HDMIErrorOverlay({ show, hdmiState }: HDMIErrorOverlayProps) {
  const isNoSignal = hdmiState === "no_signal";
  const isOtherError = hdmiState === "no_lock" || hdmiState === "out_of_range";

  return (
    <>
      <Transition
        show={show && isNoSignal}
        enter="transition duration-300"
        enterFrom="opacity-0"
        enterTo="opacity-100"
        leave="transition-all duration-300"
        leaveFrom="opacity-100"
        leaveTo="opacity-0"
      >
        <div className="absolute inset-0 w-full h-full aspect-video">
          <OverlayContent>
            <div className="flex flex-col items-start gap-y-1">
              <ExclamationTriangleIcon className="w-12 h-12 text-yellow-500" />
              <div className="text-sm text-left text-slate-700 dark:text-slate-300">
                <div className="space-y-4">
                  <div className="space-y-2 text-black dark:text-white">
                    <h2 className="text-xl font-bold">No HDMI signal detected.</h2>
                    <ul className="pl-4 space-y-2 text-left list-disc">
                      <li>Ensure the HDMI cable securely connected at both ends</li>
                      <li>Ensure source device is powered on and outputting a signal</li>
                      <li>
                        If using an adapter, it&apos;s compatible and functioning
                        correctly
                      </li>
                    </ul>
                  </div>
                  <div>
                    <LinkButton
                      to={"https://jetkvm.com/docs/getting-started/troubleshooting"}
                      theme="light"
                      text="Learn more"
                      TrailingIcon={ArrowRightIcon}
                      size="SM"
                    />
                  </div>
                </div>
              </div>
            </div>
          </OverlayContent>
        </div>
      </Transition>
      <Transition
        show={show && isOtherError}
        enter="transition duration-300"
        enterFrom="opacity-0"
        enterTo="opacity-100"
        leave="transition duration-300 "
        leaveFrom="opacity-100"
        leaveTo="opacity-0"
      >
        <div className="absolute inset-0 w-full h-full aspect-video">
          <OverlayContent>
            <div className="flex flex-col items-start gap-y-1">
              <ExclamationTriangleIcon className="w-12 h-12 text-yellow-500" />
              <div className="text-sm text-left text-slate-700 dark:text-slate-300">
                <div className="space-y-4">
                  <div className="space-y-2 text-black dark:text-white">
                    <h2 className="text-xl font-bold">HDMI signal error detected.</h2>
                    <ul className="pl-4 space-y-2 text-left list-disc">
                      <li>A loose or faulty HDMI connection</li>
                      <li>Incompatible resolution or refresh rate settings</li>
                      <li>Issues with the source device&apos;s HDMI output</li>
                    </ul>
                  </div>
                  <div>
                    <LinkButton
                      to={"/help/hdmi-error"}
                      theme="light"
                      text="Learn more"
                      TrailingIcon={ArrowRightIcon}
                      size="SM"
                    />
                  </div>
                </div>
              </div>
            </div>
          </OverlayContent>
        </div>
      </Transition>
    </>
  );
}
