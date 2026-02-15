import { useNavigate, useOutletContext } from "react-router";

import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import LogoBlue from "@assets/logo-blue.svg";
import LogoWhite from "@assets/logo-white.svg";
import { m } from "@localizations/messages";

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
            <p className="text-base font-semibold dark:text-white">{m.other_session_detected()}</p>
            <p className="mb-4 text-sm text-slate-600 dark:text-slate-400">
              {m.other_session_take_over()}
            </p>
            <div className="flex items-center justify-start space-x-4">
              <Button
                size="SM"
                theme="primary"
                text={m.other_session_use_here_button()}
                onClick={handleClose}
              />
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
