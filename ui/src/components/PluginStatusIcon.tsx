import { cx } from "@/cva.config";
import { PluginStatus } from "@/hooks/stores";

export function PluginStatusIcon({ plugin }: { plugin: PluginStatus; }) {
  let classNames = "bg-slate-500 border-slate-600";
  if (plugin.enabled && plugin.status === "running") {
    classNames = "bg-green-500 border-green-600";
  } else if (plugin.enabled && plugin.status === "pending-configuration") {
    classNames = "bg-yellow-500 border-yellow-600";
  } else if (plugin.enabled && plugin.status === "errored") {
    classNames = "bg-red-500 border-red-600";
  }

  return (
    <div className="flex items-center px-2" title={plugin.status}>
      <div className={cx("h-2 w-2 rounded-full border transition", classNames)} />
    </div>
  );
}
