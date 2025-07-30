import SidebarHeader from "@/components/SidebarHeader";
import { useUiStore } from "@/hooks/stores";
import AudioMetricsDashboard from "@/components/AudioMetricsDashboard";

export default function AudioMetricsSidebar() {
  const setSidebarView = useUiStore(state => state.setSidebarView);

  return (
    <>
      <SidebarHeader title="Audio Metrics" setSidebarView={setSidebarView} />
      <div className="h-full overflow-y-scroll bg-white px-4 py-2 pb-8 dark:bg-slate-900">
        <AudioMetricsDashboard />
      </div>
    </>
  );
}