import { useState } from "react";
import { ExclamationTriangleIcon } from "@heroicons/react/24/solid";
import { motion, AnimatePresence } from "framer-motion";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { GitHubIcon } from "@components/Icons";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useVersion } from "@hooks/useVersion";
import { useDeviceStore } from "@hooks/stores";
import notifications from "@/notifications";
import { DOWNGRADE_VERSION } from "@/ui.config";
import { callJsonRpc } from "@/utils/jsonrpc";

interface FailSafeModeOverlayProps {
  reason: string;
}

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

export function FailSafeModeOverlay({ reason }: FailSafeModeOverlayProps) {
  const { navigateTo } = useDeviceUiNavigation();
  const { appVersion } = useVersion();
  const { systemVersion } = useDeviceStore();
  const [isDownloadingLogs, setIsDownloadingLogs] = useState(false);

  const getReasonCopy = () => {
    switch (reason) {
      case "video":
        return {
          message:
            "We've detected an issue with the video capture process. Your device is still running and accessible, but video streaming is temporarily unavailable.",
        };
      default:
        return {
          message:
            "A critical process has encountered an issue. Your device is still accessible, but some functionality may be temporarily unavailable.",
        };
    }
  };

  const { message } = getReasonCopy();

  const handleReportAndDownloadLogs = async () => {
    setIsDownloadingLogs(true);

    try {
      const response = await callJsonRpc<string>({
        method: "getDiagnostics",
        attemptTimeoutMs: 20000, // 20s - diagnostics collects a lot of data
        maxAttempts: 1,
      });

      if (response.error) {
        notifications.error(`Failed to get diagnostics: ${response.error.message}`);
        return;
      }

      // Download logs
      const logContent = response.result;
      const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
      const filename = `jetkvm-recovery-${reason}-${timestamp}.txt`;

      const blob = new Blob([logContent], { type: "text/plain" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      await new Promise(resolve => setTimeout(resolve, 1000));
      a.click();
      await new Promise(resolve => setTimeout(resolve, 1000));
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      notifications.success("Crash logs downloaded successfully");

      // Open GitHub issue
      const issueBody = `## Issue Description
The \`${reason}\` process encountered an error and failsafe mode was activated.

**Reason:** \`${reason}\`
**Timestamp:** ${new Date().toISOString()}
**App Version:** ${appVersion || "Unknown"}
**System Version:** ${systemVersion || "Unknown"}

## Logs
Please attach the recovery logs file that was downloaded to your computer:
\`${filename}\`

> [!NOTE]
> Please remove any sensitive information from the logs. The reports are public and can be viewed by anyone.

## Additional Context
[Please describe what you were doing when this occurred]`;

      const issueUrl =
        `https://github.com/jetkvm/kvm/issues/new?` +
        `title=${encodeURIComponent(`Recovery Mode: ${reason} process issue`)}&` +
        `body=${encodeURIComponent(issueBody)}`;

      window.open(issueUrl, "_blank");
    } catch (error) {
      notifications.error(
        `Failed to get diagnostics: ${error instanceof Error ? error.message : "Request timed out"}`,
      );
    } finally {
      setIsDownloadingLogs(false);
    }
  };

  const handleDowngrade = () => {
    navigateTo(`/settings/general/update?custom_app_version=${DOWNGRADE_VERSION}`);
  };

  return (
    <AnimatePresence>
      <motion.div
        className="isolate aspect-video h-full w-full"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0, transition: { duration: 0 } }}
        transition={{
          duration: 0.4,
          ease: "easeInOut",
        }}
      >
        <OverlayContent>
          <div className="flex max-w-lg flex-col items-start gap-y-1">
            <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
            <div className="text-left text-sm text-slate-700 dark:text-slate-300">
              <div className="space-y-4">
                <div className="space-y-2 text-black dark:text-white">
                  <h2 className="text-xl font-bold">Fail safe mode activated</h2>
                  <p className="text-sm">{message}</p>
                </div>
                <div className="space-y-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      onClick={handleReportAndDownloadLogs}
                      theme="primary"
                      size="SM"
                      disabled={isDownloadingLogs}
                      LeadingIcon={GitHubIcon}
                      loading={isDownloadingLogs}
                      text={
                        isDownloadingLogs ? "Downloading Logs..." : "Download Logs & Report Issue"
                      }
                    />

                    <div className="block h-8 w-px bg-slate-200 dark:bg-slate-700" />

                    <Button
                      onClick={() => navigateTo("/settings/general/reboot")}
                      theme="light"
                      size="SM"
                      text="Reboot Device"
                    />

                    <Button
                      size="SM"
                      onClick={handleDowngrade}
                      theme="light"
                      text={`Downgrade to v${DOWNGRADE_VERSION}`}
                    />
                  </div>
                </div>
              </div>
            </div>
          </div>
        </OverlayContent>
      </motion.div>
    </AnimatePresence>
  );
}
