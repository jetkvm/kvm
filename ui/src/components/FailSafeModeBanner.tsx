import { LuTriangleAlert } from "react-icons/lu";

import Card from "@components/Card";

interface FailsafeModeBannerProps {
  reason: string;
}

export function FailsafeModeBanner({ reason }: FailsafeModeBannerProps) {
  const getReasonMessage = () => {
    switch (reason) {
      case "video":
        return "Failsafe Mode Active: Video-related settings are currently unavailable";
      default:
        return "Failsafe Mode Active: Some settings may be unavailable";
    }
  };

  return (
    <Card>
      <div className="diagonal-stripes flex items-center gap-3 rounded p-4">
        <LuTriangleAlert className="h-5 w-5 flex-shrink-0 text-red-600 dark:text-red-400" />
        <p className="text-sm font-medium text-red-800 dark:text-white">{getReasonMessage()}</p>
      </div>
    </Card>
  );
}
