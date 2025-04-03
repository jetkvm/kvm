import { useEffect } from "react";
import { LuCommand } from "react-icons/lu";

import { Button } from "@components/Button";
import Container from "@components/Container";
import { useMacrosStore } from "@/hooks/stores";
import useKeyboard from "@/hooks/useKeyboard";
import { useJsonRpc } from "@/hooks/useJsonRpc";

export default function MacroBar() {
  const { macros, initialized, loadMacros, setSendFn } = useMacrosStore();
  const { executeMacro } = useKeyboard();
  const [send] = useJsonRpc();

  useEffect(() => {
    setSendFn(send);
    
    if (!initialized) {
      loadMacros();
    }
  }, [initialized, loadMacros, setSendFn, send]);

  if (macros.length === 0) {
    return null;
  }

  return (
    <Container className="bg-white dark:bg-slate-900 border-b border-b-slate-800/20 dark:border-b-slate-300/20">
      <div className="flex items-center gap-x-2 py-1.5">
        <div className="absolute -ml-5">
          <LuCommand className="h-4 w-4 text-slate-500" />
        </div>
        <div className="flex flex-wrap gap-2">
          {macros.map(macro => (
            <Button
              key={macro.id}
              aria-label={macro.name}
              size="XS"
              theme="light"
              text={macro.name}
              onClick={() => executeMacro(macro.steps)}
            />
          ))}
        </div>
      </div>
    </Container>
  );
} 