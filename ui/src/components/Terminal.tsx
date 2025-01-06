import "react-simple-keyboard/build/css/index.css";
import { useUiStore, useRTCStore } from "@/hooks/stores";
import { XTerm } from "./Xterm";
import { Button } from "./Button";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import { cx } from "../cva.config";
import { Transition } from "@headlessui/react";

function TerminalWrapper() {
  const enableTerminal = useUiStore(state => state.enableTerminal);
  const setEnableTerminal = useUiStore(state => state.setEnableTerminal);
  const terminalChannel = useRTCStore(state => state.terminalChannel);

  return (
    <div onKeyDown={e => e.stopPropagation()} onKeyUp={e => e.stopPropagation()}>
      <Transition show={enableTerminal} appear>
        <div
          className={cx([
            // Base styles
            "fixed bottom-0 w-full transform transition duration-500 ease-in-out",
            "translate-y-[0px]",
            "data-[enter]:translate-y-[500px]",
            "data-[closed]:translate-y-[500px]",
          ])}
        >
          <div className="h-[500px] w-full bg-[#0f172a]">
            <div className="flex items-center justify-center px-2 py-1 bg-white dark:bg-slate-800 border-y border-y-slate-800/30 dark:border-y-slate-300/20">
              <h2 className="select-none self-center font-sans text-[12px] text-slate-700 dark:text-slate-300">
                Web Terminal
              </h2>
              <div className="absolute right-2">
                <Button
                  size="XS"
                  theme="light"
                  text="Hide"
                  LeadingIcon={ChevronDownIcon}
                  onClick={() => setEnableTerminal(false)}
                />
              </div>
            </div>
            <div className="h-[calc(100%-36px)] p-3">
              <XTerm terminalChannel={terminalChannel} />
            </div>
          </div>
        </div>
      </Transition>
    </div>
  );
}

export default TerminalWrapper;
