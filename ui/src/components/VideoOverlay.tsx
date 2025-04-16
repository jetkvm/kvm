import React from "react";
import { ExclamationTriangleIcon } from "@heroicons/react/24/solid";
import { ArrowPathIcon, ArrowRightIcon } from "@heroicons/react/16/solid";
import { motion, AnimatePresence } from "framer-motion";
import { LuPlay } from "react-icons/lu";
import { BsMouseFill } from "react-icons/bs";

import { Button, LinkButton } from "@components/Button";
import LoadingSpinner from "@components/LoadingSpinner";
import Card, { GridCard } from "@components/Card";

interface OverlayContentProps {
  children: React.ReactNode;
}
function OverlayContent({ children }: OverlayContentProps) {
  return (
    <GridCard cardClassName="h-full pointer-events-auto !outline-none">
      <div className="flex h-full w-full flex-col items-center justify-center rounded-md border border-slate-800/30 dark:border-slate-300/20">
        {children}
      </div>
    </GridCard>
  );
}

interface LoadingOverlayProps {
  show: boolean;
}

export function LoadingVideoOverlay({ show }: LoadingOverlayProps) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="aspect-video h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{
            duration: show ? 0.3 : 0.1,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="flex flex-col items-center justify-center gap-y-1">
              <div className="animate flex h-12 w-12 items-center justify-center">
                <LoadingSpinner className="h-8 w-8 text-blue-800 dark:text-blue-200" />
              </div>
              <p className="text-center text-sm text-slate-700 dark:text-slate-300">
                Loading video stream...
              </p>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface LoadingConnectionOverlayProps {
  show: boolean;
  text: string;
}
export function LoadingConnectionOverlay({ show, text }: LoadingConnectionOverlayProps) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="aspect-video h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0 } }}
          transition={{
            duration: 0.4,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="flex flex-col items-center justify-center gap-y-1">
              <div className="animate flex h-12 w-12 items-center justify-center">
                <LoadingSpinner className="h-8 w-8 text-blue-800 dark:text-blue-200" />
              </div>
              <p className="text-center text-sm text-slate-700 dark:text-slate-300">
                {text}
              </p>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface ConnectionErrorOverlayProps {
  show: boolean;
  setupPeerConnection: () => Promise<void>;
}

export function ConnectionFailedOverlay({
  show,
  setupPeerConnection,
}: ConnectionErrorOverlayProps) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="aspect-video h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0 } }}
          transition={{
            duration: 0.4,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="flex flex-col items-start gap-y-1">
              <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
              <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                <div className="space-y-4">
                  <div className="space-y-2 text-black dark:text-white">
                    <h2 className="text-xl font-bold">Connection Issue Detected</h2>
                    <ul className="list-disc space-y-2 pl-4 text-left">
                      <li>Verify that the device is powered on and properly connected</li>
                      <li>Check all cable connections for any loose or damaged wires</li>
                      <li>Ensure your network connection is stable and active</li>
                      <li>Try restarting both the device and your computer</li>
                    </ul>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <LinkButton
                      to={"https://jetkvm.com/docs/getting-started/troubleshooting"}
                      theme="primary"
                      text="Troubleshooting Guide"
                      TrailingIcon={ArrowRightIcon}
                      size="SM"
                    />
                    <Button
                      onClick={() => setupPeerConnection()}
                      LeadingIcon={ArrowPathIcon}
                      text="Try again"
                      size="SM"
                      theme="light"
                    />
                  </div>
                </div>
              </div>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface PeerConnectionDisconnectedOverlay {
  show: boolean;
}

export function PeerConnectionDisconnectedOverlay({
  show,
}: PeerConnectionDisconnectedOverlay) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="aspect-video h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0 } }}
          transition={{
            duration: 0.4,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="flex flex-col items-start gap-y-1">
              <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
              <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                <div className="space-y-4">
                  <div className="space-y-2 text-black dark:text-white">
                    <h2 className="text-xl font-bold">Connection Issue Detected</h2>
                    <ul className="list-disc space-y-2 pl-4 text-left">
                      <li>Verify that the device is powered on and properly connected</li>
                      <li>Check all cable connections for any loose or damaged wires</li>
                      <li>Ensure your network connection is stable and active</li>
                      <li>Try restarting both the device and your computer</li>
                    </ul>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <Card>
                      <div className="flex items-center gap-x-2 p-4">
                        <LoadingSpinner className="h-4 w-4 text-blue-800 dark:text-blue-200" />
                        <p className="text-sm text-slate-700 dark:text-slate-300">
                          Retrying connection...
                        </p>
                      </div>
                    </Card>
                  </div>
                </div>
              </div>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
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
      <AnimatePresence>
        {show && isNoSignal && (
          <motion.div
            className="absolute inset-0 aspect-video h-full w-full"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{
              duration: 0.3,
              ease: "easeInOut",
            }}
          >
            <OverlayContent>
              <div className="flex flex-col items-start gap-y-1">
                <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
                <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                  <div className="space-y-4">
                    <div className="space-y-2 text-black dark:text-white">
                      <h2 className="text-xl font-bold">No HDMI signal detected.</h2>
                      <ul className="list-disc space-y-2 pl-4 text-left">
                        <li>Ensure the HDMI cable securely connected at both ends</li>
                        <li>
                          Ensure source device is powered on and outputting a signal
                        </li>
                        <li>
                          If using an adapter, ensure it&apos;s compatible and functioning
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
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {show && isOtherError && (
          <motion.div
            className="absolute inset-0 aspect-video h-full w-full"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{
              duration: 0.3,
              ease: "easeInOut",
            }}
          >
            <OverlayContent>
              <div className="flex flex-col items-start gap-y-1">
                <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
                <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                  <div className="space-y-4">
                    <div className="space-y-2 text-black dark:text-white">
                      <h2 className="text-xl font-bold">HDMI signal error detected.</h2>
                      <ul className="list-disc space-y-2 pl-4 text-left">
                        <li>A loose or faulty HDMI connection</li>
                        <li>Incompatible resolution or refresh rate settings</li>
                        <li>Issues with the source device&apos;s HDMI output</li>
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
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

interface NoAutoplayPermissionsOverlayProps {
  show: boolean;
  onPlayClick: () => void;
}

export function NoAutoplayPermissionsOverlay({
  show,
  onPlayClick,
}: NoAutoplayPermissionsOverlayProps) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="absolute inset-0 z-10 aspect-video h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{
            duration: 0.3,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="space-y-4">
              <h2 className="text-2xl font-extrabold text-black dark:text-white">
                Autoplay permissions required
              </h2>

              <div className="space-y-2 text-center">
                <div>
                  <Button
                    size="MD"
                    theme="primary"
                    LeadingIcon={LuPlay}
                    text="Manually start stream"
                    onClick={onPlayClick}
                  />
                </div>

                <div className="text-xs text-slate-600 dark:text-slate-400">
                  Please adjust browser settings to enable autoplay
                </div>
              </div>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface PointerLockBarProps {
  show: boolean;
}

export function PointerLockBar({ show }: PointerLockBarProps) {
  return (
    <AnimatePresence mode="wait">
      {show ? (
        <motion.div
          className="absolute -top-[36px] left-0 right-0 z-20 bg-white"
          initial={{ y: 20, opacity: 0, zIndex: 0 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ y: 43, zIndex: 0 }}
          transition={{ duration: 0.5, ease: "easeInOut", delay: 0.5 }}
        >
          <div>
            <Card className="rounded-b-none shadow-none !outline-0">
              <div className="flex items-center justify-between border border-slate-800/50 px-4 py-2 outline-0 backdrop-blur-sm dark:border-slate-300/20 dark:bg-slate-800">
                <div className="flex items-center space-x-2">
                  <BsMouseFill className="h-4 w-4 text-blue-700 dark:text-blue-500" />
                  <span className="text-sm text-black dark:text-white">
                    Click on the video to enable mouse control
                  </span>
                </div>
              </div>
            </Card>
          </div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}
