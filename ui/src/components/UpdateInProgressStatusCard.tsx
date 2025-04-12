import { cx } from "@/cva.config";

import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

import { Button } from "./Button";
import { GridCard } from "./Card";
import LoadingSpinner from "./LoadingSpinner";

export default function UpdateInProgressStatusCard() {
  const { navigateTo } = useDeviceUiNavigation();

  return (
    <div className="w-full select-none opacity-100 transition-all duration-300 ease-in-out">
      <GridCard cardClassName="!shadow-xl">
        <div className="flex items-center justify-between gap-x-3 px-2.5 py-2.5 text-black dark:text-white">
          <div className="flex items-center gap-x-3">
            <LoadingSpinner className={cx("h-5 w-5", "shrink-0 text-blue-700")} />
            <div className="space-y-1">
              <div className="text-ellipsis text-sm font-semibold leading-none transition">
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
            onClick={() => navigateTo("/settings/general/update")}
          />
        </div>
      </GridCard>
    </div>
  );
}
