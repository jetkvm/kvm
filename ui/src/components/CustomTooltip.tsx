import Card from "@components/Card";

export interface CustomTooltipProps {
  payload: { payload: { date: number; metric: number }; unit: string }[];
}

export default function CustomTooltip({ payload }: CustomTooltipProps) {
  if (payload?.length) {
    const toolTipData = payload[0];
    const { date, metric } = toolTipData.payload;

    return (
      <Card>
        <div className="px-2 py-1.5 text-black dark:text-white">
          <div className="text-[13px] font-semibold">
            {new Date(date * 1000).toLocaleTimeString()}
          </div>
          <div className="space-y-1">
            <div className="flex items-center gap-x-1">
              <div className="h-[2px] w-2 bg-blue-700" />
              <span className="text-[13px]">
                {metric} {toolTipData?.unit}
              </span>
            </div>
          </div>
        </div>
      </Card>
    );
  }

  return null;
}
