import React from "react";

interface Props {
  headline: string;
  description?: string | React.ReactNode;
  Button?: React.ReactNode;
}

export const CardHeader = ({ headline, description, Button }: Props) => {
  return (
    <div className="flex items-center justify-between gap-x-4 pb-0">
      <div className="grow space-y-1">
        <h3 className="text-lg leading-none font-bold text-black dark:text-white">
          {headline}
        </h3>
        {description && (
          <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>
        )}
      </div>
      {Button && <div>{Button}</div>}
    </div>
  );
};
