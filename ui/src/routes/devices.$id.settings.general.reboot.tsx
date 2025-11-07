import { useNavigate } from "react-router";
import { useCallback, useState } from "react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";

import LoadingSpinner from "../components/LoadingSpinner";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

export default function SettingsGeneralRebootRoute() {
  const navigate = useNavigate();
  const { send } = useJsonRpc();
  const [isRebooting, setIsRebooting] = useState(false);
  const { navigateTo } = useDeviceUiNavigation();

  const onConfirmUpdate = useCallback(async () => {
    setIsRebooting(true);
    // This is where we send the RPC to the golang binary
    send("reboot", { force: true });

    await new Promise(resolve => setTimeout(resolve, 5000));
    navigateTo("/");
  }, [navigateTo, send]);

  {
    /* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
  }
  return <Dialog isRebooting={isRebooting} onClose={() => navigate("..")} onConfirmUpdate={onConfirmUpdate} />;
}

export function Dialog({
  isRebooting,
  onClose,
  onConfirmUpdate,
}: {
  isRebooting: boolean;
  onClose: () => void;
  onConfirmUpdate: () => void;
}) {

  return (
    <div className="pointer-events-auto relative mx-auto text-left">
      <div>
        <ConfirmationBox
          isRebooting={isRebooting}
          onYes={onConfirmUpdate}
          onNo={onClose}
        />
      </div>
    </div>
  );
}

function ConfirmationBox({
  isRebooting,
  onYes,
  onNo,
}: {
  isRebooting: boolean;
  onYes: () => void;
  onNo: () => void;
}) {
  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="text-left">
        <p className="text-base font-semibold text-black dark:text-white">
          Reboot JetKVM
        </p>
        <p className="text-sm text-slate-600 dark:text-slate-300">
          Do you want to proceed with rebooting the system?
        </p>
        {isRebooting ? (
          <div className="mt-4 flex items-center justify-center">
            <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
          </div>
        ) : (
          <div className="mt-4 flex gap-x-2">
            <Button size="SM" theme="light" text="Yes" onClick={onYes} />
            <Button size="SM" theme="blank" text="No" onClick={onNo} />
          </div>
        )}
      </div>
    </div>
  );
}
