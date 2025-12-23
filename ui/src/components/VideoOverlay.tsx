import React, { useEffect, useState, useRef, useCallback } from "react";
import { ExclamationTriangleIcon } from "@heroicons/react/24/solid";
import { ArrowPathIcon, ArrowRightIcon } from "@heroicons/react/16/solid";
import { motion, AnimatePresence } from "framer-motion";
import { LuPlay } from "react-icons/lu";
import { BsMouseFill } from "react-icons/bs";

import { m } from "@localizations/messages.js";
import { Button, LinkButton } from "@components/Button";
import LoadingSpinner from "@components/LoadingSpinner";
import Card, { GridCard } from "@components/Card";
import { useRTCStore, PostRebootAction } from "@/hooks/stores";
import LogoBlue from "@/assets/logo-blue.svg";
import LogoWhite from "@/assets/logo-white.svg";
import { isOnDevice } from "@/main";
import { sleep, buildCloudUrl } from "@/utils";
import { getLocalVersion } from "@/utils/jsonrpc";

interface OverlayContentProps {
  readonly children: React.ReactNode;
}
function OverlayContent({ children }: OverlayContentProps) {
  return (
    <GridCard cardClassName="h-full pointer-events-auto outline-hidden!">
      <div className="flex h-full w-full flex-col items-center justify-center rounded-md border border-slate-800/30 dark:border-slate-300/20">
        {children}
      </div>
    </GridCard>
  );
}

interface LoadingOverlayProps {
  readonly show: boolean;
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
                {m.video_overlay_loading_stream()}
              </p>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface LoadingConnectionOverlayProps {
  readonly show: boolean;
  readonly text: string;
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
              <p className="text-center text-sm text-slate-700 dark:text-slate-300">{text}</p>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface ConnectionErrorOverlayProps {
  readonly show: boolean;
  readonly setupPeerConnection: () => Promise<void>;
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
                    <h2 className="text-xl font-bold">
                      {m.video_overlay_connection_issue_title()}
                    </h2>
                    <ul className="list-disc space-y-2 pl-4 text-left">
                      <li>{m.video_overlay_conn_verify_power()}</li>
                      <li>{m.video_overlay_conn_check_cables()}</li>
                      <li>{m.video_overlay_conn_ensure_network()}</li>
                      <li>{m.video_overlay_conn_restart()}</li>
                    </ul>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <LinkButton
                      to={"https://jetkvm.com/docs/getting-started/troubleshooting"}
                      theme="primary"
                      text={m.video_overlay_troubleshooting_guide()}
                      TrailingIcon={ArrowRightIcon}
                      size="SM"
                    />
                    <Button
                      onClick={() => setupPeerConnection()}
                      LeadingIcon={ArrowPathIcon}
                      text={m.video_overlay_try_again()}
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
  readonly show: boolean;
}

export function PeerConnectionDisconnectedOverlay({ show }: PeerConnectionDisconnectedOverlay) {
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
                    <h2 className="text-xl font-bold">
                      {m.video_overlay_connection_issue_title()}
                    </h2>
                    <ul className="list-disc space-y-2 pl-4 text-left">
                      <li>{m.video_overlay_conn_verify_power()}</li>
                      <li>{m.video_overlay_conn_check_cables()}</li>
                      <li>{m.video_overlay_conn_ensure_network()}</li>
                      <li>{m.video_overlay_conn_restart()}</li>
                    </ul>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <Card>
                      <div className="flex items-center gap-x-2 p-4">
                        <LoadingSpinner className="h-4 w-4 text-blue-800 dark:text-blue-200" />
                        <p className="text-sm text-slate-700 dark:text-slate-300">
                          {m.video_overlay_retrying_connection()}
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
  readonly show: boolean;
  readonly hdmiState: string;
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
                      <h2 className="text-xl font-bold">{m.video_overlay_no_hdmi_signal()}</h2>
                      <ul className="list-disc space-y-2 pl-4 text-left">
                        <li>{m.video_overlay_no_hdmi_ensure_cable()}</li>
                        <li>{m.video_overlay_no_hdmi_ensure_power()}</li>
                        <li>{m.video_overlay_no_hdmi_adapter_compat()}</li>
                      </ul>
                    </div>
                    <div>
                      <LinkButton
                        to={"https://jetkvm.com/docs/getting-started/troubleshooting"}
                        theme="light"
                        text={m.video_overlay_learn_more()}
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
                      <h2 className="text-xl font-bold">{m.video_overlay_hdmi_error_title()}</h2>
                      <ul className="list-disc space-y-2 pl-4 text-left">
                        <li>{m.video_overlay_hdmi_loose_faulty()}</li>
                        <li>{m.video_overlay_hdmi_incompatible_resolution()}</li>
                        <li>{m.video_overlay_hdmi_source_issue()}</li>
                      </ul>
                    </div>
                    <div>
                      <LinkButton
                        to={"https://jetkvm.com/docs/getting-started/troubleshooting"}
                        theme="light"
                        text={m.video_overlay_learn_more()}
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
  readonly show: boolean;
  readonly onPlayClick: () => void;
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
                {m.video_overlay_autoplay_permissions_required()}
              </h2>

              <div className="space-y-2 text-center">
                <div>
                  <Button
                    size="MD"
                    theme="primary"
                    LeadingIcon={LuPlay}
                    text={m.video_overlay_manually_start_stream()}
                    onClick={onPlayClick}
                  />
                </div>

                <div className="text-xs text-slate-600 dark:text-slate-400">
                  {m.video_overlay_enable_autoplay_settings()}
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
  readonly show: boolean;
}

export function PointerLockBar({ show }: PointerLockBarProps) {
  return (
    <AnimatePresence mode="wait">
      {show ? (
        <motion.div
          className="flex w-full items-center justify-between bg-transparent"
          initial={{ opacity: 0, zIndex: 0 }}
          animate={{ opacity: 1, zIndex: 20 }}
          exit={{ opacity: 0, zIndex: 0 }}
          transition={{ duration: 0.5, ease: "easeInOut", delay: 0.5 }}
        >
          <div>
            <Card className="rounded-b-none shadow-none outline-0!">
              <div className="flex items-center justify-between border border-slate-800/50 px-4 py-2 outline-0 backdrop-blur-xs dark:border-slate-300/20 dark:bg-slate-800">
                <div className="flex items-center space-x-2">
                  <BsMouseFill className="h-4 w-4 text-blue-700 dark:text-blue-500" />
                  <span className="text-sm text-black dark:text-white">
                    {m.video_overlay_pointerlock_click_to_enable()}
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

interface RebootingOverlayProps {
  readonly show: boolean;
  readonly postRebootAction: PostRebootAction;
  readonly deviceId?: string; // Required for cloud mode to build versioned URLs
}

export function RebootingOverlay({ show, postRebootAction, deviceId }: RebootingOverlayProps) {
  const { peerConnectionState } = useRTCStore();
  const [hasSeenDisconnect, setHasSeenDisconnect] = useState(
    ["disconnected", "closed", "failed"].includes(peerConnectionState ?? ""),
  );
  const [hasTimedOut, setHasTimedOut] = useState(false);
  const isCheckingRef = useRef(false);
  const abortControllerRef = useRef<AbortController | null>(null);

  // Detect connection drop (confirms reboot started)
  useEffect(() => {
    if (!show || hasSeenDisconnect) return;
    if (["disconnected", "closed", "failed"].includes(peerConnectionState ?? "")) {
      console.log("hasSeenDisconnect", hasSeenDisconnect);
      setHasSeenDisconnect(true);
    }
  }, [show, peerConnectionState, hasSeenDisconnect]);

  // Timeout after 30 seconds
  useEffect(() => {
    if (!show) {
      setHasTimedOut(false);
      return;
    }
    const id = setTimeout(() => setHasTimedOut(true), 30_000);
    return () => clearTimeout(id);
  }, [show]);

  // Redirect helper - navigates and forces reload
  const redirectTo = useCallback(async (url: string) => {
    console.log("Redirecting to", url);
    window.location.href = url;
    await sleep(1000);
    window.location.reload();
  }, []);

  // Local mode: poll HTTP health endpoint
  useEffect(() => {
    if (!isOnDevice || !postRebootAction || !show || !hasSeenDisconnect) return;

    const checkHealth = async () => {
      if (isCheckingRef.current) return;

      abortControllerRef.current?.abort();
      const controller = new AbortController();
      abortControllerRef.current = controller;
      isCheckingRef.current = true;

      console.log("Checking post-reboot health endpoint:", postRebootAction.healthCheck);
      const timeout = setTimeout(() => controller.abort(), 2000);
      try {
        // URL constructor handles relative paths, protocol-relative URLs, and absolute URLs
        // Relative path → resolves against origin
        // new URL("/device/status", "http://192.168.1.77").href
        // // → "http://192.168.1.77/device/status"

        // // Protocol-relative URL → uses protocol from base, host from URL
        // new URL("//192.168.1.100/device/status", "http://192.168.1.77").href
        // // → "http://192.168.1.100/device/status"

        // // Fully qualified URL → base is ignored entirely
        // new URL("http://192.168.1.100/device/status", "http://192.168.1.77").href
        // // → "http://192.168.1.100/device/status"
        const healthUrl = new URL(postRebootAction.healthCheck, window.location.origin).href;
        const res = await fetch(healthUrl, { signal: controller.signal });
        if (res.ok) {
          clearInterval(intervalId);
          const targetUrl = new URL(postRebootAction.redirectTo, window.location.origin).href;
          await redirectTo(targetUrl);
        }
      } catch (err) {
        if (err instanceof Error && err.name !== "AbortError") {
          console.debug("Health check failed:", err.message);
        }
      } finally {
        clearTimeout(timeout);
        isCheckingRef.current = false;
      }
    };

    const intervalId = setInterval(checkHealth, 2000);
    checkHealth();

    return () => {
      clearInterval(intervalId);
      abortControllerRef.current?.abort();
      isCheckingRef.current = false;
    };
  }, [show, postRebootAction, hasSeenDisconnect, redirectTo]);

  // Cloud mode: wait for WebRTC reconnection via RPC, then redirect with versioned URL
  useEffect(() => {
    if (isOnDevice) return;
    if (!postRebootAction || !deviceId || !show || !hasSeenDisconnect) return;

    let cancelled = false;

    const waitForReconnectAndRedirect = async () => {
      if (isCheckingRef.current) return;
      isCheckingRef.current = true;

      try {
        const { appVersion } = await getLocalVersion({
          attemptTimeoutMs: 2000,
        });

        if (cancelled) return;

        clearInterval(intervalId);
        const targetUrl = buildCloudUrl(deviceId, appVersion, postRebootAction.redirectTo);
        await redirectTo(targetUrl);
      } catch (err) {
        console.debug("Cloud reconnect check failed:", err);
        isCheckingRef.current = false;
      }
    };

    const intervalId = setInterval(waitForReconnectAndRedirect, 3000);
    waitForReconnectAndRedirect();

    return () => {
      cancelled = true;
      clearInterval(intervalId);
      isCheckingRef.current = false;
    };
  }, [show, postRebootAction, deviceId, hasSeenDisconnect, redirectTo]);

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
            <div className="flex w-full max-w-md flex-col items-start gap-y-4">
              <div className="h-[24px]">
                <img src={LogoBlue} alt="" className="h-full dark:hidden" />
                <img src={LogoWhite} alt="" className="hidden h-full dark:block" />
              </div>
              <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                <div className="space-y-4">
                  <div className="space-y-2 text-black dark:text-white">
                    <h2 className="text-xl font-bold">
                      {hasTimedOut
                        ? m.video_overlay_reboot_unable_to_reconnect()
                        : m.video_overlay_reboot_device_is_rebooting()}
                    </h2>
                    <p className="text-sm text-slate-700 dark:text-slate-300">
                      {hasTimedOut ? (
                        <>{m.video_overlay_reboot_different_ip_message()}</>
                      ) : (
                        <>{m.video_overlay_reboot_please_wait_message()}</>
                      )}
                    </p>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <Card>
                      <div className="flex items-center gap-x-2 p-4">
                        {!hasTimedOut ? (
                          <>
                            <LoadingSpinner className="h-4 w-4 text-blue-800 dark:text-blue-200" />
                            <p className="text-sm text-slate-700 dark:text-slate-300">
                              {m.video_overlay_reboot_waiting_for_restart()}
                            </p>
                          </>
                        ) : (
                          <div className="flex flex-col gap-y-2">
                            <div className="flex items-center gap-x-2">
                              <ExclamationTriangleIcon className="h-5 w-5 text-yellow-500" />
                              <p className="text-sm text-black dark:text-white">
                                {m.video_overlay_reboot_timeout_message()}
                              </p>
                            </div>
                          </div>
                        )}
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
