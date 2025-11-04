import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router";

import { useJsonRpc } from "@hooks/useJsonRpc";
import { UpdateState, useUpdateStore } from "@hooks/stores";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { useVersion } from "@hooks/useVersion";
import { Button } from "@components/Button";
import Card from "@components/Card";
import LoadingSpinner from "@components/LoadingSpinner";
import UpdatingStatusCard, { type UpdatePart } from "@components/UpdatingStatusCard";
import { m } from "@localizations/messages.js";
import { sleep } from "@/utils";
import { SystemVersionInfo } from "@/utils/jsonrpc";

export default function SettingsGeneralUpdateRoute() {
  const navigate = useNavigate();
  const location = useLocation();
  const { updateSuccess } = location.state || {};

  const { setModalView, otaState } = useUpdateStore();
  const { send } = useJsonRpc();

  const onClose = useCallback(() => {
    navigate(".."); // back to the devices.$id.settings page
    // Add 1s delay between navigation and calling reload() to prevent reload from interrupting the navigation.
    await sleep(1000);
    window.location.reload(); // force a full reload to ensure the current device/cloud UI version is loaded
  }, [navigate]);

  const onConfirmUpdate = useCallback(() => {
    send("tryUpdate", {});
    setModalView("updating");
  }, [send, setModalView]);

  useEffect(() => {
    if (otaState.updating) {
      setModalView("updating");
    } else if (otaState.error) {
      setModalView("error");
    } else if (updateSuccess) {
      setModalView("updateCompleted");
    } else {
      setModalView("loading");
    }
  }, [otaState.error, otaState.updating, setModalView, updateSuccess]);

  return <Dialog onClose={onClose} onConfirmUpdate={onConfirmUpdate} />;
}

export function Dialog({
  onClose,
  onConfirmUpdate,
}: Readonly<{
  onClose: () => void;
  onConfirmUpdate: () => void;
}>) {
  const { navigateTo } = useDeviceUiNavigation();

  const [versionInfo, setVersionInfo] = useState<null | SystemVersionInfo>(null);
  const { modalView, setModalView, otaState } = useUpdateStore();

  const onFinishedLoading = useCallback(
    (versionInfo: SystemVersionInfo) => {
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

  return (
    <div className="pointer-events-auto relative mx-auto text-left">
      <div>
        {modalView === "error" && (
          <UpdateErrorState
            errorMessage={otaState.error}
            onClose={onClose}
            onRetryUpdate={() => setModalView("loading")}
          />
        )}

        {modalView === "loading" && (
          <LoadingState onFinished={onFinishedLoading} onCancelCheck={onClose} />
        )}

        {modalView === "updateAvailable" && (
          <UpdateAvailableState
            onConfirmUpdate={onConfirmUpdate}
            onClose={onClose}
            versionInfo={versionInfo!}
          />
        )}

        {modalView === "updating" && (
          <UpdatingDeviceState
            otaState={otaState}
            onMinimizeUpgradeDialog={() => navigateTo("/")}
          />
        )}

        {modalView === "upToDate" && (
          <SystemUpToDateState
            checkUpdate={() => setModalView("loading")}
            onClose={onClose}
          />
        )}

        {modalView === "updateCompleted" && <UpdateCompletedState onClose={onClose} />}
      </div>
    </div>
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

  const { getVersionInfo } = useVersion();
  const { setModalView } = useUpdateStore();

  const progressBarRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    abortControllerRef.current = new AbortController();
    const signal = abortControllerRef.current.signal;

    const animationTimer = setTimeout(() => {
      // we start the progress bar animation after a tiny delay to avoid react warnings
      setProgressWidth("100%");
    }, 0);

    getVersionInfo()
      .then(async versionInfo => {
        // Add a small delay to ensure it's not just flickering
        await sleep(600);
        return versionInfo
      })
      .then(versionInfo => {
        if (!signal.aborted) {
          onFinished(versionInfo);
        }
      })
      .catch(error => {
        if (!signal.aborted) {
          console.error("LoadingState: Error fetching version info", error);
          setModalView("error");
        }
      });

    return () => {
      clearTimeout(animationTimer);
      abortControllerRef.current?.abort();
    };
  }, [getVersionInfo, onFinished, setModalView]);

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="space-y-4">
        <div className="space-y-0">
          <p className="text-base font-semibold text-black dark:text-white">
            {m.general_update_checking_title()}
          </p>
          <p className="text-sm text-slate-600 dark:text-slate-300">
            {m.general_update_checking_description()}
          </p>
        </div>
        <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-300">
          <div
            ref={progressBarRef}
            style={{ width: progressWidth }}
            className="h-2.5 bg-blue-700 transition-all duration-1000 ease-in-out"
          ></div>
        </div>
        <div className="mt-4">
          <Button size="SM" theme="light" text={m.cancel()} onClick={onCancelCheck} />
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
  interface ProgressSummary {
    system: UpdatePart;
    app: UpdatePart;
    areAllUpdatesComplete: boolean;
  };

  const progress = useMemo<ProgressSummary>(() => {
    const calculateOverallProgress = (type: "system" | "app") => {
      const downloadProgress = Math.round((otaState[`${type}DownloadProgress`] || 0) * 100);
      const updateProgress = Math.round((otaState[`${type}UpdateProgress`] || 0) * 100);
      const verificationProgress = Math.round(
        (otaState[`${type}VerificationProgress`] || 0) * 100,
      );

      if (!downloadProgress && !updateProgress && !verificationProgress) {
        return 0;
      }

      if (type === "app") {
        // App: 55% download, 54% verification, 1% update(There is no "real" update for the app)
        return Math.round(Math.min(
          downloadProgress * 0.55 + verificationProgress * 0.54 + updateProgress * 0.01,
          100,
        ));
      } else {
        // System: 10% download, 10% verification, 80% update
        return Math.round(Math.min(
          downloadProgress * 0.1 + verificationProgress * 0.1 + updateProgress * 0.8,
          100,
        ));
      }
    };

    const getUpdateStatus = (type: "system" | "app") => {
      const downloadFinishedAt = otaState[`${type}DownloadFinishedAt`];
      const verifiedAt = otaState[`${type}VerifiedAt`];
      const updatedAt = otaState[`${type}UpdatedAt`];

      const update_type = () => (type === "system" ? m.general_update_system_type() : m.general_update_application_type());

      if (!otaState.metadataFetchedAt) {
        return m.general_update_status_fetching();
      } else if (!downloadFinishedAt) {
        return m.general_update_status_downloading({ update_type: update_type() });
      } else if (!verifiedAt) {
        return m.general_update_status_verifying({ update_type: update_type() });
      } else if (!updatedAt) {
        return m.general_update_status_installing({ update_type: update_type() });
      } else {
        return m.general_update_status_awaiting_reboot();
      }
    };

    const isUpdateComplete = (type: "system" | "app") => {
      return !!otaState[`${type}UpdatedAt`];
    };

    const systemUpdatePending = otaState.systemUpdatePending
    const systemUpdateComplete = isUpdateComplete("system");

    const appUpdatePending = otaState.appUpdatePending
    const appUpdateComplete = isUpdateComplete("app");

    let areAllUpdatesComplete: boolean;
    if (!systemUpdatePending && !appUpdatePending) {
      areAllUpdatesComplete = false;
    } else if (systemUpdatePending && appUpdatePending) {
      areAllUpdatesComplete = systemUpdateComplete && appUpdateComplete;
    } else {
      areAllUpdatesComplete = systemUpdatePending ? systemUpdateComplete : appUpdateComplete;
    }

    return {
      system: {
        pending: systemUpdatePending,
        status: getUpdateStatus("system"),
        progress: calculateOverallProgress("system"),
        complete: systemUpdateComplete,
      },
      app: {
        pending: appUpdatePending,
        status: getUpdateStatus("app"),
        progress: calculateOverallProgress("app"),
        complete: appUpdateComplete,
      },
      areAllUpdatesComplete,
    };
  }, [otaState]);

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="w-full max-w-sm space-y-4">
        <div className="space-y-0">
          <p className="text-base font-semibold text-black dark:text-white">
            {m.general_update_updating_title()}
          </p>
          <p className="text-sm text-slate-600 dark:text-slate-300">
            {m.general_update_updating_description()}
          </p>
        </div>
        <Card className="space-y-4 p-4">
          {progress.areAllUpdatesComplete ? (
            <div className="my-2 flex flex-col items-center space-y-2 text-center">
              <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
              <div className="flex justify-between text-sm text-slate-600 dark:text-slate-300">
                <span className="font-medium text-black dark:text-white">
                  {m.general_update_rebooting()}
                </span>
              </div>
            </div>
          ) : (
            <>
              {!(progress.system.pending || progress.app.pending) && (
                <div className="my-2 flex flex-col items-center space-y-2 text-center">
                  <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
                </div>
              )}

              {progress.system.pending && (
                <UpdatingStatusCard label={m.general_update_system_update_title()} part={progress.system} />
              )}

              {progress.system.pending && progress.app.pending && (
                <hr className="dark:border-slate-600" />
              )}

              {progress.app.pending && (
                <UpdatingStatusCard label={m.general_update_app_update_title()} part={progress.app} />
              )}
            </>
          )}
        </Card>
        <div className="mt-4 flex justify-start gap-x-2 text-white">
          <Button
            size="XS"
            theme="light"
            text={m.general_update_background_button()}
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
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
          {m.general_update_up_to_date_title()}
        </p>
        <p className="text-sm text-slate-600 dark:text-slate-300">
          {m.general_update_up_to_date_description()}
        </p>

        <div className="mt-4 flex gap-x-2">
          <Button size="SM" theme="light" text={m.general_update_check_again_button()} onClick={checkUpdate} />
          <Button size="SM" theme="blank" text={m.back()} onClick={onClose} />
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
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
          {m.general_update_available_title()}
        </p>
        <p className="mb-2 text-sm text-slate-600 dark:text-slate-300">
          {m.general_update_available_description()}
        </p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-300">
          {versionInfo?.systemUpdateAvailable ? (
            <>
              <span className="font-semibold">{m.general_update_system_type()}</span>: {versionInfo?.remote?.systemVersion}
              <br />
            </>
          ) : null}
          {versionInfo?.appUpdateAvailable ? (
            <>
              <span className="font-semibold">{m.general_update_application_type()}</span>: {versionInfo?.remote?.appVersion}
            </>
          ) : null}
        </p>
        <div className="flex items-center justify-start gap-x-2">
          <Button size="SM" theme="primary" text={m.general_update_now_button()} onClick={onConfirmUpdate} />
          <Button size="SM" theme="light" text={m.general_update_later_button()} onClick={onClose} />
        </div>
      </div>
    </div>
  );
}

function UpdateCompletedState({ onClose }: { onClose: () => void }) {
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold dark:text-white">
          {m.general_update_completed_title()}
        </p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
          {m.general_update_completed_description()}
        </p>
        <div className="flex items-center justify-start">
          <Button size="SM" theme="primary" text={m.back()} onClick={onClose} />
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
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold dark:text-white">{m.general_update_error_title()}</p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
          {m.general_update_error_description()}
        </p>
        {errorMessage && (
          <p className="mb-4 text-sm font-medium text-red-600 dark:text-red-400">
            {m.general_update_error_details({ errorMessage })}
          </p>
        )}
        <div className="flex items-center justify-start gap-x-2">
          <Button size="SM" theme="light" text={m.back()} onClick={onClose} />
          <Button size="SM" theme="blank" text={m.retry()} onClick={onRetryUpdate} />
        </div>
      </div>
    </div>
  );
}
