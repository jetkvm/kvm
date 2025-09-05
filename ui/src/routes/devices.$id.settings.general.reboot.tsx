import { useNavigate } from "react-router";
import { useCallback } from "react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";

export default function SettingsGeneralRebootRoute() {
  const navigate = useNavigate();
  const { send } = useJsonRpc();

  const onConfirmUpdate = useCallback(() => {
    // This is where we send the RPC to the golang binary
    send("reboot", {force: true});
  }, [send]);

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

  return (
    <div className="pointer-events-auto relative mx-auto text-left">
      <div>
          <ConfirmationBox
            onYes={onConfirmUpdate}
            onNo={onClose}
          />
      </div>
    </div>
  );
}

function ConfirmationBox({
  onYes,
  onNo,
}: {
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

        <div className="mt-4 flex gap-x-2">
          <Button size="SM" theme="light" text="Yes" onClick={onYes} />
          <Button size="SM" theme="blank" text="No" onClick={onNo} />
        </div>
      </div>
    </div>
  );
}
