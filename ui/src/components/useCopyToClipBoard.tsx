import { useCallback, useState } from "react";

// Insecure origins have no navigator.clipboard, so the copy goes through the
// legacy command. It must not move focus: pages that keep a focus trap armed
// reclaim focus from a temporary input and the selection is gone before the
// copy runs. Selecting a detached span through a Range never moves focus, and
// overriding the copy event makes the payload independent of the selection.
function copyViaCopyEvent(text: string) {
  const span = document.createElement("span");
  span.textContent = text;
  span.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none;white-space:pre";
  document.body.appendChild(span);

  const selection = window.getSelection();
  const restore = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;
  const range = document.createRange();
  range.selectNodeContents(span);
  selection?.removeAllRanges();
  selection?.addRange(range);

  let copied = false;
  const onCopy = (event: ClipboardEvent) => {
    event.clipboardData?.setData("text/plain", text);
    event.preventDefault();
    copied = true;
  };

  document.addEventListener("copy", onCopy, true);
  try {
    document.execCommand("copy");
  } catch (err) {
    console.error("Fallback copy failed:", err);
  } finally {
    document.removeEventListener("copy", onCopy, true);
    selection?.removeAllRanges();
    if (restore) selection?.addRange(restore);
    span.remove();
  }

  // execCommand reports success even when nothing reached the clipboard; the
  // copy event firing is the only trustworthy signal.
  return copied;
}

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
        success = copyViaCopyEvent(text);
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
