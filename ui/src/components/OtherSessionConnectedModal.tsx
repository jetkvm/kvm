import { GridCard } from "@/components/Card";
import { Button } from "@components/Button";
import LogoBlue from "@/assets/logo-blue.svg";
import LogoWhite from "@/assets/logo-white.svg";
import Modal from "@components/Modal";

export default function OtherSessionConnectedModal({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) {
  return (
    <Modal open={open} onClose={() => setOpen(false)}>
      <Dialog setOpen={setOpen} />
    </Modal>
  );
}

export function Dialog({ setOpen }: { setOpen: (open: boolean) => void }) {
  return (
    <GridCard cardClassName="relative mx-auto max-w-md text-left pointer-events-auto">
      <div className="p-10">
        <div className="flex min-h-[140px] flex-col items-start justify-start space-y-4 text-left">
          <div className="h-[24px]">
            <img src={LogoBlue} alt="" className="h-full dark:hidden" />
            <img src={LogoWhite} alt="" className="hidden h-full dark:block" />
          </div>

          <div className="text-left">
            <p className="text-base font-semibold dark:text-white">
              Another Active Session Detected
            </p>
            <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
              Only one active session is supported at a time. Would you like to take over
              this session?
            </p>
            <div className="flex items-center justify-start space-x-4">
              <Button
                size="SM"
                theme="primary"
                text="Use Here"
                onClick={() => setOpen(false)}
              />
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
