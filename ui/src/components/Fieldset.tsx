import React from "react";
import clsx from "clsx";
import { useNavigation } from "react-router";
import type { FetcherWithComponents } from "react-router";

export default function Fieldset({
  children,
  fetcher,
  className,
  disabled,
}: {
  children: React.ReactNode;
  fetcher?: FetcherWithComponents<unknown>;
  className?: string;
  disabled?: boolean;
}) {
  const navigation = useNavigation();
  const loader = fetcher ? fetcher : navigation;
  return (
    <fieldset
      className={clsx(className)}
      disabled={
        disabled ??
        ((loader.state === "submitting" || loader.state === "loading") &&
          loader.formMethod?.toLowerCase() === "post")
      }
    >
      {children}
    </fieldset>
  );
}
