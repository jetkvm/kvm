import { useEffect, useState } from "react";
import { LuPower, LuTerminal, LuPlugZap } from "react-icons/lu";

import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import Card, { GridCard } from "@components/Card";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { ATXPowerControl } from "@components/extensions/ATXPowerControl";
import { DCPowerControl } from "@components/extensions/DCPowerControl";
import { SerialConsole } from "@components/extensions/SerialConsole";
import { Button } from "@components/Button";
import notifications from "@/notifications";

interface Extension {
  id: string;
  name: string;
  description: string;
  icon: React.ElementType;
}

const AVAILABLE_EXTENSIONS: Extension[] = [
  {
    id: "atx-power",
    name: "ATX Power Control",
    description: "Control your ATX Power extension",
    icon: LuPower,
  },
  {
    id: "dc-power",
    name: "DC Power Control",
    description: "Control your DC Power extension",
    icon: LuPlugZap,
  },
  {
    id: "serial-console",
    name: "Serial Console",
    description: "Access your serial console extension",
    icon: LuTerminal,
  },
];

export default function ExtensionPopover() {
  const { send } = useJsonRpc();
  const [activeExtension, setActiveExtension] = useState<Extension | null>(null);

  // Load active extension on component mount
  useEffect(() => {
    send("getActiveExtension", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const extensionId = resp.result as string;
      if (extensionId) {
        const extension = AVAILABLE_EXTENSIONS.find(ext => ext.id === extensionId);
        if (extension) {
          setActiveExtension(extension);
        }
      }
    });
  }, [send]);

  const handleSetActiveExtension = (extension: Extension | null) => {
    send("setActiveExtension", { extensionId: extension?.id || "" }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set active extension: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setActiveExtension(extension);
    });
  };

  const renderActiveExtension = () => {
    switch (activeExtension?.id) {
      case "atx-power":
        return <ATXPowerControl />;
      case "dc-power":
        return <DCPowerControl />;
      case "serial-console":
        return <SerialConsole />;
      default:
        return null;
    }
  };

  return (
    <GridCard>
      <div className="space-y-4 p-4 py-3">
        <div className="grid h-full grid-rows-(--grid-headerBody)">
          <div className="space-y-4">
            {activeExtension ? (
              // Extension Control View
              <div className="space-y-4">
                {renderActiveExtension()}

                <div
                  className="flex animate-fadeIn opacity-0 items-center justify-end space-x-2"
                  style={{
                    animationDuration: "0.7s",
                    animationDelay: "0.2s",
                  }}
                >
                  <Button
                    size="SM"
                    theme="light"
                    text="Unload Extension"
                    onClick={() => handleSetActiveExtension(null)}
                  />
                </div>
              </div>
            ) : (
              // Extensions List View
              <div className="space-y-4">
                <SettingsPageHeader
                  title="Extensions"
                  description="Load and manage your extensions"
                />
                <Card className="animate-fadeIn opacity-0" >
                  <div className="w-full divide-y divide-slate-700/30 dark:divide-slate-600/30">
                    {AVAILABLE_EXTENSIONS.map(extension => (
                      <div
                        key={extension.id}
                        className="flex items-center justify-between p-3"
                      >
                        <div className="space-y-0.5">
                          <p className="text-sm font-semibold leading-none text-slate-900 dark:text-slate-100">
                            {extension.name}
                          </p>
                          <p className="text-sm text-slate-600 dark:text-slate-400">
                            {extension.description}
                          </p>
                        </div>
                        <Button
                          size="XS"
                          theme="light"
                          text="Load"
                          onClick={() => handleSetActiveExtension(extension)}
                        />
                      </div>
                    ))}
                  </div>
                </Card>
              </div>
            )}
          </div>
        </div>
      </div>
    </GridCard>
  );
}
