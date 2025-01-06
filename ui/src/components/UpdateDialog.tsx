import Card, { GridCard } from "@/components/Card";
import { useCallback, useEffect, useRef, useState } from "react";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import Modal from "@components/Modal";
import { UpdateState, useUpdateStore } from "@/hooks/stores";
import notifications from "@/notifications";
import { CheckCircleIcon } from "@heroicons/react/20/solid";
import LoadingSpinner from "./LoadingSpinner";

export interface SystemVersionInfo {
  local: { appVersion: string; systemVersion: string };
  remote: { appVersion: string; systemVersion: string };
  systemUpdateAvailable: boolean;
  appUpdateAvailable: boolean;
}

export default function UpdateDialog({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) {
  // We need to keep track of the update state in the dialog even if the dialog is minimized
  const { setModalView } = useUpdateStore();

  const [send] = useJsonRpc();

  const onConfirmUpdate = useCallback(() => {
    send("tryUpdate", {});
    setModalView("updating");
  }, [send, setModalView]);

  return (
    <Modal open={open} onClose={() => setOpen(false)}>
      <Dialog setOpen={setOpen} onConfirmUpdate={onConfirmUpdate} />
    </Modal>
  );
}

export function Dialog({
  setOpen,
  onConfirmUpdate,
}: {
  setOpen: (open: boolean) => void;
  onConfirmUpdate: () => void;
}) {
  const [versionInfo, setVersionInfo] = useState<null | SystemVersionInfo>(null);
  const { modalView, setModalView, otaState } = useUpdateStore();

  const onFinishedLoading = useCallback(
    async (versionInfo: SystemVersionInfo) => {
      const hasUpdate =
        versionInfo?.systemUpdateAvailable || versionInfo?.appUpdateAvailable;

      setVersionInfo(versionInfo);

      if (hasUpdate) {
        setModalView("updateAvailable");
      } else {
        setModalView("upToDate");
      }
    },
    [setModalView],
  );

  // Reset modal view when dialog is opened
  useEffect(() => {
    setVersionInfo(null);
  }, [setModalView]);

  return (
    <GridCard cardClassName="mx-auto relative max-w-md text-left pointer-events-auto">
      <div className="p-10">
        {modalView === "error" && (
          <UpdateErrorState
            errorMessage={otaState.error}
            onClose={() => setOpen(false)}
            onRetryUpdate={() => setModalView("loading")}
          />
        )}

        {modalView === "loading" && (
          <LoadingState
            onFinished={onFinishedLoading}
            onCancelCheck={() => setOpen(false)}
          />
        )}

        {modalView === "updateAvailable" && (
          <UpdateAvailableState
            onConfirmUpdate={onConfirmUpdate}
            onClose={() => setOpen(false)}
            versionInfo={versionInfo!}
          />
        )}

        {modalView === "updating" && (
          <UpdatingDeviceState
            otaState={otaState}
            onMinimizeUpgradeDialog={() => {
              setOpen(false);
            }}
          />
        )}

        {modalView === "upToDate" && (
          <SystemUpToDateState
            checkUpdate={() => setModalView("loading")}
            onClose={() => setOpen(false)}
          />
        )}

        {modalView === "updateCompleted" && (
          <UpdateCompletedState onClose={() => setOpen(false)} />
        )}
      </div>
    </GridCard>
  );
}

function LoadingState({
  onFinished,
  onCancelCheck,
}: {
  onFinished: (versionInfo: SystemVersionInfo) => void;
  onCancelCheck: () => void;
}) {
  const [progressWidth, setProgressWidth] = useState("0%");
  const abortControllerRef = useRef<AbortController | null>(null);
  const [send] = useJsonRpc();

  const getVersionInfo = useCallback(() => {
    return new Promise<SystemVersionInfo>((resolve, reject) => {
      send("getUpdateStatus", {}, async resp => {
        if ("error" in resp) {
          notifications.error("Failed to check for updates");
          reject(new Error("Failed to check for updates"));
        } else {
          const result = resp.result as SystemVersionInfo;
          resolve(result);
        }
      });
    });
  }, [send]);

  const progressBarRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setProgressWidth("0%");

    abortControllerRef.current = new AbortController();
    const signal = abortControllerRef.current.signal;

    const animationTimer = setTimeout(() => {
      setProgressWidth("100%");
    }, 500);

    getVersionInfo()
      .then(versionInfo => {
        if (progressBarRef.current) {
          progressBarRef.current?.classList.add("!duration-1000");
        }
        return new Promise(resolve => setTimeout(() => resolve(versionInfo), 1000));
      })
      .then(versionInfo => {
        if (!signal.aborted) {
          onFinished(versionInfo as SystemVersionInfo);
        }
      })
      .catch(error => {
        if (!signal.aborted) {
          console.error("LoadingState: Error fetching version info", error);
        }
      });

    return () => {
      clearTimeout(animationTimer);
      abortControllerRef.current?.abort();
    };
  }, [getVersionInfo, onFinished]);

  return (
    <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
        <img src={LogoWhiteIcon} alt="" className="mt-0 hidden h-[24px] dark:block" />
      </div>
      <div className="max-w-sm space-y-4">
        <div className="space-y-0">
          <p className="text-base font-semibold text-black dark:text-white">
            Checking for updates...
          </p>
          <p className="text-sm text-slate-600 dark:text-slate-300">
            We{"'"}re ensuring your device has the latest features and improvements.
          </p>
        </div>
        <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-300">
          <div
            ref={progressBarRef}
            style={{ width: progressWidth }}
            className="h-2.5 bg-blue-700 transition-all duration-[4s] ease-in-out"
          ></div>
        </div>
        <div className="mt-4">
          <Button
            size="SM"
            theme="light"
            text="Cancel"
            onClick={() => {
              onCancelCheck();
            }}
          />
        </div>
      </div>
    </div>
  );
}

function UpdatingDeviceState({
  otaState,
  onMinimizeUpgradeDialog,
}: {
  otaState: UpdateState["otaState"];
  onMinimizeUpgradeDialog: () => void;
}) {
  const formatProgress = (progress: number) => `${Math.round(progress)}%`;

  const calculateOverallProgress = (type: "system" | "app") => {
    const downloadProgress = Math.round((otaState[`${type}DownloadProgress`] || 0) * 100);
    const updateProgress = Math.round((otaState[`${type}UpdateProgress`] || 0) * 100);
    const verificationProgress = Math.round(
      (otaState[`${type}VerificationProgress`] || 0) * 100,
    );

    if (!downloadProgress && !updateProgress && !verificationProgress) {
      return 0;
    }

    console.log(
      `For ${type}:\n` +
        `  Download Progress: ${downloadProgress}% (${otaState[`${type}DownloadProgress`]})\n` +
        `  Update Progress: ${updateProgress}% (${otaState[`${type}UpdateProgress`]})\n` +
        `  Verification Progress: ${verificationProgress}% (${otaState[`${type}VerificationProgress`]})`,
    );

    if (type === "app") {
      // App: 65% download, 34% verification, 1% update(There is no "real" update for the app)
      return Math.min(
        downloadProgress * 0.55 + verificationProgress * 0.54 + updateProgress * 0.01,
        100,
      );
    } else {
      // System: 10% download, 90% update
      return Math.min(
        downloadProgress * 0.1 + verificationProgress * 0.1 + updateProgress * 0.8,
        100,
      );
    }
  };

  const getUpdateStatus = (type: "system" | "app") => {
    const downloadFinishedAt = otaState[`${type}DownloadFinishedAt`];
    const verfiedAt = otaState[`${type}VerifiedAt`];
    const updatedAt = otaState[`${type}UpdatedAt`];

    if (!otaState.metadataFetchedAt) {
      return "Fetching update information...";
    } else if (!downloadFinishedAt) {
      return `Downloading ${type} update...`;
    } else if (!verfiedAt) {
      return `Verifying ${type} update...`;
    } else if (!updatedAt) {
      return `Installing ${type} update...`;
    } else {
      return `Awaiting reboot`;
    }
  };

  const isUpdateComplete = (type: "system" | "app") => {
    return !!otaState[`${type}UpdatedAt`];
  };

  const areAllUpdatesComplete = () => {
    if (otaState.systemUpdatePending && otaState.appUpdatePending) {
      return isUpdateComplete("system") && isUpdateComplete("app");
    }
    return (
      (otaState.systemUpdatePending && isUpdateComplete("system")) ||
      (otaState.appUpdatePending && isUpdateComplete("app"))
    );
  };

  return (
    <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
        <img src={LogoWhiteIcon} alt="" className="mt-0 hidden h-[24px] dark:block" />
      </div>
      <div className="w-full max-w-sm space-y-4">
        <div className="space-y-0">
          <p className="text-base font-semibold text-black dark:text-white">
            Updating your device
          </p>
          <p className="text-sm text-slate-600 dark:text-slate-300">
            Please don{"'"}t turn off your device. This process may take a few minutes.
          </p>
        </div>
        <Card className="p-4 space-y-4">
          {areAllUpdatesComplete() ? (
            <div className="flex flex-col items-center my-2 space-y-2 text-center">
              <LoadingSpinner className="w-6 h-6 text-blue-700 dark:text-blue-500" />
              <div className="flex justify-between text-sm text-slate-600 dark:text-slate-300">
                <span className="font-medium text-black dark:text-white">
                  Rebooting to complete the update...
                </span>
              </div>
            </div>
          ) : (
            <>
              {!(otaState.systemUpdatePending || otaState.appUpdatePending) && (
                <div className="flex flex-col items-center my-2 space-y-2 text-center">
                  <LoadingSpinner className="w-6 h-6 text-blue-700 dark:text-blue-500" />
                </div>
              )}

              {otaState.systemUpdatePending && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <p className="text-sm font-semibold text-black dark:text-white">
                      Linux System Update
                    </p>
                    {calculateOverallProgress("system") < 100 ? (
                      <LoadingSpinner className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                    ) : (
                      <CheckCircleIcon className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                    )}
                  </div>
                  <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-300 dark:bg-slate-600">
                    <div
                      className="h-2.5 rounded-full bg-blue-700 transition-all duration-500 ease-linear dark:bg-blue-500"
                      style={{
                        width: formatProgress(calculateOverallProgress("system")),
                      }}
                    ></div>
                  </div>
                  <div className="flex justify-between text-sm text-slate-600 dark:text-slate-300">
                    <span>{getUpdateStatus("system")}</span>
                    {calculateOverallProgress("system") < 100 ? (
                      <span>{formatProgress(calculateOverallProgress("system"))}</span>
                    ) : null}
                  </div>
                </div>
              )}
              {otaState.appUpdatePending && (
                <>
                  {otaState.systemUpdatePending && (
                    <hr className="dark:border-slate-600" />
                  )}
                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <p className="text-sm font-semibold text-black dark:text-white">
                        App Update
                      </p>
                      {calculateOverallProgress("app") < 100 ? (
                        <LoadingSpinner className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                      ) : (
                        <CheckCircleIcon className="w-4 h-4 text-blue-700 dark:text-blue-500" />
                      )}
                    </div>
                    <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-300 dark:bg-slate-600">
                      <div
                        className="h-2.5 rounded-full bg-blue-700 transition-all duration-500 ease-linear dark:bg-blue-500"
                        style={{
                          width: formatProgress(calculateOverallProgress("app")),
                        }}
                      ></div>
                    </div>
                    <div className="flex justify-between text-sm text-slate-600 dark:text-slate-300">
                      <span>{getUpdateStatus("app")}</span>
                      {calculateOverallProgress("system") < 100 ? (
                        <span>{formatProgress(calculateOverallProgress("app"))}</span>
                      ) : null}
                    </div>
                  </div>
                </>
              )}
            </>
          )}
        </Card>
        <div className="flex justify-start mt-4 text-white gap-x-2">
          <Button
            size="XS"
            theme="light"
            text="Update in Background"
            onClick={onMinimizeUpgradeDialog}
          />
        </div>
      </div>
    </div>
  );
}

function SystemUpToDateState({
  checkUpdate,
  onClose,
}: {
  checkUpdate: () => void;
  onClose: () => void;
}) {
  return (
    <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
        <img src={LogoWhiteIcon} alt="" className="mt-0 hidden h-[24px] dark:block" />
      </div>
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
          System is up to date
        </p>
        <p className="text-sm text-slate-600 dark:text-slate-300">
          Your system is running the latest version. No updates are currently available.
        </p>

        <div className="flex mt-4 gap-x-2">
          <Button
            size="SM"
            theme="light"
            text="Check Again"
            onClick={() => {
              checkUpdate();
            }}
          />
          <Button
            size="SM"
            theme="blank"
            text="Close"
            onClick={() => {
              onClose();
            }}
          />
        </div>
      </div>
    </div>
  );
}

function UpdateAvailableState({
  versionInfo,
  onConfirmUpdate,
  onClose,
}: {
  versionInfo: SystemVersionInfo;
  onConfirmUpdate: () => void;
  onClose: () => void;
}) {
  return (
    <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
        <img src={LogoWhiteIcon} alt="" className="mt-0 hidden h-[24px] dark:block" />
      </div>
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
          Update available
        </p>
        <p className="mb-2 text-sm text-slate-600 dark:text-slate-300">
          A new update is available to enhance system performance and improve
          compatibility. We recommend updating to ensure everything runs smoothly.
        </p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-300">
          {versionInfo?.systemUpdateAvailable ? (
            <>
              <span className="font-semibold">System:</span>{" "}
              {versionInfo?.remote.systemVersion}
              <br />
            </>
          ) : null}
          {versionInfo?.appUpdateAvailable ? (
            <>
              <span className="font-semibold">App:</span> {versionInfo?.remote.appVersion}
            </>
          ) : null}
        </p>
        <div className="flex items-center justify-start gap-x-2">
          <Button size="SM" theme="primary" text="Update Now" onClick={onConfirmUpdate} />
          <Button size="SM" theme="light" text="Do it later" onClick={onClose} />
        </div>
      </div>
    </div>
  );
}

function UpdateCompletedState({ onClose }: { onClose: () => void }) {
  return (
    <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
        <img src={LogoWhiteIcon} alt="" className="mt-0 hidden h-[24px] dark:block" />
      </div>
      <div className="text-left">
        <p className="text-base font-semibold dark:text-white">
          Update Completed Successfully
        </p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
          Your device has been successfully updated to the latest version. Enjoy the new
          features and improvements!
        </p>
        <div className="flex items-center justify-start">
          <Button size="SM" theme="primary" text="Close" onClick={onClose} />
        </div>
      </div>
    </div>
  );
}

function UpdateErrorState({
  errorMessage,
  onClose,
  onRetryUpdate,
}: {
  errorMessage: string | null;
  onClose: () => void;
  onRetryUpdate: () => void;
}) {
  return (
    <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
      <div>
        <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
        <img src={LogoWhiteIcon} alt="" className="mt-0 hidden h-[24px] dark:block" />
      </div>
      <div className="text-left">
        <p className="text-base font-semibold dark:text-white">Update Error</p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
          An error occurred while updating your device. Please try again later.
        </p>
        {errorMessage && (
          <p className="mb-4 text-sm font-medium text-red-600 dark:text-red-400">
            Error details: {errorMessage}
          </p>
        )}
        <div className="flex items-center justify-start gap-x-2">
          <Button size="SM" theme="primary" text="Close" onClick={onClose} />
          <Button size="SM" theme="primary" text="Retry" onClick={onRetryUpdate} />
        </div>
      </div>
    </div>
  );
}
