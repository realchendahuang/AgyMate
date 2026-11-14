'use strict';

(function () {
  const qs = new URLSearchParams(location.search);
  if (qs.get('mode') === 'sidecar') {
    document.documentElement.classList.add('sidecar-mode');
  }

  const state = {
    view: 'overview',
    overview: null,
    telemetry: null,
    sessions: null,
    doctor: null,
    cache: null,
    config: null,
    currency: 'USD',
    usdToCny: 7.20,
    preset: 'today',
    mode: 'std',
    telInst: 'all',
  };

  const VIEW_TITLES = {
    overview: '全局总览',
    telemetry: 'Token 账单',
    sessions: '会话与上下文',
    doctor: '体检与护航',
    storage: '缓存瘦身',
    settings: '设置与通知',
  };

  const $ = (id) => document.getElementById(id);

  function esc(str) {
    return String(str ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function fmtTokens(n) {
    n = Number(n || 0);
    if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
    if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(1) + 'k';
    return String(Math.round(n));
  }

  function fmtBytes(b) {
    b = Number(b || 0);
    if (b >= 1024 ** 3) return (b / 1024 ** 3).toFixed(2) + ' GB';
    if (b >= 1024 ** 2) return (b / 1024 ** 2).toFixed(1) + ' MB';
    if (b >= 1024) return (b / 1024).toFixed(0) + ' KB';
    return b + ' B';
  }

  function fmtMoney(usd) {
    const val = Number(usd || 0);
    if (state.currency === 'CNY') {
      return '¥' + (val * (state.usdToCny || 7.2)).toFixed(2);
    }
    return val >= 100 ? '$' + val.toFixed(1) : '$' + val.toFixed(2);
  }

  function fmtResetLeft(unixSec) {
    if (!unixSec || unixSec <= 0) return '';
    const diff = unixSec - Math.floor(Date.now() / 1000);
    if (diff <= 0) return '即将重置';
    const mins = Math.floor(diff / 60);
    const hrs = Math.floor(mins / 60);
    const days = Math.floor(hrs / 24);
    if (days > 0) return `${days}d${hrs % 24}h`;
    if (hrs > 0) return `${hrs}h${mins % 60}m`;
    return `${mins}m`;
  }

  let toastTimer = null;
  function showToast(msg) {
    const el = $('toast');
    if (!el) return;
    el.textContent = msg;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { el.hidden = true; }, 2600);
  }

  async function api(url, opts = {}) {
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
      ...opts,
    });
    const ct = res.headers.get('content-type') || '';
    const data = ct.includes('application/json') ? await res.json() : await res.text();
    if (!res.ok) {
      const msg = (data && data.error) ? data.error : `HTTP ${res.status}`;
      throw new Error(msg);
    }
    return data;
  }

  // Dark Mode Toggle per modern-web-guidance/dark-mode-toggle
  function initThemeToggle() {
    const meta = document.querySelector('meta[name="color-scheme"]');
    const mq = window.matchMedia('(prefers-color-scheme: dark)');

    function applyStored(v) {
      if (v === 'light' || v === 'dark') {
        meta.content = v;
      } else {
        meta.content = 'light dark';
      }
    }

    $('btn-theme').addEventListener('click', () => {
      const curStored = meta.content === 'dark' || meta.content === 'light' ? meta.content : null;
      const sysDark = mq.matches;
      const renderedDark = curStored ? curStored === 'dark' : sysDark;
      const target = renderedDark ? 'light' : 'dark';
      const targetMatchesSys = (target === 'dark') === sysDark;
      try {
        if (targetMatchesSys) {
          localStorage.removeItem('agymate-color-scheme');
          applyStored(null);
        } else {
          localStorage.setItem('agymate-color-scheme', target);
          applyStored(target);
        }
      } catch (e) {
        applyStored(target);
      }
    });

    window.addEventListener('storage', (e) => {
      if (e.key === 'agymate-color-scheme') {
        applyStored(e.newValue);
      }
    });
  }

  // Light-dismiss fallback for <dialog closedby="any">
  function initDialogFallback() {
    if ('closedBy' in HTMLDialogElement.prototype) return;
    const outside = (e, dialog) => {
      const r = dialog.getBoundingClientRect();
      return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
    };
    document.querySelectorAll('dialog[closedby="any"]').forEach((dialog) => {
      let pressedOutside = false;
      dialog.addEventListener('pointerdown', (e) => { pressedOutside = outside(e, dialog); });
      dialog.addEventListener('pointerup', (e) => {
        if (pressedOutside && outside(e, dialog)) dialog.close('');
      });
    });
  }

  function switchView(view) {
    state.view = view;
    $('view-title').textContent = VIEW_TITLES[view] || 'AgyMate';
    document.querySelectorAll('#main-nav .nav-item').forEach((btn) => {
      btn.classList.toggle('active', btn.dataset.view === view);
    });
    document.querySelectorAll('.view-panel').forEach((sec) => {
      sec.hidden = sec.id !== `view-${view}`;
    });
    if (view === 'telemetry') loadTelemetry();
    if (view === 'sessions') loadSessions();
    if (view === 'doctor') loadDoctor();
    if (view === 'storage') loadCache();
    if (view === 'settings') loadConfig();
  }

  function renderOverview() {
    const ov = state.overview;
    if (!ov) return;

    $('hdr-ver').textContent = 'v' + (ov.version || '0.1.0');
    if (ov.pricing) {
      state.usdToCny = ov.pricing.usdToCny || 7.2;
      if (!sessionStorage.getItem('agymate-cur-override') && ov.pricing.currency) {
        state.currency = ov.pricing.currency;
      }
      state.mode = ov.pricing.mode || state.mode;
    }
    syncCurrencyButtons();

    const ts = ov.updatedAt ? new Date(ov.updatedAt * 1000).toLocaleTimeString() : '--';
    $('side-updated').textContent = `最近同步 ${ts} (${Math.round(ov.pollMs || 0)}ms)`;

    const onlineCount = (ov.instances || []).filter((i) => i.running).length;
    $('top-running-chip').textContent = `${onlineCount}/${(ov.instances || []).length} 在线`;

    const waitCount = (ov.liveSessions || []).filter((s) => s.state === 'waiting_approval').length;
    const runCount = (ov.liveSessions || []).filter((s) => s.state === 'running').length;
    const sessBadge = $('badge-sessions');
    if (waitCount + runCount > 0) {
      sessBadge.textContent = String(waitCount + runCount);
      sessBadge.classList.toggle('warn', waitCount > 0);
      sessBadge.hidden = false;
    } else {
      sessBadge.hidden = true;
    }

    const warnDocs = (ov.doctorCounts?.warn || 0) + (ov.doctorCounts?.error || 0);
    const docBadge = $('badge-doctor');
    if (warnDocs > 0) {
      docBadge.textContent = String(warnDocs);
      docBadge.hidden = false;
    } else {
      docBadge.hidden = true;
    }

    const sum = ov.todaySummary || {};
    const burn = ov.burnRate15m || {};
    const roi = ov.monthRoi || {};

    $('overview-kpis').innerHTML = `
      <div class="kpi-card">
        <div class="kpi-label">今日全实例等值账单</div>
        <div class="kpi-val">${fmtMoney(sum.totalUsd)}</div>
        <div class="kpi-sub">15m 速率 ${fmtMoney(burn.usdPerHour)}/h · ${fmtTokens(burn.tokensPerMin)} tok/m</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">Main Ultra 本月回本</div>
        <div class="kpi-val">${fmtMoney(roi.mainMonthUsd)} <span style="font-size:13px;color:var(--good)">(${(roi.roiMultiple || 0).toFixed(1)}x)</span></div>
        <div class="kpi-sub">订阅基准 ${fmtMoney(roi.subUsd || 100)} · 全实例本月 ${fmtMoney(roi.allMonthUsd)}</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">活跃会话与审批雷达</div>
        <div class="kpi-val">${runCount} 运行 · ${waitCount > 0 ? `<span style="color:var(--warn)">${waitCount} 待批</span>` : '0 阻塞'}</div>
        <div class="kpi-sub">今日共 ${sum.calls || 0} 次 LLM 调用</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">提示词缓存命中率</div>
        <div class="kpi-val">${(sum.cacheHitRate || 0).toFixed(1)}%</div>
        <div class="kpi-sub">缓存已节省约 ${fmtMoney(sum.savedByCacheUsd)}</div>
      </div>
    `;

    const grid = $('instance-grid');
    grid.innerHTML = (ov.instances || []).map((inst) => {
      const snap = inst.snapshot || {};
      const buckets = snap.buckets || [];
      const ctx = snap.context;
      const isRO = inst.readOnlyLifecycle;
      const isWait = inst.liveState === 'waiting_approval';
      const stateBadge = !inst.running
        ? `<span class="badge">离线</span>`
        : isWait
          ? `<span class="badge warn">⏳ 等待审批</span>`
          : inst.liveState === 'running'
            ? `<span class="badge good">● 生成中 (${inst.activeSessions})</span>`
            : `<span class="badge good">在线</span>`;

      const roPill = isRO ? `<span class="badge lock" title="主账号与 IDE 严格只读，不提供启停/自愈操作">🔒 只读保护</span>` : '';
      const acc = inst.account || snap.email || (inst.running ? '已登录 (未返回邮箱)' : '未检出活跃账号');
      const plan = snap.planName ? `<span class="badge accent">${esc(snap.planName)}</span>` : '';

      const barsHTML = buckets.slice(0, 2).map((b) => {
        const pct = Math.max(0, Math.min(100, b.remainingPct || 0));
        const fillCls = pct <= 15 ? 'danger' : pct <= 35 ? 'warn' : '';
        const resetLeft = fmtResetLeft(b.resetUnix);
        let runWarn = '';
        if (b.exhaustsBeforeReset && b.hoursUntilEmpty > 0) {
          runWarn = ` · <span style="color:var(--warn)">按当前速率 ${b.hoursUntilEmpty.toFixed(1)}h 耗尽</span>`;
        }
        return `
          <div class="bar-row">
            <div class="bar-head">
              <span>${esc(b.displayName || b.groupName)}</span>
              <span><strong>${pct.toFixed(0)}%</strong>${resetLeft ? ` · ${resetLeft}后重置` : ''}${runWarn}</span>
            </div>
            <div class="track"><div class="fill ${fillCls}" style="width:${pct}%"></div></div>
          </div>`;
      }).join('');

      let ctxHTML = '';
      if (ctx) {
        const cpct = Math.max(0, Math.min(100, ctx.usagePct || 0));
        const cCls = cpct >= 80 ? 'danger' : cpct >= 60 ? 'warn' : '';
        ctxHTML = `
          <div class="bar-row">
            <div class="bar-head">
              <span class="cell-clip">上下文: ${esc(ctx.title || ctx.cascadeId)}</span>
              <span>${fmtTokens(ctx.estimatedTokensUsed)} / ${fmtTokens(ctx.maxContextTokens)} (${cpct.toFixed(0)}%)</span>
            </div>
            <div class="track"><div class="fill ${cCls}" style="width:${cpct}%"></div></div>
          </div>`;
      }

      const lifecycleButtons = isRO
        ? (inst.running ? `<button type="button" class="btn sm quiet" data-act="focus" data-id="${esc(inst.id)}">聚焦窗口</button>` : '')
        : `
          ${inst.running
            ? `<button type="button" class="btn sm quiet" data-act="focus" data-id="${esc(inst.id)}">聚焦</button>
               <button type="button" class="btn sm danger" data-act="stop" data-id="${esc(inst.id)}">停止</button>`
            : `<button type="button" class="btn sm primary" data-act="launch" data-id="${esc(inst.id)}">启动</button>`}
          <button type="button" class="btn sm quiet" data-act="heal" data-id="${esc(inst.id)}">自愈</button>
        `;

      return `
        <article class="inst-card ${isWait ? 'waiting' : ''}">
          <div class="inst-top">
            <div>
              <div class="inst-title-row">
                <span class="inst-id">${esc(inst.id)}</span>
                <span>${esc(inst.label)}</span>
                ${stateBadge}
                ${plan}
                ${roPill}
              </div>
              <div class="inst-acc" title="${esc(acc)}">${esc(acc)}${inst.hub?.lsVersion ? ` · v${esc(inst.hub.lsVersion)}` : ''}${inst.cdpPort ? ` · CDP:${inst.cdpPort}` : ''}</div>
            </div>
          </div>
          <div class="bars">
            ${barsHTML || (inst.running ? '<div class="card-sub">正在等待 Connect-RPC 返回配额数据…</div>' : '<div class="card-sub">实例未运行，可从历史账单查看累计消耗</div>')}
            ${ctxHTML}
          </div>
          <div class="inst-meta">
            <span>今日 ${fmtMoney(inst.todayUsd)} · ${fmtTokens(inst.todayTokens)} (${(inst.todayCacheHitRate || 0).toFixed(0)}% 缓存)</span>
            <div class="inst-actions">${lifecycleButtons}</div>
          </div>
        </article>`;
    }).join('');
  }

  async function loadOverview(force = false) {
    try {
      state.overview = await api('/api/overview' + (force ? '?refresh=1' : ''));
      renderOverview();
    } catch (e) {
      showToast('刷新总览失败: ' + e.message);
    }
  }

  async function loadTelemetry() {
    try {
      const qsStr = new URLSearchParams({
        preset: state.preset,
        mode: state.mode,
        instance: state.telInst,
      }).toString();
      const res = await api('/api/telemetry?' + qsStr);
      state.telemetry = res;
      if (res.pricing) {
        state.usdToCny = res.pricing.usdToCny || state.usdToCny;
      }
      $('btn-export-csv').href = '/api/telemetry/export.csv?' + qsStr;
      renderTelemetry();
    } catch (e) {
      showToast('加载 Token 账单失败: ' + e.message);
    }
  }

  function renderTelemetry() {
    const data = state.telemetry;
    if (!data || !data.report) return;
    const rep = data.report;
    const sum = rep.summary || {};

    $('tel-chart-sub').textContent = `${rep.presetLabel || ''} · ${rep.granularity || ''} 粒度 · SQLite 只读扫描 ${(rep.scanMs || 0).toFixed(1)}ms`;
    $('tel-kpis').innerHTML = `
      <div class="kpi-card">
        <div class="kpi-label">${esc(rep.presetLabel || '区间')}总账单</div>
        <div class="kpi-val">${fmtMoney(sum.totalUsd)}</div>
        <div class="kpi-sub">${sum.calls || 0} 次调用 · 含图像调用 ${sum.imageCalls || 0} 次</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">总 Token 吞吐</div>
        <div class="kpi-val">${fmtTokens(sum.totalTokens)}</div>
        <div class="kpi-sub">In ${fmtTokens(sum.inputTokens)} · Out ${fmtTokens(sum.outputTokens)}</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">缓存读取 Token</div>
        <div class="kpi-val">${fmtTokens(sum.cacheTokens)}</div>
        <div class="kpi-sub">命中率 ${(sum.cacheHitRate || 0).toFixed(1)}% · 省 ${fmtMoney(sum.savedByCacheUsd)}</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">近 15 分钟燃烧速率</div>
        <div class="kpi-val">${fmtMoney(rep.burnRate15m?.usdPerHour)}/h</div>
        <div class="kpi-sub">${fmtTokens(rep.burnRate15m?.tokensPerMin)} tok/m · ${rep.burnRate15m?.calls15m || 0} calls</div>
      </div>
    `;

    renderTrendSVG(rep.trend || []);

    $('tel-models-body').innerHTML = (rep.byModel || []).map((m) => `
      <tr>
        <td class="cell-clip" title="${esc(m.modelName)}">${esc(m.modelName)}</td>
        <td>${m.calls}</td>
        <td>${fmtTokens(m.inputTokens)}</td>
        <td>${fmtTokens(m.cacheTokens)}</td>
        <td>${fmtTokens(m.outputTokens)}</td>
        <td><strong>${fmtMoney(m.totalUsd)}</strong></td>
      </tr>
    `).join('') || '<tr><td colspan="6" style="color:var(--muted)">当前区间暂无调用数据</td></tr>';

    $('tel-inst-body').innerHTML = (rep.byInstance || []).map((a) => `
      <tr>
        <td class="mono">${esc(a.instanceId)}</td>
        <td>${a.calls}</td>
        <td>${fmtTokens(a.totalTokens)}</td>
        <td>${(a.cacheRate || 0).toFixed(1)}%</td>
        <td><strong>${fmtMoney(a.totalUsd)}</strong></td>
      </tr>
    `).join('') || '<tr><td colspan="5" style="color:var(--muted)">暂无实例统计</td></tr>';

    $('tel-calls-body').innerHTML = (rep.recentCalls || []).map((rc) => `
      <tr>
        <td class="mono">${esc(rc.timeStr)}</td>
        <td class="mono">${esc(rc.instanceId)}</td>
        <td class="cell-clip" title="${esc(rc.title || rc.cascadeId)}">${esc(rc.title || rc.cascadeId)}</td>
        <td class="cell-clip">${esc(rc.modelName)}</td>
        <td>${fmtTokens(rc.inputTokens)}</td>
        <td>${fmtTokens(rc.cacheTokens)}</td>
        <td>${fmtTokens(rc.outputTokens)}</td>
        <td><strong>${fmtMoney(rc.usd)}</strong></td>
      </tr>
    `).join('') || '<tr><td colspan="8" style="color:var(--muted)">暂无记录</td></tr>';
  }

  function renderTrendSVG(trend) {
    const host = $('tel-chart-host');
    if (!trend.length) {
      host.innerHTML = '<p style="color:var(--muted);padding:32px 0;text-align:center">当前筛选条件下暂无趋势柱状数据</p>';
      return;
    }
    const maxTok = Math.max(1, ...trend.map((b) => (b.inputTokens || 0) + (b.cacheTokens || 0) + (b.outputTokens || 0)));
    const H = 190;
    const padB = 26;
    const padT = 10;
    const chartH = H - padB - padT;
    const barW = Math.max(14, Math.min(34, Math.floor(760 / Math.max(trend.length, 6)) - 6));
    const gap = 8;
    const W = Math.max(320, trend.length * (barW + gap) + 20);

    const bars = trend.map((b, i) => {
      const x = 10 + i * (barW + gap);
      const tot = (b.inputTokens || 0) + (b.cacheTokens || 0) + (b.outputTokens || 0);
      const hCache = Math.round(((b.cacheTokens || 0) / maxTok) * chartH);
      const hIn = Math.round(((b.inputTokens || 0) / maxTok) * chartH);
      const hOut = Math.round(((b.outputTokens || 0) / maxTok) * chartH);
      const yCache = H - padB - hCache;
      const yIn = yCache - hIn;
      const yOut = yIn - hOut;
      const showLbl = trend.length <= 16 || i % Math.ceil(trend.length / 12) === 0;
      const meta = `${b.label} · ${b.calls} 次 · ${fmtMoney(b.totalUsd)} · 总计 ${fmtTokens(tot)} (缓存 ${fmtTokens(b.cacheTokens)})`;
      return `
        <g class="tbar" data-tip="${esc(meta)}">
          <rect x="${x}" y="${padT}" width="${barW}" height="${chartH}" fill="transparent"></rect>
          <rect x="${x}" y="${yCache}" width="${barW}" height="${Math.max(1, hCache)}" rx="2" fill="#10b981"></rect>
          <rect x="${x}" y="${yIn}" width="${barW}" height="${hIn}" fill="#0ea5e9"></rect>
          <rect x="${x}" y="${yOut}" width="${barW}" height="${hOut}" rx="2" fill="#f59e0b"></rect>
          ${showLbl ? `<text x="${x + barW / 2}" y="${H - 8}" text-anchor="middle" font-size="10" fill="currentColor" opacity="0.65">${esc(b.label)}</text>` : ''}
        </g>`;
    }).join('');

    host.innerHTML = `<svg viewBox="0 0 ${W} ${H}" width="100%" height="${H}" style="display:block;min-width:${Math.min(W, 560)}px">${bars}</svg>`;
    host.querySelectorAll('.tbar').forEach((g) => {
      g.addEventListener('mouseenter', () => {
        $('chart-tooltip').textContent = g.dataset.tip;
      });
    });
  }

  async function loadSessions() {
    try {
      const q = $('session-search-input').value.trim();
      const inst = $('session-search-inst').value || 'all';
      const data = await api('/api/sessions?' + new URLSearchParams({ q, instance: inst, limit: '30' }).toString());
      state.sessions = data;
      renderSessions();
    } catch (e) {
      showToast('加载会话数据失败: ' + e.message);
    }
  }

  function renderSessions() {
    const data = state.sessions;
    if (!data) return;

    const live = data.live || [];
    $('live-sessions-list').innerHTML = live.length ? live.map((s) => {
      const wait = s.state === 'waiting_approval';
      const cls = wait ? 'warn' : s.state === 'running' ? 'good' : s.state === 'error' ? 'err' : '';
      const stateLabel = wait ? '等待审批/提问' : s.state === 'running' ? '运行中' : s.state === 'error' ? '异常' : '空闲';
      return `
        <div class="session-row ${wait ? 'waiting' : ''}">
          <div>
            <div><span class="mono">${esc(s.instanceId)}</span> · <strong>${esc(s.title || s.conversationId)}</strong> <span class="badge ${cls}">${stateLabel}</span></div>
            <div class="card-sub">${esc(s.workspace || '-')} · ${esc(s.model || 'Unknown')} · 耗时 ${s.durationSec || 0}s${s.lastTool ? ` · 工具: ${esc(s.lastTool)}` : ''}</div>
          </div>
          <button type="button" class="btn sm quiet" data-act="focus" data-id="${esc(s.instanceId)}">聚焦窗口</button>
        </div>`;
    }).join('') : '<p class="card-sub">当前暂无 Hooks/RPC 记录到运行中的活跃会话。</p>';

    const ctxs = data.contexts || [];
    $('context-cards').innerHTML = ctxs.length ? ctxs.map((entry) => {
      const c = entry.context || {};
      const pct = Math.max(0, Math.min(100, c.usagePct || 0));
      const cls = pct >= 80 ? 'danger' : pct >= 60 ? 'warn' : '';
      const tree = (c.tokenBreakdown || []).map((g) => `
        <div class="bar-head"><span>${esc(g.name)}</span><span>${fmtTokens(g.numTokens)}</span></div>
      `).join('');
      return `
        <div class="ctx-card">
          <div class="bar-head">
            <span><strong class="mono">${esc(entry.instanceId)}</strong> · ${esc(c.title || c.cascadeId)}</span>
            <span class="badge ${pct >= 80 ? 'err' : 'accent'}">${pct.toFixed(1)}%</span>
          </div>
          <div class="track"><div class="fill ${cls}" style="width:${pct}%"></div></div>
          <div class="card-sub">${fmtTokens(c.estimatedTokensUsed)} / ${fmtTokens(c.maxContextTokens)} · 模型 ${esc(c.model || '--')}</div>
          ${pct >= 80 ? '<div class="badge warn">建议执行 /compact 释放上下文</div>' : ''}
          <div>${tree}</div>
        </div>`;
    }).join('') : '<p class="card-sub">运行中的实例暂无活跃上下文窗口报告。</p>';

    const hits = data.hits || [];
    $('session-hits').innerHTML = hits.length ? hits.map((h) => `
      <div class="hit-row">
        <div style="min-width:0">
          <div><span class="badge">${esc(h.instanceId)}</span> <strong>${esc(h.title || h.cascadeId)}</strong></div>
          <div class="card-sub">${esc(h.workspace || '-')} · ${h.stepCount || 0} steps · ${esc(h.modifiedAt || '')}</div>
          ${h.snippet ? `<div class="card-sub" style="color:var(--fg-2)">${esc(h.snippet)}</div>` : ''}
        </div>
        <span class="mono" style="font-size:11px;color:var(--muted)">${esc(h.cascadeId.slice(0, 8))}</span>
      </div>
    `).join('') : '<p class="card-sub">未找到匹配的会话记录。</p>';
  }

  async function loadDoctor() {
    try {
      state.doctor = await api('/api/doctor');
      renderDoctor();
    } catch (e) {
      showToast('体检执行失败: ' + e.message);
    }
  }

  function renderDoctor() {
    const rep = state.doctor;
    if (!rep) return;
    $('doctor-versions-sub').textContent = `Main: ${rep.mainVersion || '-'} | Box: ${rep.boxVersion || '未安装'} | IDE: ${rep.ideVersion || '-'} | 通过 ${rep.okCount || 0} · 提醒 ${rep.warnCount || 0}`;

    $('doctor-grid').innerHTML = (rep.checks || []).map((c) => `
      <div class="doc-card ${c.status !== 'ok' ? esc(c.status) : ''}">
        <div class="bar-head">
          <strong>${esc(c.title)}</strong>
          <span class="badge ${c.status === 'ok' ? 'good' : c.status === 'error' ? 'err' : 'warn'}">${c.status.toUpperCase()}</span>
        </div>
        <div style="font-size:12.5px">${esc(c.summary)}</div>
        ${c.detail ? `<div class="card-sub">${esc(c.detail)}</div>` : ''}
      </div>
    `).join('');

    const targets = state.overview?.cdpTargets || [];
    $('cdp-table-body').innerHTML = targets.length ? targets.map((t) => `
      <tr>
        <td class="mono">${esc(t.instanceId)}</td>
        <td class="mono">${t.port}</td>
        <td class="cell-clip">${esc(t.title)}</td>
        <td>${t.zhInjected ? '<span class="badge good">已注入</span>' : '<span class="badge warn">未注入</span>'}</td>
        <td>${t.hudActive ? '<span class="badge good">HUD 运行</span>' : '<span class="badge">未开启</span>'}</td>
        <td class="cell-clip">${esc(t.lastError || t.url)}</td>
      </tr>
    `).join('') : '<tr><td colspan="6" style="color:var(--muted)">未检测到活跃的 CDP DevToolsActivePort 目标</td></tr>';
  }

  async function loadCache() {
    try {
      const keep = Number($('cache-keep-input').value || 20);
      state.cache = await api(`/api/cache?keep=${keep}`);
      renderCache();
    } catch (e) {
      showToast('扫描缓存失败: ' + e.message);
    }
  }

  function renderCache() {
    const sum = state.cache;
    if (!sum) return;
    $('cache-kpis').innerHTML = `
      <div class="kpi-card"><div class="kpi-label">Brain + 对话库总占用</div><div class="kpi-val">${fmtBytes(sum.totalBytes)}</div></div>
      <div class="kpi-card"><div class="kpi-label">安全可回收空间</div><div class="kpi-val" style="color:var(--accent)">${fmtBytes(sum.reclaimableBytes)}</div></div>
      <div class="kpi-card"><div class="kpi-label">保护策略</div><div class="kpi-val">最近 ${sum.keepCount} 条</div><div class="kpi-sub">运行中对话自动锁定保护</div></div>
      <div class="kpi-card"><div class="kpi-label">扫描条目数</div><div class="kpi-val">${(sum.items || []).length}</div></div>
    `;

    const filterInst = $('cache-inst-filter').value || 'all';
    const items = (sum.items || []).filter((it) => filterInst === 'all' || it.instanceId === filterInst).slice(0, 120);
    $('cache-body').innerHTML = items.length ? items.map((it) => `
      <tr>
        <td class="mono">${esc(it.instanceId)}</td>
        <td class="cell-clip" title="${esc(it.uuid)}">${esc(it.title || it.uuid)}</td>
        <td>${fmtBytes(it.brainBytes)}</td>
        <td>${fmtBytes(it.convBytes)}</td>
        <td><strong>${fmtBytes(it.totalBytes)}</strong></td>
        <td>${it.protected ? '<span class="badge good">受保护</span>' : '<span class="badge warn">可清理</span>'}</td>
      </tr>
    `).join('') : '<tr><td colspan="6" style="color:var(--muted)">无缓存条目</td></tr>';
  }

  async function loadConfig() {
    try {
      const cfg = await api('/api/config?masked=1');
      state.config = cfg;
      $('cfg-cdp-enabled').checked = !!cfg.cdp?.enabled;
      $('cfg-cdp-zh').checked = !!cfg.cdp?.zhInject;
      $('cfg-cdp-hud').checked = !!cfg.cdp?.windowHud;
      $('cfg-cdp-pos').value = cfg.cdp?.hudPosition || 'bottom-right';
      $('cfg-cdp-path').value = cfg.cdp?.zhScriptPath || '';
      $('cfg-cdp-insts').value = (cfg.cdp?.hudInstances || []).join(',');

      $('cfg-tg-enabled').checked = !!cfg.telegram?.enabled;
      $('cfg-tg-token').value = cfg.telegram?.botToken || '';
      $('cfg-tg-chatid').value = cfg.telegram?.chatId || '';
      $('cfg-tg-mindur').value = cfg.telegram?.notifyMinDurationSec ?? 60;
      $('cfg-tg-stuck').value = cfg.telegram?.notifyOnStuckSec ?? 45;
      $('cfg-tg-stop').checked = !!cfg.telegram?.notifyOnStop;
      $('cfg-tg-err').checked = !!cfg.telegram?.notifyOnError;
      $('cfg-tg-lowq').checked = !!cfg.telegram?.notifyLowQuota;

      $('cfg-price-mode').value = cfg.pricing?.mode || 'std';
      $('cfg-price-cur').value = cfg.pricing?.currency || 'USD';
      $('cfg-price-rate').value = cfg.pricing?.usdToCny ?? 7.20;
      $('cfg-price-sub').value = cfg.pricing?.ultraMonthlyUsd ?? 100;
      $('cfg-warn-quota').value = cfg.quotaWarnPct ?? 20;
      $('cfg-warn-ctx').value = cfg.contextWarnPct ?? 80;
      $('cfg-box-app').value = cfg.box?.boxAppPath || '';
      $('cfg-box-script').value = cfg.box?.updateScriptPath || '';
    } catch (e) {
      showToast('读取设置失败: ' + e.message);
    }
  }

  function syncCurrencyButtons() {
    document.querySelectorAll('.seg-btn[data-currency]').forEach((b) => {
      b.classList.toggle('active', b.dataset.currency === state.currency);
    });
  }

  // Event wiring
  function wireEvents() {
    document.querySelectorAll('#main-nav .nav-item').forEach((btn) => {
      btn.addEventListener('click', () => switchView(btn.dataset.view));
    });

    document.querySelectorAll('.seg-btn[data-currency]').forEach((btn) => {
      btn.addEventListener('click', () => {
        state.currency = btn.dataset.currency;
        sessionStorage.setItem('agymate-cur-override', '1');
        syncCurrencyButtons();
        renderOverview();
        renderTelemetry();
      });
    });

    $('btn-refresh').addEventListener('click', () => loadOverview(true));
    $('btn-cdp-now').addEventListener('click', async () => {
      try {
        const res = await api('/api/cdp/inject-now', { method: 'POST', body: '{}' });
        showToast(`已触发 CDP 注入 (${(res.targets || []).length} 个窗口)`);
        loadOverview(false);
      } catch (e) { showToast('注入失败: ' + e.message); }
    });

    // Instance card button delegation
    document.body.addEventListener('click', async (e) => {
      const btn = e.target.closest('button[data-act]');
      if (!btn) return;
      const act = btn.dataset.act;
      const id = btn.dataset.id;
      btn.disabled = true;
      try {
        if (act === 'focus') {
          await api(`/api/instances/${encodeURIComponent(id)}/focus`, { method: 'POST', body: '{}' });
          showToast(`已聚焦 ${id}`);
        } else if (act === 'launch') {
          await api(`/api/instances/${encodeURIComponent(id)}/launch`, { method: 'POST', body: '{}' });
          showToast(`正在启动 ${id}`);
          setTimeout(() => loadOverview(true), 1200);
        } else if (act === 'stop') {
          await api(`/api/instances/${encodeURIComponent(id)}/stop`, { method: 'POST', body: '{}' });
          showToast(`已停止 ${id}`);
          setTimeout(() => loadOverview(true), 600);
        } else if (act === 'heal') {
          await api(`/api/instances/${encodeURIComponent(id)}/heal`, { method: 'POST', body: '{}' });
          showToast(`已完成 ${id} 自愈检查`);
          setTimeout(() => loadOverview(true), 400);
        }
      } catch (err) {
        showToast('操作失败: ' + err.message);
      } finally {
        btn.disabled = false;
      }
    });

    // Telemetry controls
    $('telemetry-presets').addEventListener('click', (e) => {
      const b = e.target.closest('button[data-preset]');
      if (!b) return;
      state.preset = b.dataset.preset;
      document.querySelectorAll('#telemetry-presets .tab').forEach((t) => t.classList.toggle('active', t === b));
      loadTelemetry();
    });
    $('tel-inst-filter').addEventListener('change', (e) => { state.telInst = e.target.value; loadTelemetry(); });
    $('tel-mode-select').addEventListener('change', (e) => { state.mode = e.target.value; loadTelemetry(); });

    // Sessions search
    $('session-search-form').addEventListener('submit', (e) => { e.preventDefault(); loadSessions(); });

    // Doctor actions
    $('btn-doctor-fix').addEventListener('click', async () => {
      const fb = $('doctor-feedback');
      fb.hidden = false; fb.className = 'banner'; fb.textContent = '正在执行安全修复…';
      try {
        const res = await api('/api/doctor/fix', { method: 'POST', body: '{}' });
        state.doctor = res.doctor;
        renderDoctor();
        fb.textContent = `修复完成：${(res.fix?.fixed || []).join('；') || '无新增变更'}`;
      } catch (e) {
        fb.className = 'banner error'; fb.textContent = '修复出错: ' + e.message;
      }
    });
    $('btn-sync-box').addEventListener('click', async () => {
      const fb = $('doctor-feedback');
      fb.hidden = false; fb.className = 'banner'; fb.textContent = '正在执行 Antigravity-Box 版本同步（需全部 p1..pN 已停止）…';
      try {
        const res = await api('/api/doctor/sync-box', { method: 'POST', body: '{}' });
        state.doctor = res.doctor;
        renderDoctor();
        fb.textContent = 'Antigravity-Box 同步成功！';
      } catch (e) {
        fb.className = 'banner error'; fb.textContent = e.message;
      }
    });
    $('btn-doctor-cdp').addEventListener('click', async () => {
      try {
        const res = await api('/api/cdp/inject-now', { method: 'POST', body: '{}' });
        if (state.overview) state.overview.cdpTargets = res.targets;
        renderDoctor();
        showToast('CDP 探针已刷新');
      } catch (e) { showToast(e.message); }
    });

    // Storage
    $('btn-cache-scan').addEventListener('click', loadCache);
    $('cache-inst-filter').addEventListener('change', renderCache);
    $('btn-cache-clean').addEventListener('click', async () => {
      const keepCount = Number($('cache-keep-input').value || 20);
      const instance = $('cache-inst-filter').value || 'all';
      const fb = $('cache-feedback');
      try {
        const res = await api('/api/cache/clean', {
          method: 'POST',
          body: JSON.stringify({ keepCount, instance }),
        });
        state.cache = res.scan;
        renderCache();
        fb.hidden = false;
        fb.className = 'banner';
        fb.textContent = `清理完成：移除 ${res.cleaned?.deletedCount || 0} 条旧会话，释放 ${fmtBytes(res.cleaned?.freedBytes || 0)}`;
      } catch (e) {
        fb.hidden = false; fb.className = 'banner error'; fb.textContent = e.message;
      }
    });

    // Settings
    $('btn-tg-test').addEventListener('click', async () => {
      try {
        await api('/api/notify/test', {
          method: 'POST',
          body: JSON.stringify({
            botToken: $('cfg-tg-token').value.trim(),
            chatId: $('cfg-tg-chatid').value.trim(),
          }),
        });
        showToast('Telegram 测试消息已发送！');
      } catch (e) { showToast('测试失败: ' + e.message); }
    });

    $('settings-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const base = state.config || {};
      const next = {
        ...base,
        contextWarnPct: Number($('cfg-warn-ctx').value || 80),
        quotaWarnPct: Number($('cfg-warn-quota').value || 20),
        cdp: {
          enabled: $('cfg-cdp-enabled').checked,
          zhInject: $('cfg-cdp-zh').checked,
          windowHud: $('cfg-cdp-hud').checked,
          hudPosition: $('cfg-cdp-pos').value,
          zhScriptPath: $('cfg-cdp-path').value.trim(),
          hudInstances: $('cfg-cdp-insts').value.split(',').map((s) => s.trim()).filter(Boolean),
        },
        telegram: {
          enabled: $('cfg-tg-enabled').checked,
          botToken: $('cfg-tg-token').value.trim(),
          chatId: $('cfg-tg-chatid').value.trim(),
          notifyOnStop: $('cfg-tg-stop').checked,
          notifyOnError: $('cfg-tg-err').checked,
          notifyMinDurationSec: Number($('cfg-tg-mindur').value || 60),
          notifyOnStuckSec: Number($('cfg-tg-stuck').value || 45),
          notifyLowQuota: $('cfg-tg-lowq').checked,
        },
        pricing: {
          mode: $('cfg-price-mode').value,
          currency: $('cfg-price-cur').value,
          usdToCny: Number($('cfg-price-rate').value || 7.20),
          ultraMonthlyUsd: Number($('cfg-price-sub').value || 100),
        },
        box: {
          ...(base.box || {}),
          boxAppPath: $('cfg-box-app').value.trim(),
          updateScriptPath: $('cfg-box-script').value.trim(),
        },
      };
      const fb = $('settings-feedback');
      try {
        state.config = await api('/api/config', { method: 'PUT', body: JSON.stringify(next) });
        fb.hidden = false; fb.className = 'banner'; fb.textContent = '配置已原子保存并立即应用！';
        showToast('设置已保存');
      } catch (err) {
        fb.hidden = false; fb.className = 'banner error'; fb.textContent = err.message;
      }
    });

    // Create profile dialog
    const dlg = $('create-profile-dialog');
    if (!('commandForElement' in HTMLButtonElement.prototype)) {
      ['btn-open-create', 'btn-open-create-from-ov'].forEach((id) => {
        const b = $(id);
        if (b) b.addEventListener('click', () => dlg.showModal());
      });
    }
    dlg.addEventListener('close', () => { dlg.returnValue = ''; $('cp-error').hidden = true; });
    $('cp-submit-btn').addEventListener('click', async () => {
      const errBox = $('cp-error');
      errBox.hidden = true;
      const body = {
        name: $('cp-name').value.trim(),
        description: $('cp-desc').value.trim(),
        linksPolicy: $('cp-links').value,
        inheritFrom: $('cp-inherit').value,
        inheritMcp: $('cp-mcp').checked,
        launch: $('cp-launch').checked,
      };
      try {
        await api('/api/profiles/create', { method: 'POST', body: JSON.stringify(body) });
        dlg.close('created');
        $('create-profile-form').reset();
        showToast(`分身 ${body.name} 创建成功`);
        loadOverview(true);
      } catch (e) {
        errBox.textContent = e.message;
        errBox.hidden = false;
      }
    });
  }

  function connectSSE() {
    const dot = $('sse-dot');
    const lbl = $('sse-label');
    const es = new EventSource('/api/events');
    es.addEventListener('connected', () => {
      dot.className = 'dot ok';
      lbl.textContent = 'Hub 实时已连接';
    });
    es.addEventListener('update', () => {
      loadOverview(false);
      if (state.view === 'sessions') loadSessions();
    });
    es.onerror = () => {
      dot.className = 'dot err';
      lbl.textContent = '重连 Hub 中…';
    };
  }

  initThemeToggle();
  initDialogFallback();
  wireEvents();
  connectSSE();
  loadOverview(false);
})();
