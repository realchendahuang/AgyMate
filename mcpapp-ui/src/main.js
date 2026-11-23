import {
  App,
  applyDocumentTheme,
  applyHostStyleVariables,
} from "@modelcontextprotocol/ext-apps";

const rootEl = document.getElementById("app");

const state = {
  view: "dashboard",
  tab: "overview",
  currency: "USD",
  lang: "auto",
  preset: "today",
  mode: "std",
  instanceFilter: "all",
  searchQuery: "",
  hubUrl: "http://127.0.0.1:3778",
  hubOnline: false,
  overview: null,
  telemetry: null,
  sessions: null,
  doctor: null,
  cache: null,
  loading: false,
  actionBusy: false,
  toast: "",
  toastErr: false,
  canOpenLinks: false,
};

const app = new App({ name: "AgyMate", version: "0.5.1" }, {});

function resolveActiveLang() {
  const pref = String(state.lang || "auto").toLowerCase();
  if (pref === "zh" || pref.startsWith("zh-")) return "zh";
  if (pref === "en" || pref.startsWith("en-")) return "en";
  const nav = (navigator.language || "en").toLowerCase();
  return nav.startsWith("zh") ? "zh" : "en";
}

const EN_EXACT = {
  "Hub 实时": "Live",
  "直连模式": "Direct",
  "Web 控制台": "Web Console",
  "刷新": "Refresh",
  "刷新中…": "Refreshing…",
  "总览": "Overview",
  "账单": "Billing",
  "会话": "Sessions",
  "实例": "Profiles",
  "体检": "Doctor",
  "今日消费": "Today",
  "缓存节省": "Cache Saved",
  "本月回本": "Monthly ROI",
  "本月累计": "This Month",
  "客户端在线": "Online",
  "待确认": "Waiting",
  "运行中": "Running",
  "在线空闲": "Idle",
  "空闲": "Idle",
  "离线": "Offline",
  "缓存": "Cached",
  "主客户端": "Main",
  "IDE客户端": "IDE",
  "隔离沙盒": "Isolated",
  "外部实例": "External",
  "凭证有效": "Valid",
  "钥匙串托管": "Keychain",
  "访问令牌过期": "Expired",
  "无刷新令牌": "No Refresh",
  "未登录": "Signed Out",
  "不适用": "—",
  "即将重置": "Resetting",
  "不足1分钟": "<1m",
  "暂无配额数据": "No quota data",
  "加载中…": "Loading…",
  "暂无账号": "No accounts",
  "自动修复": "Auto Fix",
  "今日": "Today",
  "昨日": "Yesterday",
  "近5小时": "5h",
  "近7天": "7d",
  "本月": "Month",
  "全部": "All",
  "标准": "Std",
  "优惠": "Promo",
  "无缓存": "No Cache",
  "搜索": "Search",
  "搜索会话…": "Search sessions…",
  "聚焦": "Focus",
  "启动": "Launch",
  "停止": "Stop",
  "自愈": "Heal",
  "进行中": "Active",
  "历史检索": "History",
  "暂无进行中会话": "No active sessions",
  "暂无历史会话": "No sessions",
  "暂无实例": "No profiles",
  "环境体检": "Health Check",
  "总占用": "Total",
  "本地缓存": "Local Cache",
  "控制台": "Console",
  "在线": "Online",
  "运行": "Running",
  "上下文": "Context",
  "重置": "Reset",
  "正常": "Healthy",
  "警告": "Warning",
  "异常": "Error",
  "未开 CDP": "No CDP",
  "离线快照": "Cached snapshot",
  "操作完成": "Done",
  "查询失败": "Query failed",
};

function translateMcpText(raw) {
  if (!raw || !/[\u4e00-\u9fff]/.test(raw)) return raw;
  const trimmed = raw.trim();
  let out = EN_EXACT[trimmed];
  if (!out) {
    out = trimmed
      .replace(/^([\d,]+) 次$/, "$1 calls")
      .replace(/^(\d+) 账号$/, "$1 accounts")
      .replace(/^命中 ([\d.]+%)$/, "Hit $1")
      .replace(/^Gemini 周$/, "Gemini W")
      .replace(/^Claude 周$/, "Claude W")
      .replace(/^Gemini 日$/, "Gemini D")
      .replace(/^Claude 日$/, "Claude D")
      .replace(/^(\d+) 次调用 · (.+) tok$/, "$1 calls · $2 tok")
      .replace(/^命中率 ([\d.]+%)$/, "Hit rate $1")
      .replace(/^本月 (.+) \/ (.+)$/, "Month $1 / $2")
      .replace(/^(\d+) 个 Google 账号$/, "$1 Google account(s)")
      .replace(/^(\d+) 条记录$/, "$1 items")
      .replace(/^上下文水位/, "Context")
      .replace(/^环境健康体检/, "Health Audit")
      .replace(/受上级窗口限制，暂不适用/g, "Limited by parent quota window")
      .replace(/后台守护服务未连接，已自动切换直连采集。恢复实时守护请运行/g, "Local Hub daemon offline; using direct mode. To restore, run")
      .replace(/可安全回收（保留最近 (\d+) 条）：/g, "Reclaimable (keeping latest $1): ")
      .replace(/后重置/g, " to reset")
      .replace(/预计\s*([\d.]+h)\s*耗尽/g, "empty in ~$1")
      .replace(/离线快照/g, "Cached snapshot")
      .replace(/槽位:/g, "Slots:")
      .replace(/今日 /g, "Today ")
      .replace(/5小时/g, "5h")
      .replace(/周配额/g, "Weekly")
      .replace(/天/g, "d ")
      .replace(/小时/g, "h ")
      .replace(/分钟/g, "m")
      .replace(/分/g, "m");
  }
  const lead = raw.match(/^\s*/)[0];
  const trail = raw.match(/\s*$/)[0];
  return lead + out + trail;
}

function localizeMcpDOM(root) {
  if (!root || resolveActiveLang() !== "en") return;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
  let node;
  while ((node = walker.nextNode())) {
    if (node.nodeType === 3) {
      const parent = node.parentElement;
      if (parent && (parent.tagName === "CODE" || parent.tagName === "SCRIPT" || parent.tagName === "STYLE")) continue;
      const next = translateMcpText(node.nodeValue || "");
      if (next !== node.nodeValue) node.nodeValue = next;
    } else if (node.nodeType === 1) {
      for (const attr of ["title", "placeholder"]) {
        if (node.hasAttribute(attr)) {
          const cur = node.getAttribute(attr) || "";
          const next = translateMcpText(cur);
          if (next !== cur) node.setAttribute(attr, next);
        }
      }
    }
  }
}

function applyHostCtx(ctx) {
  if (!ctx) return;
  if (ctx.theme) applyDocumentTheme(ctx.theme);
  if (ctx.styles?.variables) applyHostStyleVariables(ctx.styles.variables);
}

function esc(v) {
  return String(v ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function fmtTokens(n) {
  const v = Number(n || 0);
  if (v >= 1e9) return (v / 1e9).toFixed(2) + "B";
  if (v >= 1e6) return (v / 1e6).toFixed(2) + "M";
  if (v >= 1e3) return (v / 1e3).toFixed(1) + "K";
  return String(Math.round(v));
}

function fmtBytes(b) {
  const v = Number(b || 0);
  if (v >= 1024 * 1024 * 1024) return (v / (1024 * 1024 * 1024)).toFixed(2) + " GB";
  if (v >= 1024 * 1024) return (v / (1024 * 1024)).toFixed(1) + " MB";
  if (v >= 1024) return (v / 1024).toFixed(1) + " KB";
  return v + " B";
}

function fmtMoney(usd, pricing) {
  const p = pricing || state.overview?.pricing || { currency: state.currency, usdToCny: 7.2 };
  const cur = state.currency || p.currency || "USD";
  const val = Number(usd || 0);
  if (String(cur).toUpperCase() === "CNY") {
    const rate = Number(p.usdToCny || 7.2) || 7.2;
    const cny = val * rate;
    if (cny >= 1000) return "¥" + Math.round(cny).toLocaleString("en-US");
    if (cny >= 100) return "¥" + cny.toFixed(1);
    return "¥" + cny.toFixed(2);
  }
  if (val >= 1000) return "$" + Math.round(val).toLocaleString("en-US");
  if (val >= 100) return "$" + val.toFixed(1);
  return "$" + val.toFixed(2);
}

function fmtResetLeft(resetUnix) {
  const ts = Number(resetUnix || 0);
  if (ts <= 0) return "";
  const diff = ts - Math.floor(Date.now() / 1000);
  if (diff <= 0) return "即将重置";
  if (diff < 60) return "不足1分钟";
  const mins = Math.floor(diff / 60);
  const hrs = Math.floor(mins / 60);
  const days = Math.floor(hrs / 24);
  if (days > 0) {
    const remH = hrs % 24;
    return remH > 0 ? `${days}天${remH}小时` : `${days}天`;
  }
  if (hrs > 0) {
    const remM = mins % 60;
    return remM > 0 ? `${hrs}小时${remM}分` : `${hrs}小时`;
  }
  return `${mins}分钟`;
}

function fmtResetShort(resetUnix) {
  const ts = Number(resetUnix || 0);
  if (ts <= 0) return "";
  const diff = ts - Math.floor(Date.now() / 1000);
  if (diff <= 0) return "重置";
  const mins = Math.floor(diff / 60);
  const hrs = Math.floor(mins / 60);
  const days = Math.floor(hrs / 24);
  if (days > 0) return `${days}d`;
  if (hrs > 0) return `${hrs}h`;
  return `${Math.max(1, mins)}m`;
}

function shortPlan(plan) {
  const raw = String(plan || "").trim();
  if (!raw) return "";
  const low = raw.toLowerCase();
  if (low.includes("ultra")) return "Ultra";
  if (low.includes("pro")) return "Pro";
  if (low.includes("team")) return "Team";
  if (low.includes("starter")) return "Starter";
  if (low.includes("free")) return "Free";
  return raw.replace(/^google\s+ai\s+/i, "").replace(/\s+quota$/i, "").trim();
}

function shortBucketLabel(b) {
  const grpRaw = String(b.groupName || b.displayName || "").toLowerCase();
  const winRaw = String(b.window || b.displayName || "").toLowerCase();
  const grp = grpRaw.includes("gemini")
    ? "Gemini"
    : grpRaw.includes("claude") || grpRaw.includes("gpt") || grpRaw.includes("3p")
      ? "Claude"
      : String(b.groupName || "").replace(/\s+models$/i, "").trim();
  const win = winRaw.includes("5h") || winRaw.includes("5小时") || winRaw.includes("five hour") || winRaw.includes("5-hour")
    ? "5h"
    : winRaw.includes("week") || winRaw.includes("7d") || winRaw.includes("周")
      ? "周"
      : winRaw.includes("day") || winRaw.includes("daily") || winRaw.includes("日")
        ? "日"
        : "";
  if (grp && win) return `${grp} ${win}`;
  return grp || win || "配额";
}

function liveStateBadge(liveState) {
  switch (String(liveState || "").toLowerCase()) {
    case "waiting_approval":
      return '<span class="badge warn"><i class="b-dot"></i>待确认</span>';
    case "running":
      return '<span class="badge ok"><i class="b-dot"></i>运行</span>';
    case "idle":
      return '<span class="badge ok"><i class="b-dot"></i>空闲</span>';
    default:
      return '<span class="badge">离线</span>';
  }
}

function kindLabel(kind) {
  switch (kind) {
    case "main":
      return "主客户端";
    case "ide":
      return "IDE客户端";
    case "profile":
      return "隔离沙盒";
    default:
      return "外部实例";
  }
}

function authLabel(auth) {
  switch (auth) {
    case "ok":
      return '<span class="badge ok">凭证有效</span>';
    case "keychain_managed":
      return '<span class="badge info">钥匙串托管</span>';
    case "expired":
      return '<span class="badge warn">访问令牌过期</span>';
    case "no_refresh_token":
      return '<span class="badge warn">无刷新令牌</span>';
    default:
      return '<span class="badge">未登录</span>';
  }
}

function showToast(msg, isErr = false) {
  state.toast = msg;
  state.toastErr = isErr;
  render();
}

function applyStructuredPayload(sc) {
  if (!sc || typeof sc !== "object") return;
  if (sc.view === "inline" || sc.view === "dashboard") {
    state.view = sc.view;
  }
  if (sc.tab) state.tab = sc.tab;
  if (sc.hubUrl) state.hubUrl = sc.hubUrl;
  if (typeof sc.hubOnline === "boolean") state.hubOnline = sc.hubOnline;
  if (sc.overview) {
    state.overview = sc.overview;
    if (sc.overview.pricing?.currency) {
      state.currency = sc.overview.pricing.currency;
    }
    if (sc.overview.language && state.lang === "auto") {
      state.lang = sc.overview.language;
    }
  }
  if (sc.telemetry) state.telemetry = sc.telemetry;
  if (sc.sessions) state.sessions = sc.sessions;
  if (sc.doctor) state.doctor = sc.doctor;
  if (sc.cache) state.cache = sc.cache;
}

async function querySection(section, extra = {}) {
  state.loading = true;
  render();
  try {
    const res = await app.callServerTool({
      name: "app_query",
      arguments: { section, ...extra },
    });
    if (res?.isError) {
      const txt = res?.content?.[0]?.text || "查询失败";
      showToast(txt, true);
      return;
    }
    applyStructuredPayload(res?.structuredContent);
  } catch (err) {
    showToast("调用失败：" + (err?.message || String(err)), true);
  } finally {
    state.loading = false;
    render();
  }
}

async function runAction(action, extra = {}) {
  if (state.actionBusy) return;
  state.actionBusy = true;
  render();
  try {
    const res = await app.callServerTool({
      name: "app_action",
      arguments: { action, ...extra },
    });
    const txt = res?.content?.[0]?.text || "操作完成";
    if (res?.isError) {
      showToast(txt, true);
    } else {
      applyStructuredPayload(res?.structuredContent);
      showToast(txt, false);
    }
  } catch (err) {
    showToast("操作异常：" + (err?.message || String(err)), true);
  } finally {
    state.actionBusy = false;
    render();
  }
}

const RING_C = 2 * Math.PI * 23; // ~144.513

function renderRingItem({ pct, label, sub = "", subWarn = false, tone = "", tip = "" }) {
  const p = Math.max(0, Math.min(100, Number(pct || 0)));
  const offset = (RING_C * (1 - p / 100)).toFixed(2);
  const arcCls = [tone, p <= 0.1 ? "empty" : ""].filter(Boolean).join(" ");
  const pctCls = tone === "danger" || tone === "warn" ? tone : "";
  return `
    <div class="ring-item" title="${esc(tip)}">
      <div class="ring-wrap">
        <svg class="ring-svg" viewBox="0 0 56 56" aria-hidden="true">
          <circle class="ring-track" cx="28" cy="28" r="23"></circle>
          <circle class="ring-arc ${arcCls}" cx="28" cy="28" r="23" stroke-dasharray="${RING_C.toFixed(2)}" stroke-dashoffset="${offset}"></circle>
        </svg>
        <span class="ring-pct ${pctCls}">${p.toFixed(0)}%</span>
      </div>
      <div class="ring-meta">
        <span class="ring-lbl">${esc(label)}</span>
        ${sub ? `<span class="ring-sub${subWarn ? " warn" : ""}">${esc(sub)}</span>` : ""}
      </div>
    </div>`;
}

function renderQuotaRings(buckets, ctx, cachedQuota) {
  const winRank = (b) => {
    const w = String(b.window || b.displayName || "").toLowerCase();
    if (w.includes("5h") || w.includes("5小时") || w.includes("five hour") || w.includes("5-hour")) return 1;
    if (w.includes("day") || w.includes("daily")) return 2;
    if (w.includes("week") || w.includes("7d") || w.includes("周")) return 3;
    return 4;
  };
  const active = (buckets || [])
    .filter((b) => !b.disabled)
    .slice()
    .sort((a, b) => {
      const ga = String(a.groupName || "");
      const gb = String(b.groupName || "");
      if (ga !== gb) return 0;
      return winRank(a) - winRank(b);
    });
  const items = active.slice(0, 4).map((b) => {
    const pct = Math.max(0, Math.min(100, Number(b.remainingPct || 0)));
    const exhaustSoon = Boolean(b.exhaustsBeforeReset && b.hoursUntilEmpty > 0);
    const tone = pct <= 15 ? "danger" : (pct <= 35 || exhaustSoon) ? "warn" : "";
    const resetLong = fmtResetLeft(b.resetUnix);
    const resetShort = fmtResetShort(b.resetUnix);
    const sub = exhaustSoon
      ? `~${b.hoursUntilEmpty < 10 ? b.hoursUntilEmpty.toFixed(1) : Math.round(b.hoursUntilEmpty)}h`
      : resetShort;
    const tipParts = [b.displayName || shortBucketLabel(b), `${pct.toFixed(0)}%`];
    if (resetLong) tipParts.push(resetLong === "即将重置" ? resetLong : `${resetLong}后重置`);
    else if (cachedQuota) tipParts.push("离线快照");
    if (exhaustSoon) tipParts.push(`预计 ${b.hoursUntilEmpty.toFixed(1)}h 耗尽`);
    return renderRingItem({
      pct,
      label: shortBucketLabel(b),
      sub,
      subWarn: exhaustSoon,
      tone,
      tip: tipParts.join(" · "),
    });
  });

  if (ctx) {
    const cpct = Math.max(0, Math.min(100, Number(ctx.usagePct || 0)));
    const tone = cpct >= 80 ? "danger" : cpct >= 60 ? "warn" : "ctx";
    const tip = [
      ctx.model || "上下文",
      `${cpct.toFixed(1)}%`,
      `${fmtTokens(ctx.estimatedTokensUsed)} / ${fmtTokens(ctx.maxContextTokens)}`,
    ].join(" · ");
    items.push(
      renderRingItem({
        pct: cpct,
        label: "上下文",
        sub: fmtTokens(ctx.estimatedTokensUsed),
        tone,
        tip,
      })
    );
  }

  return items.length ? `<div class="ring-row">${items.join("")}</div>` : '<div class="sub-text">暂无配额数据</div>';
}

function renderOverviewTab(isInline) {
  const ov = state.overview;
  if (!ov) {
    return '<div class="empty">加载中…</div>';
  }
  const pricing = ov.pricing;
  const sum = ov.todaySummary || {};
  const roi = ov.monthRoi || {};
  const accounts = ov.accounts || [];
  const insts = ov.instances || [];
  const runningCount = insts.filter((i) => i.running).length;

  const roiTxt = roi.subUsd > 0 ? `${Number(roi.roiMultiple || 0).toFixed(1)}×` : fmtMoney(roi.allMonthUsd, pricing);
  const roiSub = roi.subUsd > 0 ? `${fmtMoney(roi.allMonthUsd, pricing)} / ${fmtMoney(roi.subUsd, pricing)}` : "本月累计";

  const kpisHTML = `
    <div class="kpis">
      <div class="kpi">
        <div class="kpi-k">今日消费</div>
        <div class="kpi-v">${esc(fmtMoney(sum.totalUsd, pricing))}</div>
        <div class="kpi-sub">${(sum.calls || 0).toLocaleString()} 次</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">缓存节省</div>
        <div class="kpi-v">${esc(fmtMoney(sum.savedByCacheUsd, pricing))}</div>
        <div class="kpi-sub">命中 ${Number(sum.cacheHitRate || 0).toFixed(1)}%</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">${roi.subUsd > 0 ? "本月回本" : "本月累计"}</div>
        <div class="kpi-v">${esc(roiTxt)}</div>
        <div class="kpi-sub">${esc(roiSub)}</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">在线</div>
        <div class="kpi-v">${runningCount} / ${insts.length}</div>
        <div class="kpi-sub">${accounts.length} 账号</div>
      </div>
    </div>`;

  const shownAccounts = isInline ? accounts.slice(0, 4) : accounts;
  const prefixCounts = {};
  shownAccounts.forEach((a) => {
    const p = String(a.displayName || "").split("@")[0].toLowerCase();
    prefixCounts[p] = (prefixCounts[p] || 0) + 1;
  });
  const shortAcctName = (full) => {
    const s = String(full || "");
    if (!s.includes("@")) return s;
    const p = s.split("@")[0];
    return prefixCounts[p.toLowerCase()] === 1 ? p : s;
  };

  const acctCards = shownAccounts.length
    ? `<div class="grid">${shownAccounts
        .map((acct) => {
          const isOnline = (acct.onlineCount || 0) > 0;
          const sp = shortPlan(acct.planName);
          const plan = sp ? `<span class="plan-tag" title="${esc(acct.planName)}">${esc(sp)}</span>` : "";
          const headTip = [acct.displayName, acct.planName, `今日 ${fmtMoney(acct.todayUsd, pricing)}`].filter(Boolean).join(" · ");
          return `
            <article class="card ${isOnline ? "" : "is-offline"}">
              <div class="card-head">
                <div class="card-title" title="${esc(headTip)}">${esc(shortAcctName(acct.displayName))}</div>
                <div class="card-head-right">${plan}${liveStateBadge(acct.liveState)}</div>
              </div>
              ${renderQuotaRings(acct.buckets, acct.context, acct.cachedQuota)}
            </article>`;
        })
        .join("")}</div>`
    : '<div class="empty">暂无账号</div>';

  let colabHTML = "";
  const colab = ov.colab;
  if (colab && colab.enabled) {
    const cBadge = colab.assignmentsCount > 0 ? `<span class="badge warn">运行 ${colab.assignmentsCount}</span>` : '<span class="badge ok">空闲</span>';
    colabHTML = `
      <div class="banner">
        <div>
          <strong>Colab</strong> · <strong>${Number(colab.currentBalance || 0).toFixed(1)} CCU</strong>
          · ${Number(colab.consumptionRateHourly || 0).toFixed(2)}/h
        </div>
        ${cBadge}
      </div>`;
  }

  return kpisHTML + acctCards + colabHTML;
}

function renderTelemetryTab() {
  const rep = state.telemetry;
  if (!rep) {
    return '<div class="empty">正在加载账单数据…</div>';
  }
  const pricing = state.overview?.pricing;
  const sum = rep.summary || {};
  const presets = [
    ["today", "今日"],
    ["yesterday", "昨日"],
    ["last_5h", "近5小时"],
    ["last_7d", "近7天"],
    ["this_month", "本月"],
    ["all", "全部"],
  ];
  const modes = [
    ["std", "标准+缓存"],
    ["promo", "优惠+缓存"],
    ["nc_std", "标准无缓存"],
  ];

  const filterBar = `
    <div class="filter-bar">
      ${presets
        .map(([k, lbl]) => `<button type="button" class="chip-btn ${state.preset === k ? "active" : ""}" data-preset="${k}">${lbl}</button>`)
        .join("")}
      <span style="flex:1"></span>
      ${modes
        .map(([k, lbl]) => `<button type="button" class="chip-btn ${state.mode === k ? "active" : ""}" data-mode="${k}">${lbl}</button>`)
        .join("")}
    </div>`;

  const kpis = `
    <div class="kpis">
      <div class="kpi">
        <div class="kpi-k">${esc(rep.presetLabel || "当前区间")}账单</div>
        <div class="kpi-v">${esc(fmtMoney(sum.totalUsd, pricing))}</div>
        <div class="kpi-sub">${sum.calls || 0} 次调用 · 耗时 ${Number(rep.scanMs || 0).toFixed(0)}ms</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">总 Token 吞吐</div>
        <div class="kpi-v">${fmtTokens(sum.totalTokens)}</div>
        <div class="kpi-sub">输入 ${fmtTokens(sum.inputTokens)} · 输出 ${fmtTokens(sum.outputTokens)}</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">缓存读取节省</div>
        <div class="kpi-v">${esc(fmtMoney(sum.savedByCacheUsd, pricing))}</div>
        <div class="kpi-sub">命中 ${fmtTokens(sum.cacheTokens)} (${Number(sum.cacheHitRate || 0).toFixed(1)}%)</div>
      </div>
    </div>`;

  const models = rep.byModel || [];
  const modelTable = models.length
    ? `<div class="table-wrap"><table>
        <thead><tr><th>模型</th><th class="num">调用</th><th class="num">输入</th><th class="num">缓存</th><th class="num">输出</th><th class="num">等值账单</th></tr></thead>
        <tbody>${models
          .slice(0, 20)
          .map(
            (m) => `<tr>
              <td>${esc(m.modelName)}</td>
              <td class="num">${m.calls}</td>
              <td class="num">${fmtTokens(m.inputTokens)}</td>
              <td class="num">${fmtTokens(m.cacheTokens)}</td>
              <td class="num">${fmtTokens(m.outputTokens)}</td>
              <td class="num">${esc(fmtMoney(m.totalUsd, pricing))}</td>
            </tr>`
          )
          .join("")}</tbody>
      </table></div>`
    : '<div class="empty">该时间段暂无模型调用记录</div>';

  return filterBar + kpis + modelTable;
}

function renderSessionsTab() {
  const data = state.sessions || {};
  const live = data.live || state.overview?.liveSessions || [];
  const hits = data.hits || [];

  const liveHTML = live.length
    ? `<div class="list-wrap">${live
        .slice(0, 8)
        .map(
          (ls) => `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">${esc(ls.title || ls.conversationId)}</div>
            <div class="list-sub"><span class="mono">${esc(ls.instanceId)}</span>${ls.model ? ` · ${esc(ls.model)}` : ""}${ls.workspace ? ` · ${esc(ls.workspace)}` : ""}</div>
          </div>
          ${liveStateBadge(ls.state)}
        </div>`
        )
        .join("")}</div>`
    : '<div class="empty">暂无进行中会话</div>';

  const searchForm = `
    <form class="search-row" id="mcp-search-form">
      <input class="input" id="mcp-search-input" type="search" value="${esc(state.searchQuery)}" placeholder="搜索会话…">
      <button type="submit" class="btn primary">搜索</button>
    </form>`;

  const hitsHTML = hits.length
    ? `<div class="list-wrap">${hits
        .map(
          (h) => `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">${esc(h.title || h.cascadeId)}</div>
            <div class="list-sub"><span class="mono">${esc(h.instanceId)}</span> · ${esc(h.workspace || "-")} · ${h.stepCount || 0} 步</div>
            ${h.snippet ? `<div class="list-sub">${esc(h.snippet)}</div>` : ""}
          </div>
        </div>`
        )
        .join("")}</div>`
    : '<div class="empty">暂无历史会话</div>';

  return `
    <div class="section-head"><span class="section-title">进行中</span></div>
    ${liveHTML}
    <div class="section-head"><span class="section-title">历史检索</span></div>
    ${searchForm}
    ${hitsHTML}`;
}

function renderProfilesTab() {
  const insts = state.overview?.instances || [];
  if (!insts.length) {
    return '<div class="empty">暂无实例</div>';
  }
  return `<div class="list-wrap">${insts
    .map((inst) => {
      const protectedSlot = !!inst.readOnlyLifecycle;
      const acts = [];
      if (inst.running) {
        acts.push(`<button type="button" class="btn ghost" data-act="profile_focus" data-id="${esc(inst.id)}">聚焦</button>`);
      }
      if (!protectedSlot) {
        if (inst.running) {
          acts.push(`<button type="button" class="btn danger" data-act="profile_stop" data-id="${esc(inst.id)}">停止</button>`);
        } else {
          acts.push(`<button type="button" class="btn soft-blue" data-act="profile_launch" data-id="${esc(inst.id)}">启动</button>`);
          acts.push(`<button type="button" class="btn ghost" data-act="profile_heal" data-id="${esc(inst.id)}">自愈</button>`);
        }
      }
      const noCdpHint = protectedSlot
        ? "当前窗口未开启 CDP 调试端口（退出后通过 agymate launch 或 Antigravity (AgyMate).app 启动以启用浮窗与汉化）"
        : "当前实例未开启 CDP 调试端口（停止后重新启动即可启用浮窗与汉化）";
      const cdpBadge = inst.running && !inst.cdpPort
        ? ` <span class="badge warn" title="${esc(noCdpHint)}">未开 CDP</span>`
        : "";
      return `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">${esc(inst.label || inst.id)} <span class="badge">${esc(kindLabel(inst.kind))}</span> ${inst.running ? '<span class="badge ok">运行中</span>' : '<span class="badge">离线</span>'}${cdpBadge}</div>
            <div class="list-sub"><span class="mono">${esc(inst.id)}</span> · ${esc(inst.account || "-")} · ${authLabel(inst.authState)}</div>
          </div>
          <div class="row-acts">${acts.join("")}</div>
        </div>`;
    })
    .join("")}</div>`;
}

function renderDoctorTab() {
  const doc = state.doctor;
  const cache = state.cache;
  if (!doc) {
    return '<div class="empty">加载中…</div>';
  }
  const checksHTML = `<div class="list-wrap">${(doc.checks || [])
    .map((c) => {
      const cls = c.status === "ok" ? "ok" : c.status === "error" ? "danger" : "warn";
      const lbl = c.status === "ok" ? "正常" : c.status === "error" ? "异常" : "警告";
      return `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">${esc(c.title)}</div>
            <div class="list-sub">${esc(c.summary)}</div>
            ${c.detail ? `<div class="list-sub">${esc(c.detail)}</div>` : ""}
          </div>
          <span class="badge ${cls}">${lbl}</span>
        </div>`;
    })
    .join("")}</div>`;

  let cacheHTML = "";
  if (cache) {
    cacheHTML = `
      <div class="banner">
        <div>
          <strong>本地缓存</strong> · ${fmtBytes(cache.totalBytes)} · 可释放 <strong>${fmtBytes(cache.reclaimableBytes)}</strong>
        </div>
        <span class="badge info">${(cache.items || []).length} 条</span>
      </div>`;
  }

  return `
    <div class="section-head">
      <span class="section-title">环境体检 (${esc(doc.mainVersion || "-")}${doc.ideVersion ? ` · ${esc(doc.ideVersion)}` : ""})</span>
      <button type="button" class="btn primary" id="mcp-doc-fix" ${state.actionBusy ? "disabled" : ""}>自动修复</button>
    </div>
    ${checksHTML}
    ${cacheHTML}`;
}

function render() {
  const isInline = state.view === "inline";
  rootEl.className = "shell" + (isInline ? " is-inline" : "");

  const srcBadge = state.hubOnline
    ? '<span class="src-dot" title="Hub 实时"></span>'
    : '<span class="src-dot fallback" title="直连模式"></span>';

  const openWebBtn = state.canOpenLinks
    ? `<button type="button" class="btn" id="mcp-open-web">控制台</button>`
    : "";

  const curNext = String(state.currency).toUpperCase() === "CNY" ? "USD" : "CNY";
  const activeLang = resolveActiveLang();
  const topbar = `
    <header class="topbar">
      <div class="brand-row">
        <svg class="brand-mark" viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <rect x="3.25" y="3.25" width="17.5" height="17.5" rx="4" stroke="currentColor" stroke-width="1.75"/>
          <path d="M3.5 9.25H20.5M10.25 9.25V20.5" stroke="currentColor" stroke-width="1.65" stroke-linecap="round"/>
          <rect x="6" y="12.25" width="2" height="2" rx="0.6" fill="currentColor"/>
          <path d="M13.25 13.25H17.5M13.25 16.5H16.25" stroke="currentColor" stroke-width="1.65" stroke-linecap="round"/>
        </svg>
        <span class="brand-title">AgyMate</span>
        ${srcBadge}
      </div>
      <div class="actions">
        <button type="button" class="btn ghost" id="mcp-lang-btn">${activeLang === "en" ? "中" : "EN"}</button>
        <button type="button" class="btn ghost" id="mcp-cur-btn">${String(state.currency).toUpperCase() === "CNY" ? "¥" : "$"}</button>
        <button type="button" class="btn ghost" id="mcp-refresh-btn" ${state.loading ? "disabled" : ""}>${state.loading ? "刷新中…" : "刷新"}</button>
        ${openWebBtn}
      </div>
    </header>`;

  const toastHTML = state.toast ? `<div class="toast ${state.toastErr ? "err" : ""}">${esc(state.toast)}</div>` : "";

  if (isInline) {
    rootEl.innerHTML = topbar + toastHTML + renderOverviewTab(true);
    localizeMcpDOM(rootEl);
    bindEvents(curNext);
    return;
  }

  const tabs = [
    ["overview", "总览"],
    ["telemetry", "账单"],
    ["sessions", "会话"],
    ["profiles", "实例"],
    ["doctor", "体检"],
  ];
  const tabsHTML = `
    <nav class="tabs" role="tablist">
      ${tabs
        .map(([k, lbl]) => `<button type="button" class="tab-btn ${state.tab === k ? "active" : ""}" data-tab="${k}">${lbl}</button>`)
        .join("")}
    </nav>`;

  let bodyHTML = "";
  switch (state.tab) {
    case "telemetry":
      bodyHTML = renderTelemetryTab();
      break;
    case "sessions":
      bodyHTML = renderSessionsTab();
      break;
    case "profiles":
      bodyHTML = renderProfilesTab();
      break;
    case "doctor":
      bodyHTML = renderDoctorTab();
      break;
    default:
      bodyHTML = renderOverviewTab(false);
      break;
  }

  const offlineHint = !state.hubOnline
    ? `<div class="banner"><span>后台守护服务未连接，已自动切换直连采集。恢复实时守护请运行 <code class="mono">agymate service restart</code></span></div>`
    : "";

  rootEl.innerHTML = topbar + tabsHTML + toastHTML + bodyHTML + offlineHint;
  localizeMcpDOM(rootEl);
  bindEvents(curNext);
}

function bindEvents(curNext) {
  const langBtn = document.getElementById("mcp-lang-btn");
  if (langBtn) {
    langBtn.addEventListener("click", () => {
      state.lang = resolveActiveLang() === "en" ? "zh" : "en";
      render();
    });
  }
  const refreshBtn = document.getElementById("mcp-refresh-btn");
  if (refreshBtn) {
    refreshBtn.addEventListener("click", () => {
      if (state.tab === "telemetry") {
        void querySection("telemetry", { preset: state.preset, mode: state.mode, instance: state.instanceFilter });
      } else if (state.tab === "sessions") {
        void querySection("sessions", { query: state.searchQuery, instance: state.instanceFilter, limit: 25 });
      } else if (state.tab === "doctor") {
        void Promise.all([querySection("doctor"), querySection("cache", { keep: 20 })]);
      } else {
        void querySection("overview", { refresh: true });
      }
    });
  }

  const curBtn = document.getElementById("mcp-cur-btn");
  if (curBtn) {
    curBtn.addEventListener("click", () => {
      state.currency = curNext;
      render();
      void runAction("set_currency", { currency: curNext });
    });
  }

  const webBtn = document.getElementById("mcp-open-web");
  if (webBtn) {
    webBtn.addEventListener("click", () => {
      void app.openLink({ url: state.hubUrl || "http://127.0.0.1:3778" });
    });
  }

  for (const el of rootEl.querySelectorAll("[data-tab]")) {
    el.addEventListener("click", () => {
      const tab = el.getAttribute("data-tab");
      state.tab = tab;
      render();
      if (tab === "telemetry" && !state.telemetry) {
        void querySection("telemetry", { preset: state.preset, mode: state.mode, instance: state.instanceFilter });
      } else if (tab === "sessions" && !state.sessions) {
        void querySection("sessions", { query: state.searchQuery, instance: state.instanceFilter, limit: 25 });
      } else if (tab === "doctor" && (!state.doctor || !state.cache)) {
        void Promise.all([querySection("doctor"), querySection("cache", { keep: 20 })]);
      } else if ((tab === "overview" || tab === "profiles") && !state.overview) {
        void querySection("overview");
      }
    });
  }

  for (const el of rootEl.querySelectorAll("[data-preset]")) {
    el.addEventListener("click", () => {
      state.preset = el.getAttribute("data-preset") || "today";
      void querySection("telemetry", { preset: state.preset, mode: state.mode, instance: state.instanceFilter });
    });
  }

  for (const el of rootEl.querySelectorAll("[data-mode]")) {
    el.addEventListener("click", () => {
      state.mode = el.getAttribute("data-mode") || "std";
      void querySection("telemetry", { preset: state.preset, mode: state.mode, instance: state.instanceFilter });
    });
  }

  const searchForm = document.getElementById("mcp-search-form");
  if (searchForm) {
    searchForm.addEventListener("submit", (e) => {
      e.preventDefault();
      const inp = document.getElementById("mcp-search-input");
      state.searchQuery = inp ? inp.value.trim() : "";
      void querySection("sessions", { query: state.searchQuery, instance: state.instanceFilter, limit: 25 });
    });
  }

  for (const el of rootEl.querySelectorAll("[data-act]")) {
    el.addEventListener("click", () => {
      const act = el.getAttribute("data-act");
      const id = el.getAttribute("data-id");
      if (act && id) {
        void runAction(act, { instance: id });
      }
    });
  }

  const docFix = document.getElementById("mcp-doc-fix");
  if (docFix) {
    docFix.addEventListener("click", () => {
      void runAction("doctor_fix");
    });
  }
}

app.ontoolinput = ({ arguments: args }) => {
  if (args && typeof args === "object" && args.tab) {
    state.tab = String(args.tab);
  }
  render();
};

app.ontoolresult = (result) => {
  applyStructuredPayload(result?.structuredContent);
  render();
};

app.onhostcontextchanged = (ctx) => {
  applyHostCtx(ctx);
};

async function boot() {
  try {
    await app.connect();
    applyHostCtx(app.getHostContext());
    const caps = app.getHostCapabilities();
    state.canOpenLinks = Boolean(caps?.openLinks);
    if (!state.overview) {
      void querySection("overview");
    } else {
      render();
    }
  } catch (err) {
    rootEl.innerHTML = `<div class="toast err">连接 Antigravity MCP App 宿主失败：${esc(err?.message || String(err))}</div>`;
  }
}

void boot();
