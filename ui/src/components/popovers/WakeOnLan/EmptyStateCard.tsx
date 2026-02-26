import { PlusCircleIcon } from "@heroicons/react/16/solid";
import { LuPlus } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import Card from "@components/Card";
import { Button } from "@components/Button";

export default function EmptyStateCard({
  onCancelWakeOnLanModal,
  setShowAddForm,
}: {
  onCancelWakeOnLanModal: () => void;
  setShowAddForm: (show: boolean) => void;
}) {
  return (
    <div className="space-y-4 select-none">
      <Card className="animate-fadeIn opacity-0">
        <div className="flex items-center justify-center py-8 text-center">
          <div className="space-y-3">
            <div className="space-y-1">
              <div className="inline-block">
                <Card>
                  <div className="p-1">
                    <PlusCircleIcon className="h-4 w-4 shrink-0 text-blue-700 dark:text-white" />
                  </div>
                </Card>
              </div>
              <h3 className="text-sm leading-none font-semibold text-black dark:text-white">
                {m.wake_on_lan_empty_no_devices_added()}
              </h3>
              <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                {m.wake_on_lan_empty_add_device_to_start()}
              </p>
            </div>
          </div>
        </div>
      </Card>
      <div
        className="flex animate-fadeIn items-center justify-end space-x-2 opacity-0"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        <Button size="SM" theme="blank" text={m.close()} onClick={onCancelWakeOnLanModal} />
        <Button
          size="SM"
          theme="primary"
          text={m.wake_on_lan_empty_add_new_device()}
          onClick={() => setShowAddForm(true)}
          LeadingIcon={LuPlus}
        />
      </div>
    </div>
  );
}
