import { useState, useEffect, useCallback } from "react";
import {
  UserGroupIcon,
  ArrowPathIcon,
  PencilIcon,
} from "@heroicons/react/20/solid";
import clsx from "clsx";

import { useSessionStore, useSharedSessionStore } from "@/stores/sessionStore";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import SessionControlPanel from "@/components/SessionControlPanel";
import NicknameModal from "@/components/NicknameModal";
import SessionsList, { SessionModeBadge } from "@/components/SessionsList";
import { sessionApi } from "@/api/sessionApi";

export default function SessionPopover() {
  const {
    currentSessionId,
    currentMode,
    sessions,
    sessionError,
    setSessions,
  } = useSessionStore();
  const { setNickname } = useSharedSessionStore();

  const [isRefreshing, setIsRefreshing] = useState(false);
  const [showNicknameModal, setShowNicknameModal] = useState(false);
  const [editingSessionId, setEditingSessionId] = useState<string | null>(null);

  const { send } = useJsonRpc();

  // Adapter function to match existing callback pattern
  const sendRpc = useCallback((method: string, params: Record<string, unknown>, callback?: (response: { result?: unknown; error?: { message: string } }) => void) => {
    send(method, params, (response) => {
      if (callback) callback(response);
    });
  }, [send]);

  const handleRefresh = async () => {
    if (isRefreshing) return;

    setIsRefreshing(true);
    try {
      const refreshedSessions = await sessionApi.getSessions(sendRpc);
      setSessions(refreshedSessions);
    } catch (error) {
      console.error("Failed to refresh sessions:", error);
    } finally {
      setIsRefreshing(false);
    }
  };

  // Fetch sessions on mount
  useEffect(() => {
    if (sessions.length === 0) {
      sessionApi.getSessions(sendRpc)
        .then(sessions => setSessions(sessions))
        .catch(error => console.error("Failed to fetch sessions:", error));
    }
  }, [sendRpc, sessions.length, setSessions]);

  return (
    <div className="w-full rounded-lg bg-white dark:bg-slate-800 shadow-lg border border-slate-200 dark:border-slate-700">
      {/* Header */}
      <div className="p-4 border-b border-slate-200 dark:border-slate-700">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <UserGroupIcon className="h-5 w-5 text-slate-600 dark:text-slate-400" />
            <h3 className="text-sm font-semibold text-slate-900 dark:text-white">
              Session Management
            </h3>
          </div>
          <button
            className="p-1 rounded hover:bg-slate-100 dark:hover:bg-slate-700 disabled:opacity-50"
            onClick={handleRefresh}
            disabled={isRefreshing}
          >
            <ArrowPathIcon className={clsx("h-4 w-4 text-slate-600 dark:text-slate-400", isRefreshing && "animate-spin")} />
          </button>
        </div>
      </div>

      {/* Session Error */}
      {sessionError && (
        <div className="p-3 bg-red-50 dark:bg-red-900/10 border-b border-red-200 dark:border-red-800">
          <p className="text-xs text-red-700 dark:text-red-400">{sessionError}</p>
        </div>
      )}

      {/* Current Session */}
      <div className="p-4 border-b border-slate-200 dark:border-slate-700">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-xs font-medium text-slate-500 dark:text-slate-400">Your Session</span>
              <button
                onClick={() => {
                  setEditingSessionId(currentSessionId);
                  setShowNicknameModal(true);
                }}
                className="p-1 hover:bg-slate-100 dark:hover:bg-slate-700 rounded transition-colors"
                title="Edit nickname"
              >
                <PencilIcon className="h-3 w-3 text-slate-500 dark:text-slate-400" />
              </button>
            </div>
            <SessionModeBadge mode={currentMode || "unknown"} />
          </div>

          {currentSessionId && (
            <>
              {/* Display current session nickname if exists */}
              {sessions.find(s => s.id === currentSessionId)?.nickname && (
                <div className="flex items-center justify-between text-xs">
                  <span className="text-slate-600 dark:text-slate-400">Nickname:</span>
                  <span className="font-medium text-slate-900 dark:text-white">
                    {sessions.find(s => s.id === currentSessionId)?.nickname}
                  </span>
                </div>
              )}

              <div className="mt-3">
                <SessionControlPanel sendFn={sendRpc} />
              </div>
            </>
          )}
        </div>
      </div>

      {/* Active Sessions List */}
      <div className="p-4 max-h-64 overflow-y-auto">
        <div className="mb-2 text-xs font-medium text-slate-500 dark:text-slate-400">
          Active Sessions ({sessions.length})
        </div>

        {sessions.length > 0 ? (
          <SessionsList
            sessions={sessions}
            currentSessionId={currentSessionId || undefined}
            onEditNickname={(sessionId) => {
              setEditingSessionId(sessionId);
              setShowNicknameModal(true);
            }}
            onApprove={(sessionId) => {
              sendRpc("approveNewSession", { sessionId }, (response) => {
                if (response.error) {
                  console.error("Failed to approve session:", response.error);
                } else {
                  handleRefresh();
                }
              });
            }}
            onDeny={(sessionId) => {
              sendRpc("denyNewSession", { sessionId }, (response) => {
                if (response.error) {
                  console.error("Failed to deny session:", response.error);
                } else {
                  handleRefresh();
                }
              });
            }}
            onTransfer={async (sessionId) => {
              try {
                await sessionApi.transferPrimary(sendRpc, currentSessionId!, sessionId);
                handleRefresh();
              } catch (error) {
                console.error("Failed to transfer primary:", error);
              }
            }}
          />
        ) : (
          <p className="text-xs text-slate-500 dark:text-slate-400">No active sessions</p>
        )}
      </div>

      <NicknameModal
        isOpen={showNicknameModal}
        title={editingSessionId === currentSessionId
          ? (sessions.find(s => s.id === currentSessionId)?.nickname ? "Update Your Nickname" : "Set Your Nickname")
          : `Set Nickname for ${sessions.find(s => s.id === editingSessionId)?.mode || 'Session'}`}
        description={editingSessionId === currentSessionId
          ? "Choose a nickname to help identify your session to others"
          : "Choose a nickname to help identify this session"}
        onSubmit={async (nickname) => {
          if (editingSessionId && sendRpc) {
            try {
              await sessionApi.updateNickname(sendRpc, editingSessionId, nickname);
              if (editingSessionId === currentSessionId) {
                setNickname(nickname);
              }
              setShowNicknameModal(false);
              setEditingSessionId(null);
              handleRefresh();
            } catch (error) {
              console.error("Failed to update nickname:", error);
              throw error;
            }
          }
        }}
        onSkip={() => {
          setShowNicknameModal(false);
          setEditingSessionId(null);
        }}
      />
    </div>
  );
}

