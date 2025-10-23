import { useCallback } from "react";
import { useNavigate } from "react-router";

import { useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import { m } from "@localizations/messages.js";

export default function SettingsGeneralRebootRoute() {
  const navigate = useNavigate();
  const { send } = useJsonRpc();

  const onConfirmUpdate = useCallback(() => {
    send("reboot", { force: true});
  }, [send]);

  return <Dialog onClose={() => navigate("..")} onConfirmUpdate={onConfirmUpdate} />;
}

export function Dialog({
  onClose,
  onConfirmUpdate,
}: Readonly<{
  onClose: () => void;
  onConfirmUpdate: () => void;
}>) {

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
          {m.general_reboot_title()}
        </p>
        <p className="text-sm text-slate-600 dark:text-slate-300">
          {m.general_reboot_description()}
        </p>

        <div className="mt-4 flex gap-x-2">
          <Button size="SM" theme="light" text={m.general_reboot_yes_button()} onClick={onYes} />
          <Button size="SM" theme="blank" text={m.general_reboot_no_button()} onClick={onNo} />
        </div>
      </div>
    </div>
  );
}
