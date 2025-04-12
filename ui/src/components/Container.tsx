/* eslint-disable react-refresh/only-export-components */
import React, { ReactNode } from "react";

import { cx } from "@/cva.config";

function Container({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx("mx-auto h-full w-full px-8 ", className)}>{children}</div>;
}

function Article({ children }: { children: React.ReactNode }) {
  return (
    <Container>
      <div className="grid w-full grid-cols-12">
        <div className="col-span-12 xl:col-span-11 xl:col-start-2">{children}</div>
      </div>
    </Container>
  );
}

export default Object.assign(Container, {
  Article,
});
