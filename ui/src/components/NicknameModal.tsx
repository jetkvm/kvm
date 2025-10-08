import { useState, useEffect, useRef } from "react";
import { Dialog, DialogPanel, DialogBackdrop } from "@headlessui/react";
import { UserIcon, XMarkIcon } from "@heroicons/react/20/solid";
import { Button } from "./Button";
import { useSettingsStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useRTCStore } from "@/hooks/stores";
import { generateNickname } from "@/utils/nicknameGenerator";

type SessionRole = "primary" | "observer" | "queued" | "pending";

interface NicknameModalProps {
  isOpen: boolean;
  onSubmit: (nickname: string) => void | Promise<void>;
  onSkip?: () => void;
  title?: string;
  description?: string;
  isRequired?: boolean;
  expectedRole?: SessionRole;
}

export default function NicknameModal({
  isOpen,
  onSubmit,
  onSkip,
  title = "Set Your Session Nickname",
  description = "Add a nickname to help identify your session to other users",
  isRequired,
  expectedRole = "observer"
}: NicknameModalProps) {
  const [nickname, setNickname] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [generatedNickname, setGeneratedNickname] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const { requireSessionNickname } = useSettingsStore();
  const { send } = useJsonRpc();
  const { rpcDataChannel } = useRTCStore();

  const isNicknameRequired = isRequired ?? requireSessionNickname;

  // Role-based color coding
  const getRoleColors = (role: SessionRole) => {
    switch (role) {
      case "primary":
        return {
          bg: "bg-green-100 dark:bg-green-900/30",
          icon: "text-green-600 dark:text-green-400"
        };
      case "observer":
        return {
          bg: "bg-blue-100 dark:bg-blue-900/30",
          icon: "text-blue-600 dark:text-blue-400"
        };
      case "queued":
        return {
          bg: "bg-yellow-100 dark:bg-yellow-900/30",
          icon: "text-yellow-600 dark:text-yellow-400"
        };
      case "pending":
        return {
          bg: "bg-orange-100 dark:bg-orange-900/30",
          icon: "text-orange-600 dark:text-orange-400"
        };
      default:
        return {
          bg: "bg-slate-100 dark:bg-slate-900/30",
          icon: "text-slate-600 dark:text-slate-400"
        };
    }
  };

  const roleColors = getRoleColors(expectedRole);

  // Generate nickname when modal opens and RPC is ready
  useEffect(() => {
    if (!isOpen || generatedNickname) return;
    if (rpcDataChannel?.readyState !== "open") return;

    generateNickname(send).then(nickname => {
      setGeneratedNickname(nickname);
    }).catch((error) => {
      console.error('Backend nickname generation failed:', error);
    });
  }, [isOpen, generatedNickname, rpcDataChannel?.readyState, send]);

  // Focus input when modal opens
  useEffect(() => {
    if (isOpen) {
      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus();
        }
      }, 100);
    }
  }, [isOpen]);

  const validateNickname = (value: string): string | null => {
    if (value.length < 2) {
      return "Nickname must be at least 2 characters";
    }
    if (value.length > 30) {
      return "Nickname must be 30 characters or less";
    }
    if (!/^[a-zA-Z0-9\s\-_.@]+$/.test(value)) {
      return "Nickname can only contain letters, numbers, spaces, and - _ . @";
    }
    return null;
  };

  const handleSubmit = async (e?: React.FormEvent) => {
    e?.preventDefault();

    // Use generated nickname if input is empty
    const trimmedNickname = nickname.trim() || generatedNickname;

    // Validate
    const validationError = validateNickname(trimmedNickname);
    if (validationError) {
      setError(validationError);
      return;
    }

    setIsSubmitting(true);
    setError(null);

    try {
      await onSubmit(trimmedNickname);
      setNickname("");
      setGeneratedNickname(""); // Reset generated nickname after successful submit
    } catch (error: any) {
      setError(error.message || "Failed to set nickname");
      setIsSubmitting(false);
    }
  };

  const handleSkip = () => {
    if (!isNicknameRequired && onSkip) {
      onSkip();
      setNickname("");
      setError(null);
      setGeneratedNickname(""); // Reset generated nickname when skipping
    }
  };

  return (
    <Dialog
      open={isOpen}
      onClose={() => {
        if (!isNicknameRequired && onSkip) {
          onSkip();
          setNickname("");
          setError(null);
          setGeneratedNickname("");
        }
      }}
      className="relative z-50"
    >
      <DialogBackdrop className="fixed inset-0 bg-black/50" />
      <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
        <DialogPanel className="bg-white dark:bg-slate-800 rounded-lg shadow-xl max-w-md w-full">
        <div className="p-6 space-y-4">
          <div className="flex items-start justify-between">
            <div className="flex items-center gap-3">
              <div className={`p-2 ${roleColors.bg} rounded-lg`}>
                <UserIcon className={`h-6 w-6 ${roleColors.icon}`} />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-slate-900 dark:text-white">
                  {title}
                </h3>
                <p className="text-sm text-slate-600 dark:text-slate-400">
                  {description}
                </p>
              </div>
            </div>
            {!isNicknameRequired && (
              <button
                onClick={handleSkip}
                className="p-1 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg transition-colors"
                aria-label="Close"
              >
                <XMarkIcon className="h-5 w-5 text-slate-500 dark:text-slate-400" />
              </button>
            )}
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label htmlFor="nickname" className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">
                Nickname
              </label>
              <input
                ref={inputRef}
                id="nickname"
                type="text"
                value={nickname}
                onChange={(e) => {
                  setNickname(e.target.value);
                  setError(null);
                }}
                placeholder={generatedNickname || "e.g., John's Laptop, Office PC, etc."}
                className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md
                         bg-white dark:bg-slate-700 text-slate-900 dark:text-white
                         placeholder-slate-400 dark:placeholder-slate-500
                         focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                maxLength={30}
              />
              <div className="mt-1 flex justify-between items-center">
                {error ? (
                  <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
                ) : (
                  <div className="space-y-1">
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                      {nickname.trim() === "" && generatedNickname
                        ? `Leave empty to use: ${generatedNickname}`
                        : "2-30 characters, letters, numbers, spaces, and - _ . @ allowed"}
                    </p>
                  </div>
                )}
                <span className="text-xs text-slate-500 dark:text-slate-400">
                  {nickname.length}/30
                </span>
              </div>
            </div>

            {isNicknameRequired && (
              <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3">
                <p className="text-sm text-amber-800 dark:text-amber-300">
                  <strong>Required:</strong> A nickname is required by the administrator to help identify sessions.
                </p>
              </div>
            )}

            <div className="flex gap-3">
              <Button
                type="submit"
                theme="primary"
                size="MD"
                text="Set Nickname"
                fullWidth
                disabled={isSubmitting}
              />
              {!isNicknameRequired && (
                <Button
                  type="button"
                  onClick={handleSkip}
                  theme="light"
                  size="MD"
                  text="Skip"
                  fullWidth
                  disabled={isSubmitting}
                />
              )}
            </div>
          </form>
        </div>
        </DialogPanel>
      </div>
    </Dialog>
  );
}