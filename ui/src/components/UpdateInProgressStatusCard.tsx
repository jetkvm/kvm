import { cx } from "@/cva.config";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import LoadingSpinner from "@components/LoadingSpinner";
import { m } from "@localizations/messages.js";

export default function UpdateInProgressStatusCard() {
  const { navigateTo } = useDeviceUiNavigation();

  return (
    <div className="w-full select-none opacity-100 transition-all duration-300 ease-in-out">
      <GridCard cardClassName="shadow-xl!">
        <div className="flex items-center justify-between gap-x-3 px-2.5 py-2.5 text-black dark:text-white">
          <div className="flex items-center gap-x-3">
            <LoadingSpinner className={cx("h-5 w-5", "shrink-0 text-blue-700")} />
            <div className="space-y-1">
              <div className="text-ellipsis text-sm font-semibold leading-none transition">
                {m.update_in_progress()}
              </div>
              <div className="text-sm leading-none">
                <div className="flex items-center gap-x-1">
                  <span className={cx("transition")}>
                    {m.updating_leave_device_on()}
                  </span>
                </div>
              </div>
            </div>
          </div>
          <Button
            size="SM"
            className="pointer-events-auto"
            theme="light"
            text={m.view_details()}
            onClick={() => navigateTo("/settings/general/update")}
          />
        </div>
      </GridCard>
    </div>
  );
}
