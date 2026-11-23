(async () => {
  const DEFAULT_PORT = 3778;
  const PROBE_TIMEOUT_MS = 1500;
  const POLL_INTERVAL_MS = 2500;
  const SILENT_GRACE_MS = 140;
  const RECOVERY_CMD = "agymate service install && agymate service restart";

  let port = DEFAULT_PORT;
  let theme = "";
  let tokens = null;
  let service = "";
  let inFlight = false;
  let entered = false;
  let pollTimer = null;
  let attempts = 0;

  const body = document.body;
  const root = document.documentElement;
  const saidEl = document.getElementById("said");
  const helpEl = document.getElementById("boot-help");
  const addrEl = document.getElementById("boot-addr");
  const badgeTextEl = document.getElementById("boot-badge-text");
  const cmdEl = document.getElementById("boot-cmd");
  const copyBtn = document.getElementById("boot-copy");
  const copyLabelEl = document.getElementById("boot-copy-label");
  const retryBtn = document.getElementById("boot-retry");
  const retryLabelEl = document.getElementById("boot-retry-label");

  function applyMagpieTheme(t) {
    if (!t || typeof t !== "object") return;
    if (typeof t.dark === "boolean") {
      theme = t.dark ? "dark" : "light";
      root.dataset.theme = theme;
    }
    if (t.tokens && typeof t.tokens === "object") {
      tokens = t.tokens;
    }
  }

  function encodeTokensParam(rawTokens) {
    if (!rawTokens || typeof rawTokens !== "object") return "";
    const slim = {};
    for (const [k, v] of Object.entries(rawTokens)) {
      const s = String(v ?? "").trim();
      if (s) slim[k] = s;
    }
    if (!Object.keys(slim).length) return "";
    try {
      const bytes = new TextEncoder().encode(JSON.stringify(slim));
      let bin = "";
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      return btoa(bin);
    } catch {
      return "";
    }
  }

  function createBaseUrlObject() {
    const rawBase = service || "http://127.0.0.1:" + port + "/";
    const u = new URL(rawBase, location.href);
    if (!u.pathname.endsWith("/")) {
      u.pathname += "/";
    }
    return u;
  }

  function buildEndpointUrl(subPath, extraParams) {
    const u = createBaseUrlObject();
    if (subPath) {
      const cleanSub = String(subPath).replace(/^\/+/, "");
      u.pathname = u.pathname + cleanSub;
    }
    if (extraParams && typeof extraParams === "object") {
      for (const [k, v] of Object.entries(extraParams)) {
        if (v !== undefined && v !== null && v !== "") {
          u.searchParams.set(k, String(v));
        }
      }
    }
    return u.href;
  }

  function buildTargetUrl() {
    const params = { embed: "magpie" };
    if (theme) params.theme = theme;
    const encodedTokens = encodeTokensParam(tokens);
    if (encodedTokens) params.tokens = encodedTokens;
    return buildEndpointUrl("", params);
  }

  function displayEndpoint() {
    try {
      const u = createBaseUrlObject();
      const pathPart = u.pathname === "/" ? "" : u.pathname.replace(/\/$/, "");
      return u.protocol + "//" + u.host + pathPart;
    } catch {
      return "http://127.0.0.1:" + port;
    }
  }

  async function fetchWithTimeout(url, timeoutMs) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      return await fetch(url, {
        method: "GET",
        cache: "no-store",
        credentials: "omit",
        headers: { Accept: "application/json, text/html;q=0.8" },
        signal: controller.signal,
      });
    } finally {
      clearTimeout(timer);
    }
  }

  function deriveHttpServiceProxyBase() {
    if (service) {
      const u = createBaseUrlObject();
      if (u.protocol === "http:" || u.protocol === "https:") {
        return u.href;
      }
      return "";
    }
    try {
      const baseUrl = new URL(".", document.baseURI || location.href);
      if (baseUrl.protocol !== "http:" && baseUrl.protocol !== "https:") {
        return "";
      }
      const m = baseUrl.pathname.match(/^\/_magpie\/plugin\/([^/]+)(?:\/|$)/);
      if (m && m[1]) {
        return new URL("/_magpie/service/" + m[1] + "/", baseUrl).href;
      }
    } catch {
      /* ignore */
    }
    return "";
  }

  function probeImageWithTimeout(url, timeoutMs) {
    return new Promise((resolve) => {
      const img = new Image();
      let settled = false;
      const done = (ok) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        img.onload = null;
        img.onerror = null;
        resolve(ok);
      };
      const timer = setTimeout(() => done(false), timeoutMs);
      img.onload = () => done(true);
      img.onerror = () => done(false);
      img.src = url;
    });
  }

  let cspBlockedConnect = false;
  window.addEventListener("securitypolicyviolation", (e) => {
    if (e.effectiveDirective === "connect-src" || e.violatedDirective === "connect-src") {
      cspBlockedConnect = true;
    }
  });

  async function checkHubAlive() {
    try {
      const healthRes = await fetchWithTimeout(buildEndpointUrl("api/health"), PROBE_TIMEOUT_MS);
      if (healthRes.ok) return true;
      if (healthRes.status === 404) {
        const overviewRes = await fetchWithTimeout(buildEndpointUrl("api/overview"), PROBE_TIMEOUT_MS);
        if (overviewRes.ok) return true;
        if (overviewRes.status === 404) {
          const rootRes = await fetchWithTimeout(buildEndpointUrl(""), PROBE_TIMEOUT_MS);
          return rootRes.ok;
        }
      }
      return false;
    } catch {
      // Magpie plugin pages enforce `connect-src 'none'`, blocking fetch() even when Hub is online.
      // When served over HTTP(S) (e.g. magpie web), probe via `img-src`-allowed `/_magpie/service/`.
      const proxyBase = deriveHttpServiceProxyBase();
      if (proxyBase) {
        try {
          const probeUrl = new URL("logo.svg", proxyBase);
          probeUrl.searchParams.set("_probe", String(Date.now()));
          return await probeImageWithTimeout(probeUrl.href, PROBE_TIMEOUT_MS);
        } catch {
          return false;
        }
      }
      // On desktop `wails://` without an HTTP service proxy, `connect-src 'none'` prevents
      // pre-flight probing; proceed to direct Hub navigation instead of looping offline forever.
      await new Promise((r) => setTimeout(r, 20));
      return cspBlockedConnect;
    }
  }

  function renderOfflineState(isManualRetry) {
    if (entered) return;
    const epUrl = displayEndpoint();
    const epShort = epUrl.replace(/^https?:\/\//, "");
    if (addrEl) addrEl.textContent = epUrl;
    if (cmdEl) cmdEl.textContent = RECOVERY_CMD;

    if (isManualRetry) {
      body.dataset.state = "retrying";
      if (badgeTextEl) badgeTextEl.textContent = "正在重连…";
      if (saidEl) saidEl.textContent = "正在尝试连接 AgyMate Hub (" + epShort + ")…";
      if (retryBtn) retryBtn.disabled = true;
      if (retryLabelEl) retryLabelEl.textContent = "正在重连…";
      return;
    }

    body.dataset.state = "offline";
    if (badgeTextEl) badgeTextEl.textContent = "本地服务未启动";
    if (saidEl) saidEl.textContent = "无法连接到 AgyMate Hub (" + epShort + ")";
    if (helpEl) {
      helpEl.dataset.tip =
        attempts > 1
          ? "在终端执行下方命令启动服务；每 2.5 秒自动重连（已检测 " + attempts + " 次）"
          : "在终端执行下方命令启动服务；检测到服务在线后自动进入控制台";
    }
    if (retryBtn) retryBtn.disabled = false;
    if (retryLabelEl) retryLabelEl.textContent = "立即重连";
  }

  function scheduleNextPoll() {
    if (entered) return;
    if (pollTimer) clearTimeout(pollTimer);
    pollTimer = setTimeout(() => {
      if (document.visibilityState === "hidden" || window.magpie?.visible === false) {
        scheduleNextPoll();
        return;
      }
      void attemptConnect(false);
    }, POLL_INTERVAL_MS);
  }

  async function attemptConnect(isManual) {
    if (entered || inFlight) return;
    inFlight = true;
    attempts += 1;

    let graceTimer = null;
    if (attempts === 1) {
      graceTimer = setTimeout(() => {
        if (!entered && body.dataset.state === "silent") {
          body.dataset.state = "probing";
          if (badgeTextEl) badgeTextEl.textContent = "正在连接…";
          if (addrEl) addrEl.textContent = displayEndpoint();
        }
      }, SILENT_GRACE_MS);
    } else if (isManual) {
      renderOfflineState(true);
    }

    const ok = await checkHubAlive();
    if (graceTimer) clearTimeout(graceTimer);
    inFlight = false;

    if (ok) {
      entered = true;
      if (pollTimer) clearTimeout(pollTimer);
      if (body.dataset.state !== "silent") {
        body.dataset.state = "ready";
        if (badgeTextEl) badgeTextEl.textContent = "已连接";
        if (saidEl) saidEl.textContent = "已连接 AgyMate Hub，正在进入控制台…";
      }
      location.replace(buildTargetUrl());
      return;
    }

    renderOfflineState(false);
    scheduleNextPoll();
  }

  let copyResetTimer = null;
  async function copyRecoveryCommand() {
    let copied = false;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(RECOVERY_CMD);
        copied = true;
      }
    } catch {
      copied = false;
    }
    if (!copied) {
      try {
        const ta = document.createElement("textarea");
        ta.value = RECOVERY_CMD;
        ta.setAttribute("readonly", "");
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        copied = document.execCommand("copy");
        document.body.removeChild(ta);
      } catch {
        copied = false;
      }
    }
    if (copyBtn && copyLabelEl) {
      copyBtn.dataset.copied = copied ? "true" : "false";
      copyLabelEl.textContent = copied ? "✓ 已复制" : "请手动复制";
      if (copyResetTimer) clearTimeout(copyResetTimer);
      copyResetTimer = setTimeout(() => {
        copyBtn.dataset.copied = "false";
        copyLabelEl.textContent = "复制命令";
      }, 1800);
    }
  }

  if (copyBtn) {
    copyBtn.addEventListener("click", () => {
      void copyRecoveryCommand();
    });
  }

  if (retryBtn) {
    retryBtn.addEventListener("click", () => {
      void attemptConnect(true);
    });
  }

  try {
    const initialTheme = await Promise.race([
      Promise.resolve(window.magpie?.ready),
      new Promise((resolve) => setTimeout(() => resolve(null), 300)),
    ]);
    applyMagpieTheme(initialTheme || window.magpie?.theme);
  } catch {
    /* standalone fallback */
  }

  try {
    window.magpie?.onTheme?.((nextTheme) => {
      applyMagpieTheme(nextTheme);
    });
  } catch {
    /* ignore */
  }

  try {
    const s = window.magpie?.service;
    if (typeof s === "string" && s.trim()) {
      const resolved = new URL(s, location.href);
      if (resolved.protocol === "http:" || resolved.protocol === "https:") {
        service = resolved.href;
      }
    }
  } catch {
    /* standalone desktop without service proxy */
  }

  try {
    const o = await Promise.race([
      Promise.resolve(window.magpie?.options?.get?.()),
      new Promise((resolve) => setTimeout(() => resolve(null), 300)),
    ]);
    const p = Number(o?.port);
    if (Number.isFinite(p) && p >= 1 && p <= 65535) {
      port = Math.trunc(p);
    }
  } catch {
    /* keep default port */
  }

  if (addrEl) addrEl.textContent = displayEndpoint();

  const onWake = (visible) => {
    if (visible !== false && !entered && !inFlight) {
      void attemptConnect(false);
    }
  };
  try {
    window.magpie?.onVisibility?.(onWake);
    window.magpie?.onAction?.(() => onWake(true));
  } catch {
    /* ignore */
  }
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") onWake(true);
  });

  await attemptConnect(false);
})();
