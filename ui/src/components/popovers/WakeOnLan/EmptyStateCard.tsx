import Card from "@components/Card";
import { PlusCircleIcon } from "@heroicons/react/16/solid";
import { LuPlus } from "react-icons/lu";
import { Button } from "../../Button";

export default function EmptyStateCard({
  onCancelWakeOnLanModal,
  setShowAddForm,
}: {
  onCancelWakeOnLanModal: () => void;
  setShowAddForm: (show: boolean) => void;
}) {
  return (
    <div className="space-y-4 select-none">
      <Card className="opacity-0 animate-fadeIn">
        <div className="flex items-center justify-center py-8 text-center">
          <div className="space-y-3">
            <div className="space-y-1">
              <div className="inline-block">
                <Card>
                  <div className="p-1">
                    <PlusCircleIcon className="w-4 h-4 text-blue-700 shrink-0 dark:text-white" />
                  </div>
                </Card>
              </div>
              <h3 className="text-sm font-semibold leading-none text-black dark:text-white">
                No devices added
              </h3>
              <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                Add a device to start using Wake-on-LAN
              </p>
            </div>
          </div>
        </div>
      </Card>
      <div
        className="flex items-center justify-end space-x-2 opacity-0 animate-fadeIn"
        style={{
          animationDuration: "0.7s",
          animationDelay: "0.2s",
        }}
      >
        <Button size="SM" theme="blank" text="Close" onClick={onCancelWakeOnLanModal} />
        <Button
          size="SM"
          theme="primary"
          text="Add New Device"
          onClick={() => setShowAddForm(true)}
          LeadingIcon={LuPlus}
        />
      </div>
    </div>
  );
}
