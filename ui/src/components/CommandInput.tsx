import React, { useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";

import InputField from "@/components/InputField"; // your existing input component
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";

interface Hit {
  value: string;
  index: number;
}

// ---------- history hook ----------
function useCommandHistory(max = 300) {
  const { send } = useJsonRpc();
  const [items, setItems] = useState<string[]>([]);

  const deleteHistory = useCallback(() => {
    console.log("Deleting serial command history");
    send("deleteSerialCommandHistory", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to delete serial command history: ${resp.error.data || "Unknown error"}`,
        );
      } else {
        setItems([]);
        notifications.success("Serial command history deleted");
      }
    });
  }, [send]);

  useEffect(() => {
    send("getSerialCommandHistory", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(`Failed to get command history: ${resp.error.data || "Unknown error"}`);
      } else if ("result" in resp) {
        setItems(resp.result as string[]);
      }
    });
  }, [send]);

  const [pointer, setPointer] = useState<number>(-1); // -1 = fresh line
  const [anchorPrefix, setAnchorPrefix] = useState<string | null>(null);

  useEffect(() => {
    if (items.length > 1) {
      send("setSerialCommandHistory", { commandHistory: items }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            `Failed to update command history: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
      });
    }
  }, [items, send]);

  const push = useCallback(
    (cmd: string) => {
      if (!cmd.trim()) return;
      setItems(prev => {
        const next = prev[prev.length - 1] === cmd ? prev : [...prev, cmd];
        return next.slice(-max);
      });
      setPointer(-1);
      setAnchorPrefix(null);
    },
    [max],
  );

  const resetTraversal = useCallback(() => {
    setPointer(-1);
    setAnchorPrefix(null);
  }, []);

  const up = useCallback(
    (draft: string) => {
      const pref = anchorPrefix ?? draft;
      if (anchorPrefix == null) setAnchorPrefix(pref);
      let i = pointer < 0 ? items.length - 1 : pointer - 1;
      for (; i >= 0; i--) {
        if (items[i].startsWith(pref)) {
          setPointer(i);
          return items[i];
        }
      }
      return draft;
    },
    [items, pointer, anchorPrefix],
  );

  const down = useCallback(
    (draft: string) => {
      const pref = anchorPrefix ?? draft;
      if (anchorPrefix == null) setAnchorPrefix(pref);
      let i = pointer < 0 ? 0 : pointer + 1;
      for (; i < items.length; i++) {
        if (items[i].startsWith(pref)) {
          setPointer(i);
          return items[i];
        }
      }
      setPointer(-1);
      return draft;
    },
    [items, pointer, anchorPrefix],
  );

  const search = useCallback(
    (query: string): Hit[] => {
      if (!query) return [];
      const q = query.toLowerCase();
      return [...items]
        .map((value, index) => ({ value, index }))
        .filter(x => x.value.toLowerCase().includes(q))
        .reverse(); // newest first
    },
    [items],
  );

  return { push, up, down, resetTraversal, search, deleteHistory };
}

function Portal({ children }: { children: React.ReactNode }) {
  if (typeof document === "undefined") {
    return null;
  }
  return createPortal(children, document.body);
}

// ---------- reverse search popup ----------
function ReverseSearch({
  open,
  results,
  sel,
  setSel,
  onPick,
  onClose,
  onDeleteHistory,
}: {
  open: boolean;
  results: Hit[];
  sel: number;
  setSel: (i: number) => void;
  onPick: (val: string) => void;
  onClose: () => void;
  onDeleteHistory: () => void;
}) {
  const listRef = React.useRef<HTMLDivElement>(null);

  // keep selected item in view when sel changes
  useEffect(() => {
    if (!listRef.current) return;
    const el = listRef.current.querySelector<HTMLDivElement>(`[data-idx="${sel}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [sel, results]);

  if (!open) return null;
  return (
    <Portal>
      <div
        className="absolute right-0 bottom-12 left-0 mr-8 mb-5 ml-17 rounded-md border border-slate-600 bg-slate-900/95 p-2 shadow-lg"
        role="listbox"
        aria-activedescendant={`rev-opt-${sel}`}
      >
        <div ref={listRef} className="max-h-48 overflow-auto">
          {results.length === 0 ? (
            <div className="px-2 py-1 text-sm text-slate-400">No matches</div>
          ) : (
            results.map((r, i) => (
              <div
                id={`rev-opt-${i}`}
                data-idx={i}
                key={`${r.index}-${i}`}
                role="option"
                aria-selected={i === sel}
                className={clsx(
                  "cursor-pointer px-2 py-1 font-mono text-sm",
                  i === sel ? "rounded bg-slate-700 text-white" : "text-slate-200",
                )}
                onMouseEnter={() => setSel(i)}
                onClick={() => onPick(r.value)}
              >
                {r.value}
              </div>
            ))
          )}
        </div>
        <div className="text-s mt-1 flex justify-between text-slate-400">
          <span>↑/↓ select • Enter accept • Esc close</span>
          <div>
            <button className="mr-2 underline" onClick={onClose}>
              Close
            </button>
            <button className="mr-2 underline" onClick={onDeleteHistory}>
              Delete history
            </button>
          </div>
        </div>
      </div>
    </Portal>
  );
}

// ---------- main component ----------
interface CommandInputProps {
  onSend: (line: string) => void; // called on Enter
  storageKey?: string; // localStorage key for history
  placeholder?: string; // input placeholder
  className?: string; // container className
  disabled?: boolean; // disable input (optional)
}

export function CommandInput({
  onSend,
  placeholder = "Type serial command…  (Enter to send • ↑/↓ history • Ctrl+R search)",
  className,
  disabled,
}: CommandInputProps) {
  const [cmd, setCmd] = useState("");
  const [revOpen, setRevOpen] = useState(false);
  const [revQuery, setRevQuery] = useState("");
  const [sel, setSel] = useState(0);
  const { push, up, down, resetTraversal, search, deleteHistory } = useCommandHistory();

  const results = useMemo(() => search(revQuery), [revQuery, search]);

  const cmdInputRef = React.useRef<HTMLInputElement>(null);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    const isMeta = e.ctrlKey || e.metaKey;

    if (e.key === "Enter" && !e.shiftKey && !isMeta) {
      e.preventDefault();
      if (!cmd) return;
      onSend(cmd);
      push(cmd);
      setCmd("");
      resetTraversal();
      setRevOpen(false);
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      setCmd(prev => up(prev));
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setCmd(prev => down(prev));
      return;
    }
    if (isMeta && e.key.toLowerCase() === "r") {
      e.preventDefault();
      setRevOpen(true);
      setRevQuery(cmd);
      setSel(0);
      return;
    }
    if (e.key === "Escape" && revOpen) {
      e.preventDefault();
      setRevOpen(false);
      return;
    }
  };

  return (
    <div className={clsx("relative", className)}>
      <div className="flex items-center gap-2" style={{ visibility: revOpen ? "hidden" : "unset" }}>
        <span className="text-xs text-slate-400 select-none">CMD</span>
        <InputField
          ref={cmdInputRef}
          size="MD"
          disabled={disabled}
          value={cmd}
          onChange={e => {
            setCmd(e.target.value);
            resetTraversal();
          }}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          className="font-mono"
        />
      </div>

      {/* Reverse search controls */}
      {revOpen && (
        <div className="-mt-10">
          <div className="flex items-center gap-2 bg-[#0f172a]">
            <span className="text-s text-slate-400 select-none">Search</span>
            <InputField
              size="MD"
              autoFocus
              value={revQuery}
              onChange={e => {
                setRevQuery(e.target.value);
                setSel(0); // reset selection whenever the query changes
              }}
              onKeyDown={e => {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setSel(i => (i + 1) % Math.max(1, results.length));
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setSel(i => (i - 1 + results.length) % Math.max(1, results.length));
                } else if (e.key === "Enter") {
                  // ...
                } else if (e.key === "Escape") {
                  // ...
                }
              }}
              placeholder="Type to filter history…"
              className="font-mono"
            />
          </div>
          <ReverseSearch
            open={revOpen}
            results={results}
            sel={sel}
            setSel={setSel}
            onPick={v => {
              setCmd(v);
              setRevOpen(false);
              requestAnimationFrame(() => cmdInputRef.current?.focus());
            }}
            onClose={() => {
              setRevOpen(false);
              requestAnimationFrame(() => cmdInputRef.current?.focus());
            }}
            onDeleteHistory={deleteHistory}
          />
        </div>
      )}
    </div>
  );
}

export default CommandInput;
