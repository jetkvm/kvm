import { useCallback , useState } from "react";
import { useNavigate } from "react-router";

import { useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import { useFailsafeModeStore } from "@/hooks/stores";
import { sleep } from "@/utils";
import { m } from "@localizations/messages.js";

import LoadingSpinner from "../components/LoadingSpinner";
import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

// Time to wait after initiating reboot before redirecting to home
const REBOOT_REDIRECT_DELAY_MS = 5000;

export default function SettingsGeneralRebootRoute() {
  const navigate = useNavigate();
  const { send } = useJsonRpc();
  const [isRebooting, setIsRebooting] = useState(false);
  const { navigateTo } = useDeviceUiNavigation();
  const { setFailsafeMode } = useFailsafeModeStore();
  
  const onClose = useCallback(async () => {
    navigate(".."); // back to the devices.$id.settings page
    // Add 1s delay between navigation and calling reload() to prevent reload from interrupting the navigation.
    await sleep(1000);
    window.location.reload(); // force a full reload to ensure the current device/cloud UI version is loaded
  }, [navigate]);


  const onConfirmUpdate = useCallback(async () => {
    setIsRebooting(true);
    send("reboot", {  force: true });

    await new Promise(resolve => setTimeout(resolve, REBOOT_REDIRECT_DELAY_MS));
    setFailsafeMode(false, "");
    navigateTo("/");
  }, [navigateTo, send, setFailsafeMode]);

  return <Dialog isRebooting={isRebooting} onClose={onClose} onConfirmUpdate={onConfirmUpdate} />;
}

export function Dialog({
  isRebooting,
  onClose,
  onConfirmUpdate,
}: Readonly<{
  isRebooting: boolean;
  onClose: () => void;
  onConfirmUpdate: () => void;
}>) {

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
          {m.general_reboot_title()}
        </p>
        <p className="text-sm text-slate-600 dark:text-slate-300">
          {m.general_reboot_description()}
        </p>
        {isRebooting ? (
          <div className="mt-4 flex items-center justify-center">
            <LoadingSpinner className="h-6 w-6 text-blue-700 dark:text-blue-500" />
          </div>
        ) : (
          <div className="mt-4 flex gap-x-2">
            <Button size="SM" theme="light" text={m.general_reboot_yes_button()} onClick={onYes} />
            <Button size="SM" theme="blank" text={m.general_reboot_no_button()} onClick={onNo} />
          </div>
        )}
      </div>
    </div>
  );
}
