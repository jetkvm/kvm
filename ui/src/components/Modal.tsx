import React from "react";
import { Dialog, DialogBackdrop, DialogPanel } from "@headlessui/react";

import { cx } from "@/cva.config";

const Modal = React.memo(function Modal({
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
    <Dialog open={open} onClose={onClose} className="relative z-20">
      <DialogBackdrop
        transition
        className="fixed inset-0 bg-gray-500/75 transition-opacity data-[closed]:opacity-0 data-[enter]:duration-500 data-[leave]:duration-200 data-[enter]:ease-out data-[leave]:ease-in dark:bg-slate-900/90"
      />
      <div className="fixed inset-0 z-20 w-screen overflow-y-auto">
        {/* TODO: This doesn't work well with other-sessions */}
        <div className="flex min-h-full items-end justify-center p-4 text-center md:items-baseline md:p-4">
          <DialogPanel
            transition
            className={cx(
              "pointer-events-none relative w-full md:my-8 md:!mt-[10vh]",
              "transform transition-all data-[closed]:translate-y-8 data-[closed]:opacity-0 data-[enter]:duration-500 data-[leave]:duration-200 data-[enter]:ease-out data-[leave]:ease-in",
              className,
            )}
          >
            <div className="pointer-events-auto inline-block w-full text-left">
              <div className="flex justify-center" onClick={onClose}>
                <div
                  className="pointer-events-none w-full"
                  onClick={e => e.stopPropagation()}
                >
                  {children}
                </div>
              </div>
            </div>
          </DialogPanel>
        </div>
      </div>
    </Dialog>
  );
});

export default Modal;
