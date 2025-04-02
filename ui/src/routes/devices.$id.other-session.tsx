import { useNavigate, useOutletContext } from "react-router-dom";

import { GridCard } from "@/components/Card";
import { Button } from "@components/Button";
import LogoBlue from "@/assets/logo-blue.svg";
import LogoWhite from "@/assets/logo-white.svg";

interface ContextType {
  setupPeerConnection: () => Promise<void>;
}
/* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */

export default function OtherSessionRoute() {
  const outletContext = useOutletContext<ContextType>();
  const navigate = useNavigate();

  // Function to handle closing the modal
  const handleClose = () => {
    outletContext?.setupPeerConnection().then(() => navigate(".."));
  };

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
              <Button size="SM" theme="primary" text="Use Here" onClick={handleClose} />
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
