import Card from "@components/Card";

export interface CustomTooltipProps {
  payload: { payload: { date: number; stat: number }; unit: string }[];
}

export default function CustomTooltip({ payload }: CustomTooltipProps) {
  if (payload?.length) {
    const toolTipData = payload[0];
    const { date, stat } = toolTipData.payload;

    return (
      <Card>
        <div className="p-2 text-black dark:text-white">
          <div className="font-semibold">
            {new Date(date * 1000).toLocaleTimeString()}
          </div>
          <div className="space-y-1">
            <div className="flex items-center gap-x-1">
              <div className="h-[2px] w-2 bg-blue-700" />
              <span >
                {stat} {toolTipData?.unit}
              </span>
            </div>
          </div>
        </div>
      </Card>
    );
  }

  return null;
}
