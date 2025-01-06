import { useJsonRpc } from "@/hooks/useJsonRpc";
import { Button } from "@components/Button";
import { PluginStatus, usePluginStore, useUiStore } from "@/hooks/stores";
import { useCallback, useEffect, useState } from "react";
import UploadPluginModal from "@components/UploadPluginDialog";
import PluginConfigureModal from "@components/PluginConfigureDialog";
import { PluginStatusIcon } from "./PluginStatusIcon";

export default function PluginList() {
  const [send] = useJsonRpc();
  const [error, setError] = useState<string | null>(null);

  const {
    isPluginUploadModalOpen,
    setIsPluginUploadModalOpen,
    setPluginUploadModalView,
    plugins,
    setPlugins,
    pluginConfigureModalOpen,
    setPluginConfigureModalOpen,
    configuringPlugin,
    setConfiguringPlugin,
  } = usePluginStore();
  const sidebarView = useUiStore(state => state.sidebarView);

  const updatePlugins = useCallback(() => {
    setError(null);
    send("pluginList", {}, resp => {
      if ("error" in resp) {
        setError(resp.error.message);
        return
      }
      console.log('pluginList', resp.result);
      setPlugins(resp.result as PluginStatus[]);
    });
  }, [send, setPlugins])

  useEffect(() => {
    // Only update plugins when the sidebar view is the settings view
    if (sidebarView !== "system" && !pluginConfigureModalOpen) return;
    updatePlugins();

    const updateInterval = setInterval(() => {
      updatePlugins();
    }, 10_000);
    return () => clearInterval(updateInterval);
  }, [updatePlugins, sidebarView, pluginConfigureModalOpen])

  return (
    <>
      <div className="overflow-auto max-h-40 border border-gray-200 dark:border-gray-700 rounded-md">
        <ul role="list" className="divide-y divide-gray-200 dark:divide-gray-700 w-full">
          {error && <li className="text-red-500 dark:text-red-400">{error}</li>}
          {plugins.length === 0 && <li className="text-sm text-center text-gray-500 dark:text-gray-400 py-5">No plugins installed</li>}
          {plugins.map(plugin => (
            <li key={plugin.name} className="flex items-center justify-between pa-2 py-2 gap-x-2">
              <PluginStatusIcon plugin={plugin} />
              <div className="overflow-hidden flex grow flex-col">
                <p className="text-base font-semibold text-black dark:text-white">{plugin.name}</p>
                <p className="text-xs text-slate-700 dark:text-slate-300 line-clamp-1">
                  <a href={plugin.homepage} target="_blank" rel="noopener noreferrer" className="font-medium text-blue-600 hover:text-blue-800 dark:text-blue-500 dark:hover:text-blue-400">{plugin.homepage}</a>
                </p>
              </div>
              <div className="flex items-center w-20">
                <Button
                  size="SM"
                  theme="light"
                  text="Settings"
                  onClick={() => {
                    setConfiguringPlugin(plugin.name);
                    setPluginConfigureModalOpen(true);
                  }}
                />
              </div>
            </li>
          ))}
        </ul>
      </div>

      <PluginConfigureModal
        open={pluginConfigureModalOpen}
        setOpen={(open) => {
          setPluginConfigureModalOpen(open);
          if (!open) {
            updatePlugins();
          }
        }}
        plugin={plugins.find(p => p.name == configuringPlugin) ?? null}
      />

      <div className="flex items-center gap-x-2">
        <Button
          size="SM"
          theme="primary"
          text="Upload Plugin"
          onClick={() => {
            setPluginUploadModalView("upload");
            setIsPluginUploadModalOpen(true)
          }}
        />
        <UploadPluginModal
          open={isPluginUploadModalOpen}
          setOpen={(open) => {
            setIsPluginUploadModalOpen(open);
            if (!open) {
              updatePlugins();
            }
          }}
        />
      </div>
    </>
  );
}
