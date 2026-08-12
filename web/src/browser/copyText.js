export async function copyText(text, {
  navigatorObject = globalThis.navigator,
  documentObject = globalThis.document,
} = {}) {
  const value = typeof text === "string" ? text : "";
  if (!value) return false;

  try {
    if (typeof navigatorObject?.clipboard?.writeText === "function") {
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
  previousFocus?.focus?.();
  return copied;
}
