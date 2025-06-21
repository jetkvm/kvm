import React from "react";

import { GridCard } from "@/components/Card";

import { cx } from "../cva.config";

interface Props {
  IconElm?: React.FC<{ className: string | undefined }>;
  headline: string;
  description?: string | React.ReactNode;
  BtnElm?: React.ReactNode;
  className?: string;
}

export default function EmptyCard({
  IconElm,
  headline,
  description,
  BtnElm,
  className,
}: Props) {
  return (
    <GridCard>
      <div
        className={cx(
          "flex min-h-[256px] w-full flex-col items-center justify-center gap-y-4 px-4 py-5 text-center",
          className,
        )}
      >
        <div className="max-w-[90%] space-y-1.5 text-center md:max-w-[60%]">
          <div className="space-y-2">
            {IconElm && (
              <IconElm className="mx-auto h-5 w-5 text-blue-600 dark:text-blue-600" />
            )}
            <h4 className="text-base font-bold leading-none text-black dark:text-white">
              {headline}
            </h4>
          </div>
          <p className="mx-auto text-sm text-slate-600 dark:text-slate-400">
            {description}
          </p>
        </div>
        {BtnElm}
      </div>
    </GridCard>
  );
}
