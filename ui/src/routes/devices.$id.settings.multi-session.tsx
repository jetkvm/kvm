import { useEffect, useState } from "react";
import {
  UserGroupIcon,
} from "@heroicons/react/16/solid";

import { useJsonRpc, JsonRpcResponse } from "@/hooks/useJsonRpc";
import { usePermissions } from "@/hooks/usePermissions";
import { Permission } from "@/types/permissions";
import { useSettingsStore } from "@/hooks/stores";
import { notify } from "@/notifications";
import Card from "@/components/Card";
import Checkbox from "@/components/Checkbox";
import { SettingsPageHeader } from "@/components/SettingsPageheader";
import { SettingsItem } from "@/components/SettingsItem";

export default function SessionsSettings() {
  const { send } = useJsonRpc();
  const { hasPermission } = usePermissions();
  const canModifySettings = hasPermission(Permission.SETTINGS_WRITE);

  const {
    requireSessionNickname,
    setRequireSessionNickname,
    requireSessionApproval,
    setRequireSessionApproval,
    maxRejectionAttempts,
    setMaxRejectionAttempts
  } = useSettingsStore();

  const [reconnectGrace, setReconnectGrace] = useState(10);
  const [primaryTimeout, setPrimaryTimeout] = useState(300);
  const [privateKeystrokes, setPrivateKeystrokes] = useState(false);
  const [maxSessions, setMaxSessions] = useState(10);
  const [observerTimeout, setObserverTimeout] = useState(120);

  useEffect(() => {
    send("getSessionSettings", {}, (response: JsonRpcResponse) => {
      if ("error" in response) {
        console.error("Failed to get session settings:", response.error);
      } else {
        const settings = response.result as {
          requireApproval: boolean;
          requireNickname: boolean;
          reconnectGrace?: number;
          primaryTimeout?: number;
          privateKeystrokes?: boolean;
          maxRejectionAttempts?: number;
          maxSessions?: number;
          observerTimeout?: number;
        };
        setRequireSessionApproval(settings.requireApproval);
        setRequireSessionNickname(settings.requireNickname);
        if (settings.reconnectGrace !== undefined) {
          setReconnectGrace(settings.reconnectGrace);
        }
        if (settings.primaryTimeout !== undefined) {
          setPrimaryTimeout(settings.primaryTimeout);
        }
        if (settings.privateKeystrokes !== undefined) {
          setPrivateKeystrokes(settings.privateKeystrokes);
        }
        if (settings.maxRejectionAttempts !== undefined) {
          setMaxRejectionAttempts(settings.maxRejectionAttempts);
        }
        if (settings.maxSessions !== undefined) {
          setMaxSessions(settings.maxSessions);
        }
        if (settings.observerTimeout !== undefined) {
          setObserverTimeout(settings.observerTimeout);
        }
      }
    });
  }, [send, setRequireSessionApproval, setRequireSessionNickname, setMaxRejectionAttempts]);

  const updateSessionSettings = (updates: Partial<{
    requireApproval: boolean;
    requireNickname: boolean;
    reconnectGrace: number;
    primaryTimeout: number;
    privateKeystrokes: boolean;
    maxRejectionAttempts: number;
    maxSessions: number;
    observerTimeout: number;
  }>) => {
    if (!canModifySettings) {
      notify.error("Only the primary session can change this setting");
      return;
    }

    send("setSessionSettings", {
      settings: {
        requireApproval: requireSessionApproval,
        requireNickname: requireSessionNickname,
        reconnectGrace: reconnectGrace,
        primaryTimeout: primaryTimeout,
        privateKeystrokes: privateKeystrokes,
        maxRejectionAttempts: maxRejectionAttempts,
        maxSessions: maxSessions,
        observerTimeout: observerTimeout,
        ...updates
      }
    }, (response: JsonRpcResponse) => {
      if ("error" in response) {
        console.error("Failed to update session settings:", response.error);
        notify.error("Failed to update session settings");
      }
    });
  };

  return (
    <div className="space-y-6">
      <SettingsPageHeader
        title="Multi-Session Access"
        description="Configure multi-session access and control settings"
      />

      {!canModifySettings && (
        <Card className="border-amber-500/20 bg-amber-50 dark:bg-amber-900/10">
          <div className="p-4 text-sm text-amber-700 dark:text-amber-400">
            <strong>Note:</strong> Only the primary session can modify these settings.
            Request primary control to change settings.
          </div>
        </Card>
      )}

      <Card>
        <div className="p-6 space-y-6">
          <div className="flex items-center gap-3 mb-4">
            <UserGroupIcon className="h-5 w-5 text-slate-600 dark:text-slate-400" />
            <h3 className="text-base font-semibold text-slate-900 dark:text-white">
              Access Control
            </h3>
          </div>

          <SettingsItem
            title="Require Session Approval"
            description="New sessions must be approved by the primary session before gaining access"
          >
            <Checkbox
              checked={requireSessionApproval}
              disabled={!canModifySettings}
              onChange={e => {
                const newValue = e.target.checked;
                setRequireSessionApproval(newValue);
                updateSessionSettings({ requireApproval: newValue });
                notify.success(
                  newValue
                    ? "New sessions will require approval"
                    : "New sessions will be automatically approved"
                );
              }}
            />
          </SettingsItem>

          <SettingsItem
            title="Require Session Nicknames"
            description="All sessions must provide a nickname for identification"
          >
            <Checkbox
              checked={requireSessionNickname}
              disabled={!canModifySettings}
              onChange={e => {
                const newValue = e.target.checked;
                setRequireSessionNickname(newValue);
                updateSessionSettings({ requireNickname: newValue });
                notify.success(
                  newValue
                    ? "Session nicknames are now required"
                    : "Session nicknames are now optional"
                );
              }}
            />
          </SettingsItem>

          <SettingsItem
            title="Maximum Rejection Attempts"
            description="Number of times a denied session can re-request approval before the modal is hidden"
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min="1"
                max="10"
                value={maxRejectionAttempts}
                disabled={!canModifySettings}
                onChange={e => {
                  const newValue = parseInt(e.target.value) || 3;
                  if (newValue < 1 || newValue > 10) {
                    notify.error("Maximum attempts must be between 1 and 10");
                    return;
                  }
                  setMaxRejectionAttempts(newValue);
                  updateSessionSettings({ maxRejectionAttempts: newValue });
                  notify.success(
                    `Denied sessions can now retry up to ${newValue} time${newValue === 1 ? '' : 's'}`
                  );
                }}
                className="w-20 px-2 py-1.5 border rounded-md bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-600 text-slate-900 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed text-sm"
              />
              <span className="text-sm text-slate-600 dark:text-slate-400">attempts</span>
            </div>
          </SettingsItem>

          <SettingsItem
            title="Reconnect Grace Period"
            description="Time to wait for a session to reconnect before reassigning control"
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min="5"
                max="60"
                value={reconnectGrace}
                disabled={!canModifySettings}
                onChange={e => {
                  const newValue = parseInt(e.target.value) || 10;
                  if (newValue < 5 || newValue > 60) {
                    notify.error("Grace period must be between 5 and 60 seconds");
                    return;
                  }
                  setReconnectGrace(newValue);
                  updateSessionSettings({ reconnectGrace: newValue });
                  notify.success(
                    `Session will have ${newValue} seconds to reconnect`
                  );
                }}
                className="w-20 px-2 py-1.5 border rounded-md bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-600 text-slate-900 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed text-sm"
              />
              <span className="text-sm text-slate-600 dark:text-slate-400">seconds</span>
            </div>
          </SettingsItem>

          <SettingsItem
            title="Primary Session Timeout"
            description="Time of inactivity before the primary session loses control (0 = disabled)"
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min="0"
                max="3600"
                step="60"
                value={primaryTimeout}
                disabled={!canModifySettings}
                onChange={e => {
                  const newValue = parseInt(e.target.value) || 0;
                  if (newValue < 0 || newValue > 3600) {
                    notify.error("Timeout must be between 0 and 3600 seconds");
                    return;
                  }
                  setPrimaryTimeout(newValue);
                  updateSessionSettings({ primaryTimeout: newValue });
                  notify.success(
                    newValue === 0
                      ? "Primary session timeout disabled"
                      : `Primary session will timeout after ${Math.round(newValue / 60)} minutes of inactivity`
                  );
                }}
                className="w-24 px-2 py-1.5 border rounded-md bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-600 text-slate-900 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed text-sm"
              />
              <span className="text-sm text-slate-600 dark:text-slate-400">seconds</span>
            </div>
          </SettingsItem>

          <SettingsItem
            title="Maximum Concurrent Sessions"
            description="Maximum number of sessions that can connect simultaneously"
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min="1"
                max="20"
                value={maxSessions}
                disabled={!canModifySettings}
                onChange={e => {
                  const newValue = parseInt(e.target.value) || 10;
                  if (newValue < 1 || newValue > 20) {
                    notify.error("Max sessions must be between 1 and 20");
                    return;
                  }
                  setMaxSessions(newValue);
                  updateSessionSettings({ maxSessions: newValue });
                  notify.success(
                    `Maximum concurrent sessions set to ${newValue}`
                  );
                }}
                className="w-20 px-2 py-1.5 border rounded-md bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-600 text-slate-900 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed text-sm"
              />
              <span className="text-sm text-slate-600 dark:text-slate-400">sessions</span>
            </div>
          </SettingsItem>

          <SettingsItem
            title="Observer Cleanup Timeout"
            description="Time to wait before cleaning up inactive observer sessions with closed connections"
          >
            <div className="flex items-center gap-2">
              <input
                type="number"
                min="30"
                max="600"
                step="30"
                value={observerTimeout}
                disabled={!canModifySettings}
                onChange={e => {
                  const newValue = parseInt(e.target.value) || 120;
                  if (newValue < 30 || newValue > 600) {
                    notify.error("Timeout must be between 30 and 600 seconds");
                    return;
                  }
                  setObserverTimeout(newValue);
                  updateSessionSettings({ observerTimeout: newValue });
                  notify.success(
                    `Observer cleanup timeout set to ${Math.round(newValue / 60)} minute${Math.round(newValue / 60) === 1 ? '' : 's'}`
                  );
                }}
                className="w-20 px-2 py-1.5 border rounded-md bg-white dark:bg-slate-800 border-slate-300 dark:border-slate-600 text-slate-900 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed text-sm"
              />
              <span className="text-sm text-slate-600 dark:text-slate-400">seconds</span>
            </div>
          </SettingsItem>

          <SettingsItem
            title="Private Keystrokes"
            description="When enabled, only the primary session can see keystroke events"
          >
            <Checkbox
              checked={privateKeystrokes}
              disabled={!canModifySettings}
              onChange={e => {
                const newValue = e.target.checked;
                setPrivateKeystrokes(newValue);
                updateSessionSettings({ privateKeystrokes: newValue });
                notify.success(
                  newValue
                    ? "Keystrokes are now private to primary session"
                    : "Keystrokes are visible to all authorized sessions"
                );
              }}
            />
          </SettingsItem>
        </div>
      </Card>

      <Card>
        <div className="p-6">
          <div className="space-y-4">
            <h3 className="text-base font-semibold text-slate-900 dark:text-white">
              How Multi-Session Access Works
            </h3>
            <div className="space-y-3 text-sm text-slate-600 dark:text-slate-400">
              <div className="flex items-start gap-2">
                <span className="font-medium text-slate-700 dark:text-slate-300">Primary:</span>
                <span>Full control over the KVM device including keyboard, mouse, and settings</span>
              </div>
              <div className="flex items-start gap-2">
                <span className="font-medium text-slate-700 dark:text-slate-300">Observer:</span>
                <span>View-only access to monitor activity without control capabilities</span>
              </div>
              <div className="flex items-start gap-2">
                <span className="font-medium text-slate-700 dark:text-slate-300">Pending:</span>
                <span>Awaiting approval from the primary session (when approval is required)</span>
              </div>
            </div>
            <div className="pt-2 text-sm text-slate-500 dark:text-slate-400">
              Use the Sessions panel in the top navigation bar to view and manage active sessions.
            </div>
          </div>
        </div>
      </Card>
    </div>
  );
}