import { useNavigate } from "react-router-dom";
import { useCallback } from "react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { ConfirmDialog } from "@/components/ConfirmDialog";

export default function SettingsGeneralFactoryResetRoute() {
  const navigate = useNavigate();
  const [send] = useJsonRpc();

  const onConfirmUpdate = useCallback(() => {
    // This is where we send the RPC to the golang binary
    send("factoryReset", {});
  }, [send]);

  {
    /* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
  }
  return (
    <ConfirmDialog
      open={true}
      onClose={() => navigate("..")}
      title="Factory Reset"
      description="Do you want to proceed with factory resetting the JetKVM?"
      variant="danger"
      onConfirm={onConfirmUpdate}
    />
  );
}