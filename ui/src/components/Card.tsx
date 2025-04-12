import React, { forwardRef } from "react";

import { cx } from "@/cva.config";

interface CardPropsType {
  children: React.ReactNode;
  className?: string;
}

export const GridCard = ({
  children,
  cardClassName,
}: {
  children: React.ReactNode;
  cardClassName?: string;
}) => {
  return (
    <Card className={cx("overflow-hidden", cardClassName)}>
      <div className="relative h-full">
        <div className="absolute inset-0 z-0 h-full w-full bg-gradient-to-tr from-blue-50/30 to-blue-50/20 transition-colors duration-300 ease-in-out dark:from-slate-800/30 dark:to-slate-800/20" />
        <div className="absolute inset-0 z-0 h-full w-full rotate-0 bg-grid-blue-100/[25%] dark:bg-grid-slate-700/[7%]" />
        <div className="isolate h-full">{children}</div>
      </div>
    </Card>
  );
};

const Card = forwardRef<HTMLDivElement, CardPropsType>(({ children, className }, ref) => {
  return (
    <div
      ref={ref}
      className={cx(
        "w-full rounded border-none bg-white shadow outline outline-1 outline-slate-800/30 dark:bg-slate-800 dark:outline-slate-300/20",
        className,
      )}
    >
      {children}
    </div>
  );
});

Card.displayName = "Card";

export default Card;
