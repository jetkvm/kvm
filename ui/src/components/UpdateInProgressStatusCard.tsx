import { cx } from "@/cva.config";
import { Button } from "./Button";
import { GridCard } from "./Card";
import LoadingSpinner from "./LoadingSpinner";
import { UpdateState } from "@/hooks/stores";

interface UpdateInProgressStatusCardProps {
  setIsUpdateDialogOpen: (isOpen: boolean) => void;
  setModalView: (view: UpdateState["modalView"]) => void;
}

export default function UpdateInProgressStatusCard({
  setIsUpdateDialogOpen,
  setModalView,
}: UpdateInProgressStatusCardProps) {
  return (
    <div className="w-full transition-all duration-300 ease-in-out opacity-100 select-none">
      <GridCard cardClassName="!shadow-xl">
        <div className="flex items-center justify-between gap-x-3 px-2.5 py-2.5 text-black dark:text-white">
          <div className="flex items-center gap-x-3">
            <LoadingSpinner className={cx("h-5 w-5", "shrink-0 text-blue-700")} />
            <div className="space-y-1">
              <div className="text-sm font-semibold leading-none transition text-ellipsis">
                Update in Progress
              </div>
              <div className="text-sm leading-none">
                <div className="flex items-center gap-x-1">
                  <span className={cx("transition")}>
                    Please don{"'"}t turn off your device...
                  </span>
                </div>
              </div>
            </div>
          </div>
          <Button
            size="SM"
            className="pointer-events-auto"
            theme="light"
            text="View Details"
            onClick={() => {
              setModalView("updating");
              setIsUpdateDialogOpen(true);
            }}
          />
        </div>
      </GridCard>
    </div>
  );
}
