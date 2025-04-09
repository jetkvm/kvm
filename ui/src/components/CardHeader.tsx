import React from "react";

interface Props {
  headline: string;
  description?: string | React.ReactNode;
  Button?: React.ReactNode;
}

export const CardHeader = ({ headline, description, Button }: Props) => {
  return (
    <div className="flex items-center justify-between pb-0 gap-x-4">
      <div className="space-y-1 grow">
        <h3 className="text-lg font-bold leading-none text-black dark:text-white">{headline}</h3>
        {description && <div className="text-sm text-slate-700 dark:text-slate-300">{description}</div>}
      </div>
      {Button && <div>{Button}</div>}
    </div>
  );
};
