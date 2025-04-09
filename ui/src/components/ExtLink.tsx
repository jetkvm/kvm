import React from "react";

import { cx } from "@/cva.config";

export default function ExtLink({
  className,
  href,
  id,
  target,
  children,
}: {
  className?: string;
  href: string;
  id?: string;
  target?: string;
  children: React.ReactNode;
}) {
  return (
    <a
      className={cx(className)}
      target={target ?? "_blank"}
      id={id}
      rel="noopener noreferrer"
      href={href}
    >
      {children}
    </a>
  );
}
