import { access, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";

const MAX_PROTOCOL_BYTES = 8 * 1024 * 1024;
const MAX_FINAL_SNAPSHOT_BYTES = 2 * 1024 * 1024;
const CDP_COMMAND_TIMEOUT_MS = 10000;
const CHROME_CANDIDATES = [process.env.CHROME_BIN, "/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"].filter(Boolean);
const ISOLATED_PROCESS_GROUP = process.platform !== "win32";

function withTimeout(promise, timeoutMs, label) {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`${label} timed out after ${timeoutMs}ms`)), timeoutMs);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}

async function chromeBinary() {
  for (const candidate of CHROME_CANDIDATES) {
    try {
      await access(candidate);
      return candidate;
    } catch {}
  }
  throw new Error("Chrome was not found. Set CHROME_BIN to the user-chosen Chrome executable.");
}

function waitForFile(path, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const inspect = async () => {
      try {
        const value = (await readFile(path, "utf8")).trim();
        if (value) return resolve(value);
      } catch {}
      if (Date.now() >= deadline) return reject(new Error("Chrome did not expose a DevTools endpoint"));
      setTimeout(inspect, 50);
    };
    inspect();
  });
}

function cdpClient(url) {
  const socket = new WebSocket(url);
  const pending = new Map();
  let sequence = 0;
  let receivedBytes = 0;
  let protocolError = null;
  let openedSettled = false;
  const failPending = (error) => {
    protocolError ||= error;
    for (const request of pending.values()) {
      clearTimeout(request.timer);
      request.reject(protocolError);
    }
    pending.clear();
  };
  const opened = new Promise((resolve, reject) => {
    socket.addEventListener("open", () => {
      openedSettled = true;
      resolve();
    }, { once: true });
    socket.addEventListener("error", () => {
      const error = new Error("Chrome DevTools WebSocket failed to open");
      if (!openedSettled) reject(error);
      failPending(error);
    }, { once: true });
  });
  socket.addEventListener("message", (event) => {
    receivedBytes += Buffer.byteLength(String(event.data));
    if (receivedBytes > MAX_PROTOCOL_BYTES) {
      failPending(new Error("Chrome DevTools output exceeded 8 MiB"));
      socket.close();
      return;
    }
    let message;
    try {
      message = JSON.parse(String(event.data));
    } catch {
      failPending(new Error("Chrome DevTools returned malformed JSON"));
      socket.close();
      return;
    }
    if (!message.id || !pending.has(message.id)) return;
    const { resolve, reject, timer } = pending.get(message.id);
    clearTimeout(timer);
    pending.delete(message.id);
    if (message.error) reject(new Error(message.error.message || "Chrome DevTools command failed"));
    else resolve(message.result || {});
  });
  socket.addEventListener("close", () => failPending(new Error("Chrome DevTools WebSocket closed")));
  return {
    async send(method, params = {}) {
      await withTimeout(opened, CDP_COMMAND_TIMEOUT_MS, "Chrome DevTools connection");
      if (protocolError) throw protocolError;
      const id = ++sequence;
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error(`Chrome DevTools command ${method} timed out`));
        }, CDP_COMMAND_TIMEOUT_MS);
        pending.set(id, { resolve, reject, timer });
        try {
          socket.send(JSON.stringify({ id, method, params }));
        } catch (error) {
          clearTimeout(timer);
          pending.delete(id);
          reject(error);
        }
      });
    },
    close() {
      failPending(new Error("Chrome DevTools client closed"));
      socket.close();
    },
  };
}

export async function inspectChromePage({ url, timeoutMs = 30000, chromeArgs = [], screenshotPath = "", viewport = null, media = null, pageScaleFactor = 1 }) {
  const chrome = await chromeBinary();
  const profile = await mkdtemp(join(tmpdir(), "commons-chrome-gate-"));
  const child = spawn(chrome, [
    "--headless=new",
    "--disable-gpu",
    "--disable-dev-shm-usage",
    "--disable-background-networking",
    "--disable-default-apps",
    "--disable-extensions",
    "--disable-sync",
    "--metrics-recording-only",
    "--mute-audio",
    "--no-default-browser-check",
    "--no-first-run",
    "--remote-debugging-port=0",
    "--remote-allow-origins=http://127.0.0.1",
    "--window-size=1440,900",
    `--user-data-dir=${profile}`,
    ...chromeArgs,
    "about:blank",
  ], { detached: ISOLATED_PROCESS_GROUP, stdio: ["ignore", "ignore", "pipe"] });
  let stderr = "";
  child.stderr.on("data", (chunk) => {
    if (Buffer.byteLength(stderr) < 64 * 1024) stderr += chunk.toString("utf8");
  });
  const childClosed = new Promise((resolve) => {
    child.once("close", (code, signal) => resolve({ code, signal }));
  });
  let devToolsReady = false;
  const spawnFailure = new Promise((_, reject) => {
    child.once("error", (error) => reject(new Error(`Chrome failed to start: ${error.message}`)));
    child.once("exit", (code, signal) => {
      if (!devToolsReady) reject(new Error(`Chrome exited before DevTools was ready (${code ?? signal ?? "unknown"})`));
    });
  });
  let browser;
  let client;
  let cleanupPromise;
  const signalChrome = (signal) => {
    try {
      if (ISOLATED_PROCESS_GROUP && child.pid) process.kill(-child.pid, signal);
      else child.kill(signal);
    } catch (error) {
      if (error?.code !== "ESRCH") throw error;
    }
  };
  const cleanupChrome = () => {
    if (cleanupPromise) return cleanupPromise;
    cleanupPromise = (async () => {
      client?.close();
      browser?.close();
      signalChrome("SIGTERM");
      let closed = await Promise.race([
        childClosed.then((result) => ({ closed: true, result })),
        new Promise((resolve) => setTimeout(() => resolve({ closed: false }), 2000)),
      ]);
      if (!closed.closed) {
        signalChrome("SIGKILL");
        closed = await Promise.race([
          childClosed.then((result) => ({ closed: true, result })),
          new Promise((resolve) => setTimeout(() => resolve({ closed: false }), 2000)),
        ]);
      }
      await rm(profile, { recursive: true, force: true });
      if (!closed.closed) throw new Error("Chrome release-gate process group did not exit after SIGKILL");
      if (!client && closed.result?.code && closed.result.code !== 0) throw new Error(`Chrome exited ${closed.result.code}: ${stderr.slice(-2000)}`);
    })();
    return cleanupPromise;
  };
  let terminating = false;
  const terminateForSignal = (signal, exitCode) => {
    if (terminating) return;
    terminating = true;
    const deadline = setTimeout(() => {
      try { signalChrome("SIGKILL"); } finally { process.exit(exitCode); }
    }, 5000);
    void cleanupChrome().finally(() => {
      clearTimeout(deadline);
      process.exit(exitCode);
    });
  };
  const onSigint = () => terminateForSignal("SIGINT", 130);
  const onSigterm = () => terminateForSignal("SIGTERM", 143);
  process.once("SIGINT", onSigint);
  process.once("SIGTERM", onSigterm);
  try {
    const endpoint = await Promise.race([
      waitForFile(join(profile, "DevToolsActivePort"), 10000),
      spawnFailure,
    ]);
    devToolsReady = true;
    const [port, browserPath] = endpoint.split(/\r?\n/);
    browser = cdpClient(`ws://127.0.0.1:${port}${browserPath}`);
    const { targetId } = await browser.send("Target.createTarget", { url });
    const response = await fetch(`http://127.0.0.1:${port}/json/list`, {
      signal: AbortSignal.timeout(CDP_COMMAND_TIMEOUT_MS),
    });
    if (!response.ok) throw new Error(`Chrome target list returned HTTP ${response.status}`);
    const targets = await withTimeout(response.json(), 10000, "Chrome target list JSON");
    const page = targets.find((target) => target.id === targetId);
    if (!page?.webSocketDebuggerUrl) throw new Error("Chrome did not expose the release-gate page target");
    client = cdpClient(page.webSocketDebuggerUrl);
    await client.send("Runtime.enable");
    if (viewport) {
      const browserZoom = pageScaleFactor === 1 ? 1 : pageScaleFactor;
      await client.send("Emulation.setDeviceMetricsOverride", {
        width: Math.max(1, Math.floor(viewport.width / browserZoom)),
        height: Math.max(1, Math.floor(viewport.height / browserZoom)),
        deviceScaleFactor: viewport.deviceScaleFactor || browserZoom,
        mobile: false,
      });
    }
    if (media) {
      await client.send("Emulation.setEmulatedMedia", {
        media: "",
        features: [
          ...(media.colorScheme ? [{ name: "prefers-color-scheme", value: media.colorScheme }] : []),
          ...(media.reducedMotion ? [{ name: "prefers-reduced-motion", value: "reduce" }] : []),
        ],
      });
    }
    const deadline = Date.now() + timeoutMs;
    let snapshot;
    while (Date.now() < deadline) {
      const result = await client.send("Runtime.evaluate", {
        expression: `document.getElementById("gate-status")?.dataset.result || ""`,
        returnByValue: true,
      });
      const ready = result.result?.value || "";
      if (ready) {
        const finalResult = await client.send("Runtime.evaluate", {
          expression: `(() => { const dialog = document.querySelector(".archaeology-dialog[open]"); const rect = dialog?.getBoundingClientRect(); return ({ ready: document.getElementById("gate-status")?.dataset.result || "", html: document.documentElement.outerHTML, text: document.body?.innerText || "", reducedMotion: matchMedia("(prefers-reduced-motion: reduce)").matches, colorScheme: matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light", metrics: { clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth, clientHeight: document.documentElement.clientHeight, scrollHeight: document.documentElement.scrollHeight, visualWidth: visualViewport?.width || innerWidth, dialogLeft: rect?.left ?? null, dialogRight: rect?.right ?? null, activeLabel: document.activeElement?.getAttribute?.("aria-label") || document.activeElement?.textContent?.trim?.() || "" } }); })()`,
          returnByValue: true,
        });
        snapshot = finalResult.result?.value;
        if (!snapshot || Buffer.byteLength(snapshot.html || "") + Buffer.byteLength(snapshot.text || "") > MAX_FINAL_SNAPSHOT_BYTES) {
          throw new Error("Chrome release-gate final snapshot exceeded 2 MiB");
        }
        if (screenshotPath) {
          const capture = await client.send("Page.captureScreenshot", { format: "png", fromSurface: true });
          await writeFile(screenshotPath, Buffer.from(capture.data, "base64"));
        }
        return snapshot;
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    throw new Error("Chrome release gate timed out without PASS/FAIL. Last status: starting");
  } finally {
    process.removeListener("SIGINT", onSigint);
    process.removeListener("SIGTERM", onSigterm);
    await cleanupChrome();
  }
}
