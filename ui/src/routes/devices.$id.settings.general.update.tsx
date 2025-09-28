import { useLocation, useNavigate } from "react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircleIcon } from "@heroicons/react/20/solid";

import Card from "@/components/Card";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";
import { UpdateState, useUpdateStore } from "@/hooks/stores";
import LoadingSpinner from "@/components/LoadingSpinner";
import { useDeviceUiNavigation } from "@/hooks/useAppNavigation";
import { SystemVersionInfo, useVersion } from "@/hooks/useVersion";

export default function SettingsGeneralUpdateRoute() {
  const navigate = useNavigate();
  const location = useLocation();
  const { updateSuccess } = location.state || {};

  const { setModalView, otaState } = useUpdateStore();
  const { send } = useJsonRpc();

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
  }, [otaState.updating, otaState.error, setModalView, updateSuccess]);

  {
    /* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
  }
  return <Dialog onClose={() => navigate("..")} onConfirmUpdate={onConfirmUpdate} />;
}



export function Dialog({
  onClose,
  onConfirmUpdate,
}: {
  onClose: () => void;
  onConfirmUpdate: () => void;
}) {
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

  // Reset modal view when dialog is opened
  useEffect(() => {
    setVersionInfo(null);
  }, [setModalView]);

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

  const progressBarRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setProgressWidth("0%");

    abortControllerRef.current = new AbortController();
    const signal = abortControllerRef.current.signal;

    const animationTimer = setTimeout(() => {
      setProgressWidth("100%");
    }, 0);

    getVersionInfo()
      .then(versionInfo => {
        // Add a small delay to ensure it's not just flickering
        return new Promise(resolve => setTimeout(() => resolve(versionInfo), 600));
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
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="space-y-4">
        <div className="space-y-0">
          <p className="text-base font-semibold text-black dark:text-white">
              {t('Checking_for_updates')}
          </p>
          <p className="text-sm text-slate-600 dark:text-slate-300">
              {t('We_re_ensuring_your_device_has_the_latest_features_and_improvements')}
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
          <Button size="SM" theme="light" text={t('Cancel')} onClick={onCancelCheck} />
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
  const { t } = useTranslation();
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
      return t('Fetching_update_information');
    } else if (!downloadFinishedAt) {
      return t('Downloading_type_update',{type:t(type)});
    } else if (!verfiedAt) {
      return t('Verifying_type_update',{type:t(type)});
    } else if (!updatedAt) {
      return t('Installing_type_update',{type:t(type)});
    } else {
      return t('Awaiting_reboot');
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
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="w-full max-w-sm space-y-4">
        <div className="space-y-0">
          <p className="text-base font-semibold text-black dark:text-white">
              {t('Updating_your_device')}
          </p>
          <p className="text-sm text-slate-600 dark:text-slate-300">
              {t('Please_dont_turn_off_your_device_This_process_may_take_a_few_minutes')}
          </p>
        </div>
        <Card className="space-y-4 p-4">
          {areAllUpdatesComplete() ? (
            <div className="my-2 flex flex-col items-center space-y-2 text-center">
              <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
              <div className="flex justify-between text-sm text-slate-600 dark:text-slate-300">
                <span className="font-medium text-black dark:text-white">
                  {t('Rebooting_to_complete_the_update')}
                </span>
              </div>
            </div>
          ) : (
            <>
              {!(otaState.systemUpdatePending || otaState.appUpdatePending) && (
                <div className="my-2 flex flex-col items-center space-y-2 text-center">
                  <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
                </div>
              )}

              {otaState.systemUpdatePending && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <p className="text-sm font-semibold text-black dark:text-white">
                        {t('Linux_System_Update')}
                    </p>
                    {calculateOverallProgress("system") < 100 ? (
                      <LoadingSpinner className="h-4 w-4 text-blue-700 dark:text-blue-500" />
                    ) : (
                      <CheckCircleIcon className="h-4 w-4 text-blue-700 dark:text-blue-500" />
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
                          {t('App_Update')}
                      </p>
                      {calculateOverallProgress("app") < 100 ? (
                        <LoadingSpinner className="h-4 w-4 text-blue-700 dark:text-blue-500" />
                      ) : (
                        <CheckCircleIcon className="h-4 w-4 text-blue-700 dark:text-blue-500" />
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
        <div className="mt-4 flex justify-start gap-x-2 text-white">
          <Button
            size="XS"
            theme="light"
            text={t('Update_in_Background')}
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
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
            {t('System_is_up_to_date')}
        </p>
        <p className="text-sm text-slate-600 dark:text-slate-300">
            {t('Your_system_is_running_the_latest_version_No_updates_are_currently_available')}
        </p>

        <div className="mt-4 flex gap-x-2">
          <Button size="SM" theme="light" text={t('Check_Again')} onClick={checkUpdate} />
          <Button size="SM" theme="blank" text={t('Back')} onClick={onClose} />
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
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
            {t('Update_available')}
        </p>
        <p className="mb-2 text-sm text-slate-600 dark:text-slate-300">
            {t('A_new_update_is_available_to_enhance_system_performance_and_improve_compatibility_We_recommend_updating_to_ensure_everything_runs_smoothly')}
        </p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-300">
          {versionInfo?.systemUpdateAvailable ? (
            <>
              <span className="font-semibold">{t('System')}:</span>{" "}
              {versionInfo?.remote?.systemVersion}
              <br />
            </>
          ) : null}
          {versionInfo?.appUpdateAvailable ? (
            <>
              <span className="font-semibold">{t('App')}:</span>{" "}
              {versionInfo?.remote?.appVersion}
            </>
          ) : null}
        </p>
        <div className="flex items-center justify-start gap-x-2">
          <Button size="SM" theme="primary" text={t('Update_Now')} onClick={onConfirmUpdate} />
          <Button size="SM" theme="light" text={t('Do_it_later')} onClick={onClose} />
        </div>
      </div>
    </div>
  );
}

function UpdateCompletedState({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold dark:text-white">
            {t('Update_Completed_Successfully')}
        </p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
            {t('Your_device_has_been_successfully_updated_to_the_latest_version_Enjoy_the_new_features_and_improvements')}
        </p>
        <div className="flex items-center justify-start">
          <Button size="SM" theme="primary" text={t('Back')} onClick={onClose} />
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
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold dark:text-white">{t('Update_Error')}</p>
        <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
            {t('An_error_occurred_while_updating_your_device_Please_try_again_later')}
        </p>
        {errorMessage && (
          <p className="mb-4 text-sm font-medium text-red-600 dark:text-red-400">
              {t('Error_details_msg',{msg:errorMessage})}
          </p>
        )}
        <div className="flex items-center justify-start gap-x-2">
          <Button size="SM" theme="light" text={t('Back')} onClick={onClose} />
          <Button size="SM" theme="blank" text={t('Retry')} onClick={onRetryUpdate} />
        </div>
      </div>
    </div>
  );
}
