export async function copyText(text, {
  navigatorObject = globalThis.navigator,
  documentObject = globalThis.document,
  isSecureContext = globalThis.isSecureContext,
} = {}) {
  const value = typeof text === "string" ? text : "";
  if (!value) return false;

  try {
    if (isSecureContext !== false && typeof navigatorObject?.clipboard?.writeText === "function") {
      await navigatorObject.clipboard.writeText(value);
      return true;
    }
  } catch {
    // LAN HTTP is not a secure browser context in Chromium. Fall through to
    // the synchronous selection-based copy path instead of failing silently.
  }

  if (!documentObject?.body || typeof documentObject.createElement !== "function") return false;
  const textarea = documentObject.createElement("textarea");
  const previousFocus = documentObject.activeElement;
  const previousInputSelection = previousFocus && typeof previousFocus.selectionStart === "number"
    ? {
        start: previousFocus.selectionStart,
        end: previousFocus.selectionEnd,
        direction: previousFocus.selectionDirection,
      }
    : null;
  const selection = documentObject.getSelection?.();
  const previousRanges = selection
    ? Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index).cloneRange())
    : [];
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.inset = "0 auto auto -9999px";
  textarea.style.opacity = "0";
  documentObject.body.appendChild(textarea);
  textarea.select();
  textarea.setSelectionRange?.(0, value.length);
  let copied = false;
  try {
    copied = documentObject.execCommand?.("copy") === true;
  } catch {
    copied = false;
  }
  textarea.remove();
  previousFocus?.focus?.({ preventScroll: true });
  if (previousInputSelection && typeof previousFocus?.setSelectionRange === "function") {
    previousFocus.setSelectionRange(previousInputSelection.start, previousInputSelection.end, previousInputSelection.direction);
  }
  if (selection) {
    selection.removeAllRanges();
    previousRanges.forEach((range) => selection.addRange(range));
  }
  return copied;
}

export function manualCopyShortcut(navigatorObject = globalThis.navigator) {
  const platform = navigatorObject?.userAgentData?.platform || navigatorObject?.platform || "";
  return /mac|iphone|ipad|ipod/i.test(platform) ? "Command+C" : "Ctrl+C";
}
