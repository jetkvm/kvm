import { useNavigate } from "react-router-dom";
import { GridCard } from "@/components/Card";
import { useUpdateStore } from "@/hooks/stores";
import { Dialog } from "@/components/UpdateDialog";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useCallback } from "react";

export default function UpdateRoute() {
  const navigate = useNavigate();
  const { setModalView } = useUpdateStore();
  const [send] = useJsonRpc();

  const onConfirmUpdate = useCallback(() => {
    send("tryUpdate", {});
    setModalView("updating");
  }, [send, setModalView]);

  return (
    <GridCard cardClassName="relative mx-auto max-w-md text-left pointer-events-auto">
      {/* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */}
      <Dialog
        setOpen={open => {
          if (!open) {
            navigate("..");
          }
        }}
        onConfirmUpdate={onConfirmUpdate}
      />
    </GridCard>
  );
}
