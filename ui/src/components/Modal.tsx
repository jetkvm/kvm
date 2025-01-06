import React from "react";
import { Dialog, DialogBackdrop, DialogPanel } from "@headlessui/react";
import { cx } from "@/cva.config";

export default function Modal({
  children,
  className,
  open,
  onClose,
}: {
  children: React.ReactNode;
  className?: string;
  open: boolean;
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onClose={onClose} className="relative z-10">
      <DialogBackdrop
        transition
        className="fixed inset-0 bg-gray-500/75 dark:bg-slate-900/90 transition-opacity data-[closed]:opacity-0 data-[enter]:duration-300 data-[leave]:duration-200 data-[enter]:ease-out data-[leave]:ease-in"
      />

      <div className="fixed inset-0 z-10 w-screen overflow-y-auto">
        <div className="flex items-end justify-center min-h-full p-4 text-center sm:items-center sm:p-0">
          <DialogPanel
            transition
            className={cx(
              "pointer-events-none relative w-full sm:my-8",
              "transform transition-all data-[closed]:translate-y-8 data-[closed]:opacity-0 data-[enter]:duration-300 data-[leave]:duration-300 data-[enter]:ease-out data-[leave]:ease-in",
              className,
            )}
          >
            <div className="inline-block w-full text-left pointer-events-auto">
              <div className="flex justify-center" onClick={onClose}>
                <div className="w-full pointer-events-none" onClick={e => e.stopPropagation()}>
                  {children}
                </div>
              </div>
            </div>
          </DialogPanel>
        </div>
      </div>
    </Dialog>
  );
}
