import { useState } from "react";

import DiagnosisCard from "@components/DiagnosisCard";
import { cx } from "@/cva.config";
import { cases, healthy } from "@/diagnosis/cases";
import { diagnose } from "@/diagnosis/diagnose";

export default function PlaygroundRoute() {
  const [selected, setSelected] = useState(1);
  const [switching, setSwitching] = useState(false);
  const facts = { ...healthy, ...cases[selected].facts };
  const diagnosis = diagnose(facts);

  return (
    <div className="grid h-full min-h-screen grid-cols-[280px_1fr] bg-slate-50 text-sm text-black dark:bg-slate-900 dark:text-white">
      <nav className="overflow-y-auto border-r border-slate-800/20 p-2 dark:border-slate-300/20">
        {cases.map((c, index) => (
          <button
            key={c.name}
            onClick={() => setSelected(index)}
            className={cx(
              "block w-full rounded-sm px-2 py-1 text-left hover:bg-blue-50 dark:hover:bg-slate-800",
              index === selected && "bg-blue-100 font-semibold dark:bg-slate-700",
            )}
          >
            {c.name}
          </button>
        ))}
      </nav>

      <main className="space-y-4 overflow-y-auto p-4">
        <div className="aspect-video w-full max-w-4xl">
          {diagnosis.status === "ok" ? (
            <div className="flex h-full items-center justify-center rounded-md border border-dashed border-slate-800/30 text-slate-500 dark:border-slate-300/20">
              No card. The video shows.
            </div>
          ) : (
            <DiagnosisCard
              diagnosis={diagnosis}
              onAction={() => undefined}
              busy={switching ? { action: "edidSweep", text: "Profile 2 of 5" } : undefined}
            />
          )}
        </div>

        <label className="flex items-center gap-x-2">
          <input
            type="checkbox"
            checked={switching}
            onChange={event => setSwitching(event.target.checked)}
          />
          Display profile switch in progress
        </label>

        <div className="grid max-w-4xl grid-cols-2 gap-4 font-mono text-xs">
          <pre className="overflow-x-auto rounded-sm bg-white p-3 dark:bg-slate-800">
            {JSON.stringify(cases[selected].facts, null, 2)}
          </pre>
          <pre className="overflow-x-auto rounded-sm bg-white p-3 dark:bg-slate-800">
            {JSON.stringify(diagnosis, null, 2)}
          </pre>
        </div>
      </main>
    </div>
  );
}
