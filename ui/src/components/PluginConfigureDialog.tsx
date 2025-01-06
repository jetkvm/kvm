import { PluginStatus, usePluginStore } from "@/hooks/stores";
import Modal from "@components/Modal";
import AutoHeight from "@components/AutoHeight";
import Card, { GridCard } from "@components/Card";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import { ViewHeader } from "./MountMediaDialog";
import { Button } from "./Button";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useCallback, useEffect, useState } from "react";
import { PluginStatusIcon } from "./PluginStatusIcon";
import { cx } from "@/cva.config";

export default function PluginConfigureModal({
  plugin,
  open,
  setOpen,
}: {
  plugin: PluginStatus | null;
  open: boolean;
  setOpen: (open: boolean) => void;
}) {
  return (
    <Modal open={!!plugin && open} onClose={() => setOpen(false)}>
      <Dialog plugin={plugin} setOpen={setOpen} />
    </Modal>
  )
}

function Dialog({ plugin, setOpen }: { plugin: PluginStatus | null, setOpen: (open: boolean) => void }) {
  const [send] = useJsonRpc();

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const {setIsPluginUploadModalOpen} = usePluginStore();

  useEffect(() => {
    setLoading(false);
  }, [plugin])

  const updatePlugin = useCallback((enabled: boolean) => {
    if (!plugin) return;
    if (!enabled) {
      if (!window.confirm("Are you sure you want to disable this plugin?")) {
        return;
      }
    }

    setLoading(true);
    send("pluginUpdateConfig", { name: plugin.name, enabled }, resp => {
      if ("error" in resp) {
        setLoading(false);
        setError(resp.error.message);
        return
      }
      setOpen(false);
    });
  }, [send, plugin, setOpen])

  const uninstallPlugin = useCallback(() => {
    if (!plugin) return;
    if (!window.confirm("Are you sure you want to uninstall this plugin? This will not delete any data.")) {
      return;
    }

    setLoading(true);
    send("pluginUninstall", { name: plugin.name }, resp => {
      if ("error" in resp) {
        setLoading(false);
        setError(resp.error.message);
        return
      }
      setOpen(false);
    });
  }, [send, plugin, setOpen])

  const uploadPlugin = useCallback(() => {
    setOpen(false);
    setIsPluginUploadModalOpen(true);
  }, [setIsPluginUploadModalOpen, setOpen])

  return (
    <AutoHeight>
      <div className="mx-auto max-w-4xl px-4 transition-all duration-300 ease-in-out">
        <GridCard cardClassName="relative w-full text-left pointer-events-auto">
          <div className="p-4">
            <div className="flex flex-col items-start justify-start space-y-4 text-left">
              <div className="flex justify-between w-full">
                <div>
                  <img
                    src={LogoBlueIcon}
                    alt="JetKVM Logo"
                    className="h-[24px] dark:hidden block"
                  />
                  <img
                    src={LogoWhiteIcon}
                    alt="JetKVM Logo"
                    className="h-[24px] dark:block hidden dark:!mt-0"
                  />
                </div>
                <div className="flex items-center">
                  {plugin && <>
                    <p className="text-sm text-gray-500 dark:text-gray-400 inline-block">
                      {plugin.status}
                    </p>
                    <PluginStatusIcon plugin={plugin} />
                  </>}
                </div>
              </div>
              <div className="w-full space-y-4">
                <div className="flex items-center justify-between w-full">
                  <ViewHeader title="Plugin Configuration" description={`Configure the ${plugin?.name} plugin`} />
                  <div>
                    {/* Enable/Disable toggle */}
                    <Button
                      size="MD"
                      theme={plugin?.enabled ? "danger" : "light"}
                      text={plugin?.enabled ? "Disable Plugin" : "Enable Plugin"}
                      loading={loading}
                      onClick={() => {
                        updatePlugin(!plugin?.enabled);
                      }}
                    />
                  </div>
                </div>

                <div className="grid grid-cols-[auto,1fr] gap-x-4 text-sm text-black dark:text-white">
                  <span className="font-semibold">
                    Name
                  </span>
                  <span>{plugin?.name}</span>

                  <span className="font-semibold">
                    Active Version
                  </span>
                  <span>{plugin?.version}</span>

                  <span className="font-semibold">
                    Description
                  </span>
                  <span>{plugin?.description}</span>

                  <span className="font-semibold">
                    Homepage
                  </span>
                  <a href={plugin?.homepage} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:text-blue-800 dark:text-blue-500 dark:hover:text-blue-400">
                    {plugin?.homepage}
                  </a>
                </div>

                <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

                <div
                  className="space-y-2 opacity-0 animate-fadeIn"
                  style={{
                    animationDuration: "0.7s",
                  }}
                >
                  {error && <p className="text-red-500 dark:text-red-400">{error}</p>}
                  {plugin?.message && (
                    <>
                      <p className="text-sm text-gray-500 dark:text-gray-400">
                        Plugin message:
                      </p>
                      <Card className={cx(
                        "text-gray-500 dark:text-gray-400 p-4 border",
                        plugin.status === "error" && "border-red-200 bg-red-50 text-red-800 dark:text-red-400",
                      )}>
                        {plugin.message}
                      </Card>
                    </>
                  )}
                  <p className="text-sm text-gray-500 dark:text-gray-400 py-10">
                    Plugin configuration coming soon
                  </p>

                  <div className="h-[1px] w-full bg-slate-800/10 dark:bg-slate-300/20" />

                  <div
                    className="flex items-end w-full opacity-0 animate-fadeIn"
                    style={{
                      animationDuration: "0.7s",
                      animationDelay: "0.1s",
                    }}
                  >
                    <div className="flex items-center w-full space-x-2">
                      <Button
                        size="MD"
                        theme="primary"
                        text="Upload New Version"
                        disabled={loading}
                        onClick={uploadPlugin}
                      />
                      <Button
                        size="MD"
                        theme="blank"
                        text="Uninstall Plugin"
                        disabled={loading}
                        onClick={uninstallPlugin}
                      />
                    </div>
                    <div className="flex justify-end w-full space-x-2">
                      <Button
                        size="MD"
                        theme="light"
                        text="Back"
                        disabled={loading}
                        onClick={() => {
                          setOpen(false);
                        }}
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </GridCard>
      </div >
    </AutoHeight >
  )
}
