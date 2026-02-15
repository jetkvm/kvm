import { useCallback, useState } from "react";

export function useCopyToClipboard(resetInterval = 2000) {
  const [isCopied, setIsCopied] = useState(false);

  const copy = useCallback(
    async (text: string) => {
      if (!text) return false;

      let success = false;

      if (navigator.clipboard && window.isSecureContext) {
        try {
          await navigator.clipboard.writeText(text);
          success = true;
        } catch (err) {
          console.warn("Clipboard API failed:", err);
        }
      }

      // Fallback for insecure contexts
      if (!success) {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();

        try {
          success = document.execCommand("copy");
        } catch (err) {
          console.error("Fallback copy failed:", err);
          success = false;
        } finally {
          document.body.removeChild(textarea);
        }
      }

      setIsCopied(success);
      if (success && resetInterval > 0) {
        setTimeout(() => setIsCopied(false), resetInterval);
      }

      return success;
    },
    [resetInterval],
  );

  return { copy, isCopied };
}
