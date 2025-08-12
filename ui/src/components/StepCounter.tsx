import { CheckIcon } from "@heroicons/react/16/solid";

import { cva, cx } from "@/cva.config";
import Card from "@/components/Card";

interface Props {
  nSteps: number;
  currStepIdx: number;
  size?: keyof typeof sizes;
}

const sizes = {
  SM: "text-xs leading-[12px]",
  MD: "text-sm leading-[14px]",
};

const variants = cva({
  variants: {
    size: sizes,
  },
});

export default function StepCounter({ nSteps, currStepIdx, size = "MD" }: Props) {
  const textStyle = variants({ size });
  return (
    <Card className="inline-flex! w-auto select-none items-center justify-center gap-x-2 rounded-lg p-1">
      {[...Array(nSteps).keys()].map(i => {
        if (i < currStepIdx) {
          return (
            <div
              className={cx(
                "flex items-center justify-center rounded-full border border-blue-800 bg-blue-700 text-slate-600 dark:border-blue-300",
                textStyle,
                size === "SM" ? "h-5 w-5" : "h-6 w-6",
              )}
              key={`${i}-${currStepIdx}`}
            >
              <CheckIcon className="h-3.5 w-3.5 text-white" />
            </div>
          );
        }

        if (i === currStepIdx) {
          return (
            <div
              className={cx(
                "rounded-md border border-blue-800 bg-blue-700 px-2 py-1 font-medium text-white shadow-xs dark:border-blue-300",
                textStyle,
              )}
              key={`${i}-${currStepIdx}`}
            >
              Step {i + 1}
            </div>
          );
        }
        if (i > currStepIdx) {
          return (
            <Card
              className={cx(
                "flex items-center justify-center rounded-full! text-slate-600 dark:text-slate-400",
                textStyle,
                size === "SM" ? "h-5 w-5" : "h-6 w-6",
              )}
              key={`${i}-${currStepIdx}`}
            >
              {i + 1}
            </Card>
          );
        }
        return null;
      })}
    </Card>
  );
}
