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

const app = new App({ name: "AgyMate", version: "0.4.0" }, {});

function resolveActiveLang() {
  const pref = String(state.lang || "auto").toLowerCase();
  if (pref === "zh" || pref.startsWith("zh-")) return "zh";
  if (pref === "en" || pref.startsWith("en-")) return "en";
  const nav = (navigator.language || "en").toLowerCase();
  return nav.startsWith("zh") ? "zh" : "en";
}

const EN_EXACT = {
  "Hub 实时": "Hub Live",
  "直连模式": "Direct",
  "Web 控制台": "Web Console",
  "刷新": "Refresh",
  "刷新中…": "Refreshing…",
  "总览与配额": "Overview",
  "Token 账单": "Billing",
  "会话检索": "Sessions",
  "多开实例": "Profiles",
  "体检与缓存": "Doctor & Cache",
  "今日等值账单": "Today Est. Cost",
  "缓存命中节省": "Cache Saved",
  "本月订阅回本": "Monthly ROI",
  "本月累计消耗": "Month Spend",
  "本月累计等效": "Month est. spend",
  "客户端在线": "Online Clients",
  "待确认": "Waiting",
  "运行中": "Running",
  "在线空闲": "Idle",
  "离线": "Offline",
  "缓存": "Cached",
  "主客户端": "Main",
  "IDE客户端": "IDE",
  "隔离沙盒": "Isolated",
  "外部实例": "External",
  "凭证有效": "Valid Token",
  "钥匙串托管": "Keychain",
  "访问令牌过期": "Expired",
  "无刷新令牌": "No Refresh Token",
  "未登录": "Signed Out",
  "不适用": "N/A",
  "即将重置": "Resetting soon",
  "不足1分钟": "<1m",
  "暂无活跃配额数据": "No active quota data",
  "正在读取总览数据…": "Loading overview…",
  "暂未发现 Antigravity 账号实例": "No Antigravity accounts detected",
  "一键安全修复": "Safe Fix",
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
  "置顶": "Focus",
  "启动": "Launch",
  "停止": "Stop",
  "修复": "Heal",
  "总占用": "Total",
  "会话与 Brain 缓存": "Session & Brain Cache",
  "操作完成": "Done",
  "查询失败": "Query failed",
};

function translateMcpText(raw) {
  if (!raw || !/[\u4e00-\u9fff]/.test(raw)) return raw;
  const trimmed = raw.trim();
  let out = EN_EXACT[trimmed];
  if (!out) {
    out = trimmed
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
      .replace(/预计([\d.]+h)耗尽/g, "empty in ~$1")
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
    return "¥" + (val * rate).toFixed(2);
  }
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

function liveStateBadge(liveState) {
  switch (String(liveState || "").toLowerCase()) {
    case "waiting_approval":
      return '<span class="badge warn">待确认</span>';
    case "running":
      return '<span class="badge ok">运行中</span>';
    case "idle":
      return '<span class="badge info">在线空闲</span>';
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

function renderQuotaBars(buckets, ctx, cachedQuota) {
  const sorted = (buckets || []).slice().sort((a, b) => {
    if (!!a.disabled !== !!b.disabled) return a.disabled ? 1 : -1;
    return 0;
  });
  let html = sorted
    .slice(0, 4)
    .map((b) => {
      const name = b.displayName || b.bucketId || "配额";
      if (b.disabled) {
        return `
          <div class="q-item muted" title="${esc(b.description || "受上级窗口限制，暂不适用")}">
            <div class="q-row-top">
              <span class="q-name">${esc(name)}</span>
              <span class="q-meta">不适用</span>
            </div>
            <div class="bar-track"><div class="bar-fill" style="width:0%"></div></div>
          </div>`;
      }
      const pct = Math.max(0, Math.min(100, Number(b.remainingPct || 0)));
      const cls = pct <= 15 ? "danger" : pct <= 35 ? "warn" : "";
      const reset = fmtResetLeft(b.resetUnix);
      const metaParts = [`${pct.toFixed(0)}%`];
      if (reset) metaParts.push(reset === "即将重置" ? reset : `${reset}后重置`);
      else if (cachedQuota) metaParts.push("缓存");
      if (b.exhaustsBeforeReset && b.hoursUntilEmpty > 0) {
        metaParts.push(`预计${b.hoursUntilEmpty.toFixed(1)}h耗尽`);
      }
      return `
        <div class="q-item">
          <div class="q-row-top">
            <span class="q-name">${esc(name)}</span>
            <span class="q-meta">${esc(metaParts.join(" · "))}</span>
          </div>
          <div class="bar-track"><div class="bar-fill ${cls}" style="width:${pct}%"></div></div>
        </div>`;
    })
    .join("");

  if (ctx) {
    const cpct = Math.max(0, Math.min(100, Number(ctx.usagePct || 0)));
    const cCls = cpct >= 80 ? "danger" : cpct >= 60 ? "warn" : "info";
    html += `
      <div class="q-item">
        <div class="q-row-top">
          <span class="q-name">上下文水位${ctx.model ? ` (${esc(ctx.model)})` : ""}</span>
          <span class="q-meta">${cpct.toFixed(0)}% · ${fmtTokens(ctx.estimatedTokensUsed)} / ${fmtTokens(ctx.maxContextTokens)}</span>
        </div>
        <div class="bar-track"><div class="bar-fill ${cCls}" style="width:${cpct}%"></div></div>
      </div>`;
  }
  return html || '<div class="sub-text">暂无活跃配额数据</div>';
}

function renderOverviewTab(isInline) {
  const ov = state.overview;
  if (!ov) {
    return '<div class="empty">正在读取总览数据…</div>';
  }
  const pricing = ov.pricing;
  const sum = ov.todaySummary || {};
  const roi = ov.monthRoi || {};
  const accounts = ov.accounts || [];
  const insts = ov.instances || [];
  const runningCount = insts.filter((i) => i.running).length;

  const roiTxt = roi.subUsd > 0 ? `${Number(roi.roiMultiple || 0).toFixed(1)}x` : fmtMoney(roi.allMonthUsd, pricing);
  const roiSub = roi.subUsd > 0 ? `本月 ${fmtMoney(roi.allMonthUsd, pricing)} / ${fmtMoney(roi.subUsd, pricing)}` : "本月累计等效";

  const kpisHTML = `
    <div class="kpis">
      <div class="kpi">
        <div class="kpi-k">今日等值账单</div>
        <div class="kpi-v">${esc(fmtMoney(sum.totalUsd, pricing))}</div>
        <div class="kpi-sub">${sum.calls || 0} 次调用 · ${fmtTokens(sum.totalTokens)} tok</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">缓存命中节省</div>
        <div class="kpi-v">${esc(fmtMoney(sum.savedByCacheUsd, pricing))}</div>
        <div class="kpi-sub">命中率 ${Number(sum.cacheHitRate || 0).toFixed(1)}%</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">${roi.subUsd > 0 ? "本月订阅回本" : "本月累计消耗"}</div>
        <div class="kpi-v">${esc(roiTxt)}</div>
        <div class="kpi-sub">${esc(roiSub)}</div>
      </div>
      <div class="kpi">
        <div class="kpi-k">客户端在线</div>
        <div class="kpi-v">${runningCount} / ${insts.length}</div>
        <div class="kpi-sub">${accounts.length} 个 Google 账号</div>
      </div>
    </div>`;

  const shownAccounts = isInline ? accounts.slice(0, 4) : accounts;
  const acctCards = shownAccounts.length
    ? `<div class="grid">${shownAccounts
        .map((acct) => {
          const slots = (acct.instances || []).map((i) => i.id).join("+");
          const plan = acct.planName ? `<span class="badge">${esc(acct.planName)}</span>` : "";
          const cached = acct.cachedQuota ? '<span class="badge">缓存</span>' : "";
          return `
            <article class="card">
              <div class="card-head">
                <div class="card-title" title="${esc(acct.displayName)}">${esc(acct.displayName)}</div>
                <div class="actions">${liveStateBadge(acct.liveState)}${cached}</div>
              </div>
              <div class="sub-text" style="margin-bottom:8px">槽位: <span class="mono">${esc(slots || "-")}</span> · 今日 ${esc(fmtMoney(acct.todayUsd, pricing))} ${plan}</div>
              <div class="q-list">${renderQuotaBars(acct.buckets, acct.context, acct.cachedQuota)}</div>
            </article>`;
        })
        .join("")}</div>`
    : '<div class="empty">暂未发现 Antigravity 账号实例</div>';

  let colabHTML = "";
  const colab = ov.colab;
  if (colab && colab.enabled) {
    const cBadge = colab.assignmentsCount > 0 ? `<span class="badge warn">活跃 ${colab.assignmentsCount} 台</span>` : '<span class="badge ok">空闲</span>';
    colabHTML = `
      <div class="banner">
        <div>
          <strong>Colab 云算力池</strong>：余额 <strong>${Number(colab.currentBalance || 0).toFixed(2)} CCU</strong>
          （约 $${Number(colab.estimatedValueUsd || 0).toFixed(2)}）· 速率 ${Number(colab.consumptionRateHourly || 0).toFixed(2)} CCU/小时
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
    ? live
        .slice(0, 8)
        .map(
          (ls) => `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">${esc(ls.title || ls.conversationId)}</div>
            <div class="list-sub">实例 <span class="mono">${esc(ls.instanceId)}</span>${ls.model ? ` · ${esc(ls.model)}` : ""}${ls.workspace ? ` · ${esc(ls.workspace)}` : ""}</div>
          </div>
          ${liveStateBadge(ls.state)}
        </div>`
        )
        .join("")
    : '<div class="sub-text">当前无进行中的会话。</div>';

  const searchForm = `
    <form class="search-row" id="mcp-search-form">
      <input class="input" id="mcp-search-input" type="search" value="${esc(state.searchQuery)}" placeholder="FTS5 全文检索历史会话标题、工作区或内容…">
      <button type="submit" class="btn primary">检索</button>
    </form>`;

  const hitsHTML = hits.length
    ? hits
        .map(
          (h) => `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">[<span class="mono">${esc(h.instanceId)}</span>] ${esc(h.title || h.cascadeId)}</div>
            <div class="list-sub">${esc(h.workspace || "-")} · ${h.stepCount || 0} 步 · ${esc(h.modifiedAt || "")}</div>
            ${h.snippet ? `<div class="list-sub">${esc(h.snippet)}</div>` : ""}
          </div>
        </div>`
        )
        .join("")
    : '<div class="empty">输入关键词检索历史会话，或留空查看最近会话</div>';

  return `
    <div class="section-head"><span class="section-title">实时活动会话</span></div>
    ${liveHTML}
    <div class="section-head"><span class="section-title">跨实例历史会话检索 (FTS5)</span></div>
    ${searchForm}
    ${hitsHTML}`;
}

function renderProfilesTab() {
  const insts = state.overview?.instances || [];
  if (!insts.length) {
    return '<div class="empty">暂未检测到客户端实例</div>';
  }
  return insts
    .map((inst) => {
      const protectedSlot = !!inst.readOnlyLifecycle;
      const acts = [];
      if (inst.running) {
        acts.push(`<button type="button" class="btn" data-act="profile_focus" data-id="${esc(inst.id)}">置前</button>`);
      }
      if (!protectedSlot) {
        if (inst.running) {
          acts.push(`<button type="button" class="btn danger" data-act="profile_stop" data-id="${esc(inst.id)}">停止</button>`);
        } else {
          acts.push(`<button type="button" class="btn primary" data-act="profile_launch" data-id="${esc(inst.id)}">启动</button>`);
          acts.push(`<button type="button" class="btn" data-act="profile_heal" data-id="${esc(inst.id)}">自愈</button>`);
        }
      }
      return `
        <div class="list-item">
          <div class="list-main">
            <div class="list-title">${esc(inst.label || inst.id)} <span class="badge">${esc(kindLabel(inst.kind))}</span> ${inst.running ? '<span class="badge ok">运行中</span>' : '<span class="badge">已停止</span>'}</div>
            <div class="list-sub">ID: <span class="mono">${esc(inst.id)}</span> · 账号: ${esc(inst.account || "-")} · ${authLabel(inst.authState)} · 断链: ${inst.brokenSymlinks || 0}</div>
          </div>
          <div class="row-acts">${acts.join("")}</div>
        </div>`;
    })
    .join("");
}

function renderDoctorTab() {
  const doc = state.doctor;
  const cache = state.cache;
  if (!doc) {
    return '<div class="empty">正在加载体检结果…</div>';
  }
  const checksHTML = (doc.checks || [])
    .map((c) => {
      const cls = c.status === "ok" ? "ok" : c.status === "error" ? "danger" : "warn";
      const lbl = c.status === "ok" ? "正常" : c.status === "error" ? "异常" : "需关注";
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
    .join("");

  let cacheHTML = "";
  if (cache) {
    cacheHTML = `
      <div class="banner">
        <div>
          <strong>会话与 Brain 缓存</strong>：总占用 <strong>${fmtBytes(cache.totalBytes)}</strong> ·
          可安全回收（保留最近 ${cache.keepCount || 20} 条）：<strong>${fmtBytes(cache.reclaimableBytes)}</strong>
        </div>
        <span class="badge info">${(cache.items || []).length} 条记录</span>
      </div>`;
  }

  return `
    <div class="section-head">
      <span class="section-title">环境健康体检 (Antigravity ${esc(doc.mainVersion || "-")}${doc.ideVersion ? ` · IDE ${esc(doc.ideVersion)}` : ""})</span>
      <button type="button" class="btn primary" id="mcp-doc-fix" ${state.actionBusy ? "disabled" : ""}>一键安全修复</button>
    </div>
    ${checksHTML}
    ${cacheHTML}`;
}

function render() {
  const isInline = state.view === "inline";
  rootEl.className = "shell" + (isInline ? " is-inline" : "");

  const srcBadge = state.hubOnline
    ? '<span class="src-pill"><i></i>Hub 实时</span>'
    : '<span class="src-pill fallback"><i></i>直连模式</span>';

  const openWebBtn = state.canOpenLinks
    ? `<button type="button" class="btn" id="mcp-open-web">Web 控制台</button>`
    : "";

  const curNext = String(state.currency).toUpperCase() === "CNY" ? "USD" : "CNY";
  const activeLang = resolveActiveLang();
  const topbar = `
    <header class="topbar">
      <div class="brand-row">
        <span class="brand-title">AgyMate</span>
        ${srcBadge}
      </div>
      <div class="actions">
        <button type="button" class="btn" id="mcp-lang-btn">${activeLang === "en" ? "中" : "EN"}</button>
        <button type="button" class="btn" id="mcp-cur-btn">${String(state.currency).toUpperCase() === "CNY" ? "¥ CNY" : "$ USD"}</button>
        <button type="button" class="btn" id="mcp-refresh-btn" ${state.loading ? "disabled" : ""}>${state.loading ? "刷新中…" : "刷新"}</button>
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
    ["overview", "总览与配额"],
    ["telemetry", "Token 账单"],
    ["sessions", "会话检索"],
    ["profiles", "多开实例"],
    ["doctor", "体检与缓存"],
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
