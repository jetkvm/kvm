import { useTranslation } from "react-i18next";
import { cx } from "@/cva.config";

import { useDeviceUiNavigation } from "../hooks/useAppNavigation";

import { Button } from "./Button";
import { GridCard } from "./Card";
import LoadingSpinner from "./LoadingSpinner";

export default function UpdateInProgressStatusCard() {
  const { navigateTo } = useDeviceUiNavigation();
  const { t } = useTranslation();
  return (
    <div className="w-full select-none opacity-100 transition-all duration-300 ease-in-out">
      <GridCard cardClassName="shadow-xl!">
        <div className="flex items-center justify-between gap-x-3 px-2.5 py-2.5 text-black dark:text-white">
          <div className="flex items-center gap-x-3">
            <LoadingSpinner className={cx("h-5 w-5", "shrink-0 text-blue-700")} />
            <div className="space-y-1">
              <div className="text-ellipsis text-sm font-semibold leading-none transition">
                  {t('Update_in_Progress')}
              </div>
              <div className="text-sm leading-none">
                <div className="flex items-center gap-x-1">
                  <span className={cx("transition")}>
                    {t('Please_dont_turn_off_your_device')}
                  </span>
                </div>
              </div>
            </div>
          </div>
          <Button
            size="SM"
            className="pointer-events-auto"
            theme="light"
            text={t('View_Details')}
            onClick={() => navigateTo("/settings/general/update")}
          />
        </div>
      </GridCard>
    </div>
  );
}
