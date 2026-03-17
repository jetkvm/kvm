import { useCallback, useRef, useState } from "react";
import { LuUpload, LuCheck } from "react-icons/lu";
import { ExclamationTriangleIcon } from "@heroicons/react/20/solid";

import { Button } from "@components/Button";
import { SettingsItem } from "@components/SettingsItem";
import { DEVICE_API } from "@/ui.config";
import { m } from "@localizations/messages.js";

interface UploadResult {
  verified: boolean;
  hashOK: boolean;
  signatureOK: boolean;
  keyFetchFailed: boolean;
  signatureError?: string;
  error?: string;
}

type UploadState = "idle" | "uploading" | "verifying" | "verified" | "applying" | "error";

interface ComponentUploadState {
  state: UploadState;
  progress: number;
  result: UploadResult | null;
  error: string | null;
}

const initialState: ComponentUploadState = {
  state: "idle",
  progress: 0,
  result: null,
  error: null,
};

function ComponentUpload({ component, label }: { component: string; label: string }) {
  const [upload, setUpload] = useState<ComponentUploadState>(initialState);
  const [showBypassPrompt, setShowBypassPrompt] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleFileSelect = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (!file) return;

      if (!file.name.endsWith(".tar.gz") && !file.name.endsWith(".tgz")) {
        setUpload({
          ...initialState,
          state: "error",
          error: m.offline_update_invalid_file(),
        });
        return;
      }

      const formData = new FormData();
      formData.append("component", component);
      formData.append("file", file);

      const xhr = new XMLHttpRequest();
      xhr.open("POST", `${DEVICE_API}/ota/upload`, true);

      setUpload({ ...initialState, state: "uploading" });

      xhr.upload.onprogress = event => {
        if (event.lengthComputable) {
          const pct = Math.round((event.loaded / event.total) * 100);
          setUpload(prev => ({ ...prev, progress: pct }));
          if (pct >= 100) {
            setUpload(prev => ({ ...prev, state: "verifying" }));
          }
        }
      };

      xhr.onload = () => {
        try {
          const result: UploadResult = JSON.parse(xhr.responseText);
          if (xhr.status === 200 && result.verified) {
            setUpload({
              state: "verified",
              progress: 100,
              result,
              error: null,
            });
            if (result.keyFetchFailed) {
              setShowBypassPrompt(true);
            }
          } else {
            setUpload({
              state: "error",
              progress: 0,
              result: null,
              error: result.error || `HTTP ${xhr.status}`,
            });
          }
        } catch {
          setUpload({
            state: "error",
            progress: 0,
            result: null,
            error: xhr.statusText || "Unknown error",
          });
        }
      };

      xhr.onerror = () => {
        setUpload({
          state: "error",
          progress: 0,
          result: null,
          error: "Network error",
        });
      };

      xhr.send(formData);

      // Reset input so re-selecting the same file works
      if (fileInputRef.current) fileInputRef.current.value = "";
    },
    [component],
  );

  const handleApply = useCallback(
    (bypassSignature: boolean) => {
      setUpload(prev => ({ ...prev, state: "applying" }));
      setShowBypassPrompt(false);

      fetch(`${DEVICE_API}/ota/apply`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ component, bypassSignature }),
      }).catch(() => {
        // Expected: the device reboots, dropping the connection
      });
    },
    [component],
  );

  const reset = useCallback(() => {
    setUpload(initialState);
    setShowBypassPrompt(false);
  }, []);

  return (
    <div className="space-y-2">
      <p className="text-sm font-medium text-slate-900 dark:text-white">{label}</p>

      {upload.state === "idle" && (
        <div>
          <Button
            size="SM"
            theme="light"
            text={m.offline_update_select_file()}
            LeadingIcon={LuUpload}
            onClick={() => fileInputRef.current?.click()}
          />
          <input
            ref={fileInputRef}
            type="file"
            accept=".tar.gz,.tgz"
            onChange={handleFileSelect}
            className="hidden"
          />
        </div>
      )}

      {(upload.state === "uploading" || upload.state === "verifying") && (
        <div className="space-y-1">
          <div className="flex items-center justify-between text-sm">
            <span className="text-slate-600 dark:text-slate-400">
              {upload.state === "uploading"
                ? m.offline_update_uploading()
                : m.offline_update_verifying()}
            </span>
            <span className="font-mono text-[13px] text-slate-600 dark:text-slate-400">
              {upload.progress}%
            </span>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
            <div
              className="h-2 rounded-full bg-blue-600 transition-all duration-300"
              style={{ width: `${upload.progress}%` }}
            />
          </div>
        </div>
      )}

      {upload.state === "verified" && upload.result && !showBypassPrompt && (
        <div className="space-y-2">
          <div className="flex items-center gap-x-2 text-sm text-green-700 dark:text-green-400">
            <LuCheck className="h-4 w-4" />
            <span>{m.offline_update_hash_ok()}</span>
          </div>
          {upload.result.signatureOK && (
            <div className="flex items-center gap-x-2 text-sm text-green-700 dark:text-green-400">
              <LuCheck className="h-4 w-4" />
              <span>{m.offline_update_signature_ok()}</span>
            </div>
          )}
          <div className="flex gap-x-2">
            <Button
              size="SM"
              theme="primary"
              text={m.offline_update_apply()}
              onClick={() => handleApply(false)}
            />
            <Button size="SM" theme="light" text={m.cancel()} onClick={reset} />
          </div>
        </div>
      )}

      {showBypassPrompt && (
        <div className="space-y-2 rounded-md border border-amber-300 bg-amber-50 p-3 dark:border-amber-700 dark:bg-amber-900/20">
          <div className="flex items-start gap-x-2">
            <ExclamationTriangleIcon className="mt-0.5 h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="space-y-1">
              <p className="text-sm font-medium text-amber-800 dark:text-amber-200">
                {m.offline_update_signature_bypass_title()}
              </p>
              <p className="text-sm text-amber-700 dark:text-amber-300">
                {m.offline_update_signature_bypass_description()}
              </p>
            </div>
          </div>
          <div className="flex gap-x-2">
            <Button
              size="SM"
              theme="danger"
              text={m.offline_update_signature_bypass_confirm()}
              onClick={() => handleApply(true)}
            />
            <Button size="SM" theme="light" text={m.cancel()} onClick={reset} />
          </div>
        </div>
      )}

      {upload.state === "applying" && (
        <p className="text-sm text-slate-600 dark:text-slate-400">
          {m.offline_update_applying()}
        </p>
      )}

      {upload.state === "error" && upload.error && (
        <div className="space-y-2">
          <p className="text-sm text-red-600 dark:text-red-400">
            {m.offline_update_error({ error: upload.error })}
          </p>
          <Button size="SM" theme="light" text={m.retry()} onClick={reset} />
        </div>
      )}
    </div>
  );
}

export default function OfflineUpdateCard() {
  return (
    <div className="space-y-4">
      <SettingsItem
        title={m.offline_update_title()}
        description={m.offline_update_description()}
      />
      <div className="space-y-4 rounded-md border border-slate-200 p-4 dark:border-slate-700">
        <ComponentUpload component="app" label={m.offline_update_app_label()} />
        <hr className="border-slate-200 dark:border-slate-700" />
        <ComponentUpload component="system" label={m.offline_update_system_label()} />
      </div>
    </div>
  );
}
