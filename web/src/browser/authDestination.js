export function preopenAuthDestination(openWindow = globalThis.open) {
  if (typeof openWindow !== "function") return null;
  try {
    const destination = openWindow("", "codex-commons-authorization");
    if (!destination) return null;
    try {
      destination.document.title = "Opening Codex sign-in…";
      destination.opener = null;
    } catch {
      destination.close?.();
      return null;
    }
    return destination;
  } catch {
    return null;
  }
}

export function navigateAuthDestination(destination, verificationURL) {
  if (!destination || destination.closed || typeof verificationURL !== "string" || !verificationURL) return false;
  try {
    destination.location.replace(verificationURL);
    return true;
  } catch {
    destination.close?.();
    return false;
  }
}
