'use strict';

(function () {
  const qs = new URLSearchParams(location.search);

  const state = {
    view: 'overview',
    overview: null,
    telemetry: null,
    heatmap: null,
    quotaSeries: null,
    sessions: null,
    doctor: null,
    cache: null,
    config: null,
    currency: 'USD',
    curOverridden: false,
    usdToCny: 7.20,
    preset: 'today',
    mode: 'std',
    telInst: 'all',
    searchInst: 'all',
    cacheInst: 'all',
    cdpPos: 'top-right',
    cfgPriceMode: 'std',
    cfgPriceCur: 'USD',
    cpLinks: 'minimal',
    cpInherit: 'host',
    defaultChartMeta: '',
    syncMeta: '',
  };

  const SELECT_DEFS = {
    'tel-inst-filter': {
      get: () => state.telInst,
      set: (v) => { state.telInst = v; },
      options: [
        { value: 'all', label: '全部账号' },
      ],
      onChange: () => loadTelemetry(),
    },
    'tel-mode-select': {
      get: () => state.mode,
      set: (v) => { state.mode = v; },
      options: [
        { value: 'std', label: '标准 + 缓存', desc: '官方标准单价，计入缓存读取折扣' },
        { value: 'promo', label: '优惠 + 缓存', desc: '活动优惠单价，计入缓存读取折扣' },
        { value: 'nc_std', label: '标准无缓存', desc: '按标准原价估算未命中缓存成本' },
        { value: 'nc_promo', label: '优惠无缓存', desc: '按优惠单价估算未命中缓存成本' },
      ],
      onChange: () => loadTelemetry(),
    },
    'session-search-inst': {
      get: () => state.searchInst,
      set: (v) => { state.searchInst = v; },
      options: [
        { value: 'all', label: '全部账号' },
      ],
      onChange: () => loadSessions(),
    },
    'cache-inst-filter': {
      get: () => state.cacheInst,
      set: (v) => { state.cacheInst = v; },
      options: [
        { value: 'all', label: '全部账号' },
      ],
      onChange: () => renderCache(),
    },
    'cfg-lang': {
      get: () => state.cfgLang || 'auto',
      set: (v) => { state.cfgLang = v; },
      options: [
        { value: 'auto', label: '跟随系统（推荐）', desc: 'Follow system / browser locale automatically' },
        { value: 'zh', label: '简体中文', desc: 'Simplified Chinese UI across Dashboard, MCP App & HUD' },
        { value: 'en', label: 'English', desc: 'English UI across Dashboard, MCP App & HUD' },
      ],
    },
    'cfg-cdp-pos': {
      get: () => state.cdpPos,
      set: (v) => { state.cdpPos = v; },
      options: [
        { value: 'top-right', label: '右上角（推荐）', desc: '停靠窗口右上角，向下展开详情不遮挡输入栏' },
        { value: 'bottom-right', label: '右下角', desc: '停靠窗口右下角，向上展开详情面板' },
        { value: 'top-left', label: '左上角', desc: '停靠窗口左上角，向下展开详情面板' },
        { value: 'bottom-left', label: '左下角', desc: '停靠窗口左下角，向上展开详情面板' },
      ],
    },
    'cfg-wh-format': {
      get: () => state.cfgWhFormat || 'auto',
      set: (v) => { state.cfgWhFormat = v; },
      options: [
        { value: 'auto', label: '自动识别（推荐）', desc: '按目标域名自动适配 Slack/Discord/飞书/钉钉/企业微信/Bark' },
        { value: 'generic', label: '标准 JSON', desc: '发送 {event, title, text, html, timestamp}' },
        { value: 'slack', label: 'Slack', desc: '发送 Slack Incoming Webhook 格式' },
        { value: 'discord', label: 'Discord', desc: '发送 Discord Webhook 格式' },
        { value: 'feishu', label: '飞书 / Lark', desc: '发送飞书自定义机器人文本格式' },
        { value: 'dingtalk', label: '钉钉', desc: '发送钉钉自定义机器人文本格式' },
        { value: 'wecom', label: '企业微信', desc: '发送企业微信群机器人文本格式' },
        { value: 'bark', label: 'Bark (iOS)', desc: '发送 Bark 推送格式' },
      ],
    },
    'cfg-price-mode': {
      get: () => state.cfgPriceMode,
      set: (v) => { state.cfgPriceMode = v; },
      options: [
        { value: 'std', label: '标准 + 缓存', desc: '官方标准单价，计入缓存读取折扣' },
        { value: 'promo', label: '优惠 + 缓存', desc: '活动优惠单价，计入缓存读取折扣' },
        { value: 'nc_std', label: '标准无缓存', desc: '忽略缓存折扣，估算原始调用价值' },
        { value: 'nc_promo', label: '优惠无缓存', desc: '优惠费率下忽略缓存折扣' },
      ],
    },
    'cfg-price-cur': {
      get: () => state.cfgPriceCur,
      set: (v) => { state.cfgPriceCur = v; },
      options: [
        { value: 'USD', label: '美元 ($)', desc: '以美元展示等效账单金额' },
        { value: 'CNY', label: '人民币 (¥)', desc: '按设定汇率折算为人民币展示' },
      ],
    },
    'cp-links': {
      get: () => state.cpLinks,
      set: (v) => { state.cpLinks = v; },
      options: [
        { value: 'minimal', label: '精简隔离（推荐）', desc: '独立认证与会话，隔离 .ssh 等敏感目录' },
        { value: 'full', label: '完整继承', desc: '软链复用宿主机大部分开发环境目录' },
        { value: 'none', label: '空白沙盒', desc: '完全纯净的独立目录，不共享宿主文件' },
      ],
    },
    'cp-inherit': {
      get: () => state.cpInherit,
      set: (v) => { state.cpInherit = v; },
      options: [
        { value: 'host', label: '宿主机脱敏继承', desc: '复制常用偏好设置并剥离原有账号登录态' },
        { value: '', label: '空白初始化', desc: '使用默认出厂设置启动全新客户端' },
      ],
    },
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

  function safeStorageGet(store, key) {
    try { return store.getItem(key); } catch { return null; }
  }
  function safeStorageSet(store, key, val) {
    try { store.setItem(key, val); } catch {}
  }
  function safeStorageRemove(store, key) {
    try { store.removeItem(key); } catch {}
  }

  function getSwitch(id) {
    const el = $(id);
    return el ? el.getAttribute('aria-checked') === 'true' : false;
  }
  function setSwitch(id, on) {
    const el = $(id);
    if (el) el.setAttribute('aria-checked', on ? 'true' : 'false');
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

  function fmtMsShort(ms) {
    const n = Number(ms || 0);
    if (!Number.isFinite(n) || n <= 0) return '--';
    return n < 1000 ? `${Math.round(n)}ms` : `${(n / 1000).toFixed(2).replace(/\.?0+$/, '')}s`;
  }

  function renderQuotaSparkSVG(pts, colorVar) {
    if (!Array.isArray(pts) || pts.length < 2) return '';
    const W = 36;
    const H = 14;
    const coords = pts.map((pt, idx) => {
      const rem = Math.max(0, Math.min(1, Number(pt.remaining ?? pt.r ?? 0))) * 100;
      const x = 1.5 + (idx / (pts.length - 1)) * (W - 3);
      const y = H - 2 - (rem / 100) * (H - 4);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    const last = coords[coords.length - 1].split(',');
    return `<svg class="quota-spark" viewBox="0 0 ${W} ${H}" aria-hidden="true"><polyline fill="none" stroke="${colorVar}" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" points="${coords.join(' ')}"></polyline><circle cx="${last[0]}" cy="${last[1]}" r="1.6" fill="${colorVar}"></circle></svg>`;
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
    if (days > 0) {
      const remH = hrs % 24;
      return remH > 0 ? `${days}天${remH}小时` : `${days}天`;
    }
    if (hrs > 0) {
      const remM = mins % 60;
      return remM > 0 ? `${hrs}小时${remM}分` : `${hrs}小时`;
    }
    return `${Math.max(1, mins)}分钟`;
  }

  function fmtDurationZh(sec) {
    const s = Math.max(0, Math.round(Number(sec || 0)));
    if (s < 60) return `${s}秒`;
    const mins = Math.floor(s / 60);
    const remS = s % 60;
    if (mins < 60) return remS > 0 ? `${mins}分${remS}秒` : `${mins}分钟`;
    const hrs = Math.floor(mins / 60);
    const remM = mins % 60;
    return remM > 0 ? `${hrs}小时${remM}分` : `${hrs}小时`;
  }

  function humanizeError(err) {
    const raw = String((err && err.message) ? err.message : err || '未知错误').trim();
    const low = raw.toLowerCase();
    if (low.includes('failed to fetch') || low.includes('networkerror') || low.includes('load failed')) {
      return '无法连接本地后台服务，请确认 AgyMate 服务正在运行';
    }
    if (low.includes('aborted') || low.includes('timeout')) {
      return '请求响应超时，请稍后重试';
    }
    if (/^http\s+5\d\d$/i.test(raw)) {
      return `后台服务暂时繁忙（状态码 ${raw.replace(/\D+/g, '')}）`;
    }
    if (/^http\s+404$/i.test(raw)) {
      return '请求的接口不存在，请确认后台服务已更新至最新版本';
    }
    return raw;
  }

  // ---- 中文化语义兜底映射 ----
  const CTX_GROUP_ZH = {
    'system prompt': '系统提示词',
    'system_prompt': '系统提示词',
    'system instructions': '系统指令',
    'system': '系统指令',
    'tool definitions': '工具定义',
    'tool_definitions': '工具定义',
    'tools': '内置工具定义',
    'mcp tools': '扩展工具 (MCP)',
    'mcp_servers': '扩展工具服务',
    'mcp': '扩展工具服务',
    'subagents': '子代理配置',
    'plugins': '插件目录',
    'featured_plugins': '精选插件目录',
    'chat messages': '对话消息历史',
    'conversation history': '对话消息历史',
    'chat history': '对话消息历史',
    'messages': '对话消息',
    'history': '历史上下文',
    'tool outputs': '工具输出',
    'tool results': '工具返回结果',
    'tool_results': '工具返回结果',
    'user messages': '用户输入',
    'user input': '用户输入',
    'assistant messages': '模型回复',
    'model responses': '模型回复',
    'workspace': '工作区上下文',
    'files': '引用文件',
    'file context': '文件上下文',
    'code context': '代码上下文',
    'memories': '长期记忆',
    'memory': '长期记忆',
    'rules': '自定义规则',
    'user_rules': '用户全局规则',
    'user rules': '用户全局规则',
    'skills': '技能指令',
    'identity': '身份设定',
    'artifacts': '产物规范',
    'conversation_transcript': '转录规范',
    'guidelines': '安全准则',
    'communication_style': '回复风格',
    'slash_commands': '快捷命令',
    'messaging': '异步消息机制',
    'user_information': '工作区环境信息',
    'swarm_worker': '多代理协作规范',
    'other': '其他开销',
    'misc': '其他内容',
    'overhead': '协议开销',
  };

  function localizeContextGroup(name) {
    const raw = String(name || '').trim();
    if (!raw) return '未分类项';
    const hit = CTX_GROUP_ZH[raw.toLowerCase()];
    if (hit) return hit;
    return raw;
  }

  function localizeQuotaGroup(groupRaw) {
    const g = String(groupRaw || '').trim();
    if (!g) return '';
    const low = g.toLowerCase();
    if (low === 'gemini models' || low === 'gemini') return 'Gemini';
    if (low.includes('claude') && low.includes('gpt')) return 'Claude / GPT';
    if (low === 'claude models' || low === 'claude') return 'Claude';
    if (low === 'gpt models' || low === 'openai') return 'GPT';
    return g.replace(/\s+models$/i, '');
  }

  function localizeQuotaWindow(bucket) {
    const win = String(bucket.window || '').trim().toLowerCase();
    const disp = String(bucket.displayName || '').trim();
    const dlow = disp.toLowerCase();
    if (win === '5h' || dlow.includes('five hour') || dlow.includes('5-hour') || dlow.includes('5 hour') || dlow === '5h') {
      return '5小时';
    }
    if (win === 'weekly' || win === '7d' || dlow.includes('weekly') || dlow.includes('7-day')) {
      return '周配额';
    }
    if (win === 'daily' || win === '24h' || dlow.includes('daily')) {
      return '日配额';
    }
    if (win === 'monthly' || dlow.includes('monthly')) {
      return '月配额';
    }
    if (!disp) return '配额';
    return disp
      .replace(/\blimit\s+remaining\b/gi, '')
      .replace(/\bremaining\b/gi, '')
      .trim() || '配额';
  }

  function localizeQuotaName(bucket) {
    if (!bucket) return '配额';
    const grp = localizeQuotaGroup(bucket.groupName);
    const winLabel = localizeQuotaWindow(bucket);
    if (grp && !winLabel.toLowerCase().includes(grp.toLowerCase())) {
      return `${grp} · ${winLabel}`;
    }
    return winLabel || grp || '模型配额';
  }

  function sortQuotaBuckets(buckets) {
    const winRank = (b) => {
      const w = String(b.window || '').toLowerCase();
      const d = String(b.displayName || '').toLowerCase();
      if (w === '5h' || d.includes('five hour') || d.includes('5-hour')) return 1;
      if (w === 'daily' || d.includes('daily')) return 2;
      if (w === 'weekly' || d.includes('weekly')) return 3;
      return 4;
    };
    return (buckets || []).slice().sort((a, b) => {
      if (!!a.disabled !== !!b.disabled) return a.disabled ? 1 : -1;
      const ga = String(a.groupName || '');
      const gb = String(b.groupName || '');
      if (ga !== gb) return 0;
      return winRank(a) - winRank(b);
    });
  }

  function localizeGranularity(gran) {
    const low = String(gran || '').trim().toLowerCase();
    if (!low) return '';
    if (low === 'hour' || low === 'hourly' || low === '1h' || low === '小时') return '按小时';
    if (low === 'day' || low === 'daily' || low === '1d' || low === '天') return '按天';
    if (low === '15m' || low === '15min') return '按15分钟';
    if (low === '5m' || low === '5min') return '按5分钟';
    if (low === 'month' || low === 'monthly' || low === '月') return '按月';
    return gran;
  }

  function localizePresetLabel(label, preset) {
    const map = {
      today: '今日',
      yesterday: '昨日',
      last_5h: '近5小时',
      last_7d: '近7天',
      this_month: '本月',
      all_time: '全部时间',
    };
    if (preset && map[preset]) return map[preset];
    const raw = String(label || '').trim();
    if (raw === '近5h') return '近5小时';
    return raw || '当前区间';
  }

  function renderEmptyState(title, hint = '', actionHtml = '') {
    return `
      <div class="empty-state">
        <span class="empty-ico" aria-hidden="true">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><rect x="2.2" y="3" width="11.6" height="10" rx="2.2" stroke="currentColor" stroke-width="1.3"/><path d="M5 6.8h6M5 9.5h3.8" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>
        </span>
        <div class="empty-title">
          <span>${esc(title)}</span>
          ${hint ? `<button type="button" class="help-dot" aria-label="说明" data-tip="${esc(hint)}">?</button>` : ''}
        </div>
        ${actionHtml ? `<div class="empty-act">${actionHtml}</div>` : ''}
      </div>`;
  }

  // ---- 统一毛玻璃悬停提示管理器 (#tip-host) ----
  let activeTipTarget = null;
  let tipShowTimer = null;
  let tipHideTimer = null;

  function positionTip(el) {
    const tip = $('tip-host');
    if (!tip || !el || !el.isConnected) return;
    const ar = el.getBoundingClientRect();
    if (ar.width === 0 && ar.height === 0) {
      hideTip(true);
      return;
    }
    tip.style.left = '-9999px';
    tip.style.top = '-9999px';
    tip.hidden = false;
    const tr = tip.getBoundingClientRect();
    const vw = window.innerWidth || 1280;
    const vh = window.innerHeight || 800;
    const left = Math.max(8, Math.min(vw - tr.width - 8, Math.round(ar.left + ar.width / 2 - tr.width / 2)));
    const spaceAbove = ar.top - 8;
    const spaceBelow = vh - ar.bottom - 8;
    const placeAbove = spaceAbove >= tr.height + 6 || (spaceAbove > spaceBelow && spaceBelow < tr.height + 6);
    const rawTop = placeAbove ? (ar.top - tr.height - 7) : (ar.bottom + 7);
    const top = Math.max(8, Math.min(vh - tr.height - 8, Math.round(rawTop)));
    tip.classList.toggle('below', !placeAbove);
    tip.style.left = left + 'px';
    tip.style.top = top + 'px';
  }

  function showTip(el, instant = false) {
    if (!el || !el.dataset || !el.dataset.tip) return;
    if (tipHideTimer) { clearTimeout(tipHideTimer); tipHideTimer = null; }
    if (tipShowTimer) { clearTimeout(tipShowTimer); tipShowTimer = null; }
    const popHost = $('pop-host');
    if (popHost && !popHost.hidden && !el.closest('#pop-host')) return;

    const renderNow = () => {
      const tip = $('tip-host');
      if (!tip || !el.isConnected || !el.dataset.tip) return;
      activeTipTarget = el;
      const title = el.dataset.tipTitle || '';
      const body = el.dataset.tip || '';
      tip.innerHTML = `${title ? `<div class="tip-title">${esc(title)}</div>` : ''}<div class="tip-body">${esc(body)}</div>`;
      positionTip(el);
    };

    if (instant || el.classList.contains('help-dot')) {
      renderNow();
    } else {
      tipShowTimer = setTimeout(renderNow, 90);
    }
  }

  function hideTip(immediate = false) {
    if (tipShowTimer) { clearTimeout(tipShowTimer); tipShowTimer = null; }
    const doHide = () => {
      if (tipHideTimer) { clearTimeout(tipHideTimer); tipHideTimer = null; }
      const tip = $('tip-host');
      if (tip) tip.hidden = true;
      activeTipTarget = null;
    };
    if (immediate) {
      doHide();
    } else {
      if (tipHideTimer) clearTimeout(tipHideTimer);
      tipHideTimer = setTimeout(doHide, 60);
    }
  }

  function syncActiveTipAfterRender() {
    if (activeTipTarget && !activeTipTarget.isConnected) {
      hideTip(true);
    }
  }

  // ---- 轻提示胶囊 Toast ----
  let statusTimer = null;
  const TOAST_ICONS = {
    ok: '<svg width="13" height="13" viewBox="0 0 14 14" fill="none"><circle cx="7" cy="7" r="5.8" stroke="currentColor" stroke-width="1.4"/><path d="M4.5 7.2 6.2 8.9 9.6 5.3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    err: '<svg width="13" height="13" viewBox="0 0 14 14" fill="none"><circle cx="7" cy="7" r="5.8" stroke="currentColor" stroke-width="1.4"/><path d="M7 4.3v3.3M7 9.6v.1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    warn: '<svg width="13" height="13" viewBox="0 0 14 14" fill="none"><path d="M7 2.2 12.6 11.8H1.4L7 2.2Z" stroke="currentColor" stroke-width="1.35" stroke-linejoin="round"/><path d="M7 5.8v2.6M7 10.1v.1" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
    busy: '<span class="btn-spin"></span>',
  };

  function showStatus(msg, kind = '', ttlMs = 0) {
    const el = $('status-pill');
    if (!el) return;
    const toneCls = (kind === 'ok' || kind === 'err' || kind === 'warn') ? kind : '';
    const iconHtml = TOAST_ICONS[kind] ? `<span class="st-ico">${TOAST_ICONS[kind]}</span>` : '';
    el.innerHTML = `${iconHtml}<span>${esc(msg)}</span>`;
    el.className = 'status lift' + (toneCls ? ' ' + toneCls : '');
    el.hidden = false;
    clearTimeout(statusTimer);
    if (kind === 'busy' && ttlMs <= 0) return;
    const delay = ttlMs > 0 ? ttlMs : (kind === 'err' ? 4600 : 2800);
    statusTimer = setTimeout(() => { el.hidden = true; }, delay);
  }

  function flashEl(el) {
    if (!el) return;
    el.classList.remove('flash');
    void el.offsetWidth;
    el.classList.add('flash');
  }

  async function copyText(text, label = '内容') {
    const str = String(text ?? '');
    if (!str) return false;
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(str);
      } else {
        const ta = document.createElement('textarea');
        ta.value = str;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        ta.remove();
      }
      showStatus(`已复制${label}`, 'ok');
      return true;
    } catch {
      showStatus(`复制${label}失败，请手动选中复制`, 'warn');
      return false;
    }
  }

  const HUB_TOKEN = document.querySelector('meta[name="agymate-token"]')?.content || '';

  async function api(url, opts = {}) {
    let res;
    try {
      const headers = { ...(opts.headers || {}) };
      if (HUB_TOKEN && !headers['X-AgyMate-Token']) {
        headers['X-AgyMate-Token'] = HUB_TOKEN;
      }
      if (opts.body !== undefined && !headers['Content-Type']) {
        headers['Content-Type'] = 'application/json';
      }
      res = await fetch(url, { ...opts, headers });
    } catch (netErr) {
      throw new Error(humanizeError(netErr));
    }
    const ct = res.headers.get('content-type') || '';
    const data = ct.includes('application/json') ? await res.json() : await res.text();
    if (!res.ok) {
      const msg = (data && data.error) ? data.error : `HTTP ${res.status}`;
      throw new Error(humanizeError(msg));
    }
    return data;
  }

  // ---- Magpie 宿主桥接（主题同步 / 可见性节流 / 快捷刷新） ----
  function applyMagpieTokens(tokens) {
    if (!tokens || typeof tokens !== 'object') return;
    const root = document.documentElement;
    for (const [k, v] of Object.entries(tokens)) {
      if (/^--[a-z0-9-]+$/i.test(k) && typeof v === 'string' && v.trim()) {
        root.style.setProperty(k, v.trim());
      }
    }
  }

  function applyThemeName(dark, withTransition = true) {
    const root = document.documentElement;
    const target = dark ? 'dark' : 'light';
    if (withTransition) {
      root.classList.add('theming');
      setTimeout(() => root.classList.remove('theming'), 280);
    }
    root.dataset.theme = target;
    const meta = document.querySelector('meta[name="color-scheme"]');
    if (meta) meta.content = target;
  }

  function initThemeAndBridge() {
    const meta = document.querySelector('meta[name="color-scheme"]');
    const mq = window.matchMedia('(prefers-color-scheme: dark)');

    const langBtn = $('btn-lang');
    if (langBtn && window.AgyI18n) {
      langBtn.addEventListener('click', () => {
        const next = window.AgyI18n.toggle();
        state.cfgLang = next;
        syncAllSelectLabels();
        api('/api/config', { method: 'PUT', body: JSON.stringify({ language: next }) }).catch(() => {});
      });
    }

    const themeBtn = $('btn-theme');
    if (themeBtn) {
      themeBtn.addEventListener('click', () => {
        const root = document.documentElement;
        const cur = root.dataset.theme;
        const renderedDark = cur ? cur === 'dark' : mq.matches;
        const target = renderedDark ? 'light' : 'dark';
        applyThemeName(target === 'dark', true);
        if ((target === 'dark') === mq.matches) {
          safeStorageRemove(localStorage, 'agymate-color-scheme');
          delete root.dataset.theme;
          if (meta) meta.content = 'light dark';
        } else {
          safeStorageSet(localStorage, 'agymate-color-scheme', target);
        }
      });
    }

    window.addEventListener('message', (ev) => {
      const msg = ev.data;
      if (!msg || msg.magpie !== 1) return;
      if (msg.event === 'theme' && msg.data) {
        if (typeof msg.data.dark === 'boolean') {
          applyThemeName(msg.data.dark, true);
        }
        if (msg.data.tokens) applyMagpieTokens(msg.data.tokens);
      } else if (msg.event === 'visibility' && msg.data) {
        document.documentElement.classList.toggle('magpie-out-of-sight', !msg.data.visible);
        if (msg.data.visible) loadOverview(false);
      } else if (msg.event === 'action') {
        switchView('overview');
        loadOverview(true);
      }
    });

    if (qs.get('embed') === 'magpie' && window.parent && window.parent !== window) {
      try {
        window.parent.postMessage({ magpie: 1, type: 'hello' }, '*');
      } catch {}
    }
  }

  // ---- 分段滑块动画同步 ----
  function syncSegThumb(segEl, instant = false) {
    if (!segEl) return;
    const thumb = segEl.querySelector('.thumb');
    const active = segEl.querySelector('.opt.on');
    if (!thumb || !active) return;
    if (instant) segEl.classList.add('still');
    thumb.style.width = active.offsetWidth + 'px';
    thumb.style.transform = `translateX(${active.offsetLeft}px)`;
    if (instant) {
      void thumb.offsetWidth;
      segEl.classList.remove('still');
    }
  }

  function syncAllSegs(instant = false) {
    document.querySelectorAll('.segs').forEach((s) => syncSegThumb(s, instant));
  }

  // ---- 统一模态框管理器（支持焦点锁定、Escape 关闭、防背景滚动、异步防重提） ----
  const modalStack = [];
  let modalBusy = false;

  const MODAL_ICONS = {
    danger: '<svg width="17" height="17" viewBox="0 0 16 16" fill="none"><path d="M8 2.2 14.2 13H1.8L8 2.2Z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M8 6.2v3.2M8 11.2v.1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    warn: '<svg width="17" height="17" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.4"/><path d="M8 5v3.5M8 10.7v.1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    accent: '<svg width="17" height="17" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.4"/><path d="M8 7.2v3.6M8 5.1v.1" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
    ok: '<svg width="17" height="17" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.4"/><path d="M5.2 8.2 7.1 10.1 11 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  };

  function getFocusableElements(container) {
    if (!container) return [];
    return Array.from(
      container.querySelectorAll('button:not(:disabled), input:not(:disabled), [href], [tabindex]:not([tabindex="-1"])'),
    ).filter((el) => !el.hidden && el.offsetParent !== null);
  }

  function openModal(modalEl, focusEl = null) {
    if (!modalEl) return;
    hideTip(true);
    closePop();
    const prevFocus = document.activeElement;
    modalEl.classList.remove('closing');
    modalEl.hidden = false;
    document.body.classList.add('modal-open');

    const existingIdx = modalStack.findIndex((m) => m.el === modalEl);
    if (existingIdx >= 0) modalStack.splice(existingIdx, 1);
    modalStack.push({ el: modalEl, prevFocus });

    setTimeout(() => {
      const target = focusEl || getFocusableElements(modalEl)[0];
      if (target && typeof target.focus === 'function') {
        target.focus({ preventScroll: true });
      }
    }, 30);
  }

  function closeModal(modalEl, immediate = false) {
    if (!modalEl || modalEl.hidden) return;
    if (modalBusy) return;
    closePop();

    const idx = modalStack.findIndex((m) => m.el === modalEl);
    const entry = idx >= 0 ? modalStack.splice(idx, 1)[0] : null;

    const finish = () => {
      modalEl.classList.remove('closing');
      modalEl.hidden = true;
      if (modalStack.length === 0) {
        document.body.classList.remove('modal-open');
      }
      if (entry && entry.prevFocus && typeof entry.prevFocus.focus === 'function' && entry.prevFocus.isConnected) {
        try { entry.prevFocus.focus({ preventScroll: true }); } catch {}
      }
    };

    const reduceMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (immediate || reduceMotion) {
      finish();
    } else {
      modalEl.classList.add('closing');
      setTimeout(finish, 140);
    }
  }

  function setBtnBusy(btn, busy, busyLabel = '处理中…') {
    if (!btn) return;
    if (busy) {
      if (!btn.dataset.origHtml) btn.dataset.origHtml = btn.innerHTML;
      btn.disabled = true;
      btn.innerHTML = `<span class="btn-spin" aria-hidden="true"></span><span>${esc(busyLabel)}</span>`;
    } else {
      btn.disabled = false;
      if (btn.dataset.origHtml) {
        btn.innerHTML = btn.dataset.origHtml;
        delete btn.dataset.origHtml;
      }
    }
  }

  // 通用中文确认弹窗控制器
  let activeConfirmAction = null;
  function openConfirmModal(opts = {}) {
    const modal = $('confirm-modal');
    if (!modal) return;
    const tone = opts.tone || (opts.danger !== false ? 'danger' : 'accent');
    const iconBox = $('cfm-icon');
    iconBox.className = `modal-icon ${tone}`;
    iconBox.innerHTML = MODAL_ICONS[tone] || MODAL_ICONS.danger;

    $('cfm-title').textContent = opts.title || '请确认操作';
    const subEl = $('cfm-sub');
    if (opts.sub) {
      subEl.textContent = opts.sub;
      subEl.hidden = false;
    } else {
      subEl.hidden = true;
    }

    $('cfm-desc').textContent = opts.desc || '';

    const metaEl = $('cfm-meta');
    const rows = Array.isArray(opts.meta) ? opts.meta.filter((r) => r && r.k) : [];
    if (rows.length > 0) {
      metaEl.innerHTML = rows.map((r) => `
        <div class="cfm-kv">
          <span>${esc(r.k)}</span>
          <b class="${esc(r.cls || '')}">${esc(r.v)}</b>
        </div>`).join('');
      metaEl.hidden = false;
    } else {
      metaEl.innerHTML = '';
      metaEl.hidden = true;
    }

    const errEl = $('cfm-error');
    errEl.hidden = true;
    errEl.textContent = '';

    const cancelBtn = $('cfm-cancel-btn');
    const okBtn = $('cfm-ok-btn');
    cancelBtn.textContent = opts.cancelLabel || '取消';
    delete okBtn.dataset.origHtml;
    okBtn.disabled = false;
    okBtn.textContent = opts.okLabel || '确认执行';
    okBtn.className = opts.danger !== false ? 'text danger-fill' : 'text primary';

    activeConfirmAction = async () => {
      if (modalBusy) return;
      errEl.hidden = true;
      if (typeof opts.onConfirm !== 'function') {
        closeModal(modal);
        return;
      }
      modalBusy = true;
      cancelBtn.disabled = true;
      $('cfm-close-btn').disabled = true;
      setBtnBusy(okBtn, true, opts.busyLabel || '执行中…');
      try {
        await opts.onConfirm();
        modalBusy = false;
        cancelBtn.disabled = false;
        $('cfm-close-btn').disabled = false;
        setBtnBusy(okBtn, false);
        closeModal(modal);
      } catch (err) {
        modalBusy = false;
        cancelBtn.disabled = false;
        $('cfm-close-btn').disabled = false;
        setBtnBusy(okBtn, false);
        errEl.textContent = humanizeError(err);
        errEl.hidden = false;
      }
    };

    openModal(modal, opts.danger !== false ? cancelBtn : okBtn);
  }

  // 通用中文详情与引导模态框控制器
  function openDetailModal(opts = {}) {
    const modal = $('detail-modal');
    if (!modal) return;
    const tone = opts.tone || 'accent';
    const iconBox = $('dtl-icon');
    iconBox.className = `modal-icon ${tone}`;
    iconBox.innerHTML = MODAL_ICONS[tone] || MODAL_ICONS.accent;

    $('dtl-title').textContent = opts.title || '详情信息';
    const subEl = $('dtl-sub');
    if (opts.sub) {
      subEl.textContent = opts.sub;
      subEl.hidden = false;
    } else {
      subEl.hidden = true;
    }

    $('dtl-body').innerHTML = opts.bodyHtml || '';

    const foot = $('dtl-foot');
    const actions = Array.isArray(opts.actions) ? opts.actions : [];
    const btnsHtml = actions.map((a, idx) => `
      <button type="button" class="text ${esc(a.cls || '')}" data-dtl-act="${idx}">${esc(a.label)}</button>
    `).join('') + `<button type="button" class="text ${actions.some((a) => (a.cls || '').includes('primary')) ? '' : 'primary'}" id="dtl-ok-btn">${esc(opts.closeLabel || '关闭')}</button>`;
    foot.innerHTML = btnsHtml;

    foot.querySelectorAll('button[data-dtl-act]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const actDef = actions[Number(btn.dataset.dtlAct)];
        if (!actDef || typeof actDef.onClick !== 'function') return;
        btn.disabled = true;
        try {
          await actDef.onClick();
          if (actDef.closeOnDone) closeModal(modal);
        } finally {
          btn.disabled = false;
        }
      });
    });

    $('dtl-ok-btn')?.addEventListener('click', () => closeModal(modal));
    openModal(modal, $('dtl-ok-btn'));
  }

  // ---- 自定义毛玻璃下拉菜单控制器（完整键盘可达性 + 边缘防裁剪） ----
  let activePopField = null;
  let activePopOptions = [];
  let activePopOnPick = null;
  let activePopFocusIdx = -1;

  function syncFieldLabel(id) {
    const def = SELECT_DEFS[id];
    const btn = $(id);
    if (!def || !btn) return;
    const cur = def.get();
    const match = def.options.find((o) => o.value === cur) || def.options[0];
    const vEl = btn.querySelector('.v');
    if (vEl && match) vEl.textContent = match.shortLabel || match.label;
  }

  function syncAllSelectLabels() {
    Object.keys(SELECT_DEFS).forEach(syncFieldLabel);
  }

  function syncAccountSelectOptions(accounts, instances) {
    const acctList = Array.isArray(accounts) ? accounts : [];
    const instList = Array.isArray(instances) ? instances : [];
    const base = [{ value: 'all', label: '全部账号' }];
    if (acctList.length > 0) {
      acctList.forEach((acct) => {
        base.push({
          value: acct.key,
          label: acct.displayName || acct.email || acct.key,
        });
        const slots = acct.instances || [];
        if (slots.length > 1) {
          slots.forEach((iv) => {
            base.push({
              value: iv.id,
              label: `  ↳ ${iv.label || iv.id}`,
            });
          });
        }
      });
    } else {
      instList.forEach((inst) => {
        base.push({
          value: inst.id,
          label: inst.label || inst.id,
        });
      });
    }
    const allowed = new Set(base.map((o) => o.value));
    ['tel-inst-filter', 'session-search-inst', 'cache-inst-filter'].forEach((id) => {
      const def = SELECT_DEFS[id];
      if (!def) return;
      def.options = base.slice();
      if (!allowed.has(def.get())) {
        def.set('all');
      }
      syncFieldLabel(id);
    });
  }

  function updatePopFocusVisual() {
    const host = $('pop-host');
    if (!host || host.hidden) return;
    const items = host.querySelectorAll('.pop-item');
    items.forEach((el, idx) => {
      const focused = idx === activePopFocusIdx;
      el.classList.toggle('focus', focused);
      if (focused) {
        el.scrollIntoView({ block: 'nearest' });
      }
    });
  }

  function closePop(restoreFocus = false) {
    const host = $('pop-host');
    if (host) host.hidden = true;
    const prevField = activePopField;
    if (activePopField) {
      activePopField.classList.remove('open');
      activePopField.removeAttribute('aria-expanded');
      activePopField = null;
    }
    activePopOptions = [];
    activePopOnPick = null;
    activePopFocusIdx = -1;
    if (restoreFocus && prevField && typeof prevField.focus === 'function') {
      prevField.focus({ preventScroll: true });
    }
  }

  function openPopoverHost(anchorEl, options, curVal, onPick) {
    const host = $('pop-host');
    if (!anchorEl || !host || !Array.isArray(options) || !options.length) return;
    hideTip(true);

    if (activePopField === anchorEl && !host.hidden) {
      closePop();
      return;
    }
    closePop();

    activePopField = anchorEl;
    activePopOptions = options;
    activePopOnPick = onPick;
    anchorEl.classList.add('open');
    anchorEl.setAttribute('aria-expanded', 'true');

    const selIdx = curVal !== undefined ? options.findIndex((o) => o.value === curVal) : -1;
    activePopFocusIdx = selIdx >= 0 ? selIdx : 0;

    host.innerHTML = options.map((opt, idx) => {
      const selected = curVal !== undefined && opt.value === curVal;
      const focused = idx === activePopFocusIdx;
      return `
        <button type="button" class="pop-item ${selected ? 'on' : ''} ${focused ? 'focus' : ''}" role="option" aria-selected="${selected}" data-pop-idx="${idx}"${opt.desc ? ` data-tip="${esc(opt.desc)}"` : ''}>
          <span class="pm-check">${selected ? '<svg width="12" height="12" viewBox="0 0 12 12"><path d="M2.2 6.4 4.8 9 9.8 3.2" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>' : ''}</span>
          <span class="pi-body">
            <span class="pi-label">${esc(opt.label)}</span>
          </span>
        </button>`;
    }).join('');

    host.style.left = '8px';
    host.style.top = '8px';
    const rect = anchorEl.getBoundingClientRect();
    const minW = Math.min(window.innerWidth - 16, Math.max(168, Math.round(rect.width)));
    host.style.minWidth = minW + 'px';
    host.hidden = false;

    const popRect = host.getBoundingClientRect();
    const spaceBelow = window.innerHeight - rect.bottom - 10;
    const openUp = spaceBelow < Math.min(popRect.height, 220) && rect.top > spaceBelow;
    host.classList.toggle('up', openUp);

    let left = rect.left;
    if (left + popRect.width > window.innerWidth - 8) {
      left = window.innerWidth - popRect.width - 8;
    }
    left = Math.max(8, left);

    const top = openUp
      ? Math.max(8, rect.top - popRect.height - 6)
      : Math.min(window.innerHeight - popRect.height - 8, rect.bottom + 6);

    host.style.left = Math.round(left) + 'px';
    host.style.top = Math.round(top) + 'px';
    host.style.setProperty('--ox', Math.max(16, Math.min(popRect.width - 16, Math.round(rect.left - left + rect.width / 2))) + 'px');

    host.querySelectorAll('.pop-item').forEach((item) => {
      item.addEventListener('mouseenter', () => {
        activePopFocusIdx = Number(item.dataset.popIdx);
        updatePopFocusVisual();
      });
      item.addEventListener('click', (ev) => {
        ev.stopPropagation();
        const picked = options[Number(item.dataset.popIdx)];
        closePop(true);
        if (picked && onPick) onPick(picked);
      });
    });

    updatePopFocusVisual();
  }

  function openPop(fieldBtn) {
    const key = fieldBtn.dataset.select;
    const def = SELECT_DEFS[key];
    if (!def) return;
    openPopoverHost(fieldBtn, def.options, def.get(), (picked) => {
      def.set(picked.value);
      syncFieldLabel(key);
      if (def.onChange) def.onChange(picked.value);
    });
  }

  // ---- Colab 网页授权与账号绑定菜单 ----
  async function triggerColabLogin(loginHint = '') {
    try {
      showStatus('正在请求网页授权地址…', 'busy');
      const res = await api('/api/colab/accounts/login', {
        method: 'POST',
        body: JSON.stringify({ loginHint, openBrowser: true }),
      });
      if (res.opened) {
        showStatus('已在浏览器打开 Google 授权页，授权完成后将自动同步', 'ok', 4500);
      } else if (res.authUrl) {
        $('status-pill').hidden = true;
        openDetailModal({
          title: '完成云算力账号网页授权',
          sub: loginHint ? `目标账号：${loginHint}` : '请在浏览器中打开下方授权链接',
          tone: 'accent',
          bodyHtml: `
            <p class="cfm-desc">请在浏览器中打开下方授权链接完成登录：</p>
            <div class="dtl-codebox">
              <div class="dtl-codebox-head">
                <span>授权链接</span>
                <button type="button" class="text" data-copy-text="${esc(res.authUrl)}" data-copy-label="授权链接">复制链接</button>
              </div>
              <code>${esc(res.authUrl)}</code>
            </div>`,
          actions: [
            {
              label: '在新窗口打开',
              cls: 'primary',
              closeOnDone: true,
              onClick: () => { window.open(res.authUrl, '_blank', 'noopener,noreferrer'); },
            },
          ],
          closeLabel: '稍后处理',
        });
      }
    } catch (err) {
      showStatus('发起授权失败：' + humanizeError(err), 'err');
    }
  }

  function openColabBindPop(anchorBtn) {
    const ov = state.overview || {};
    const bound = new Set(
      ((ov.colab && ov.colab.accounts) || [])
        .filter((c) => c.account && (c.ok || c.source === 'oauth'))
        .map((c) => c.account.toLowerCase()),
    );
    const localEmails = [];
    const seen = new Set();
    (ov.accounts || []).forEach((acct) => {
      const em = (acct.email || '').trim();
      const low = em.toLowerCase();
      if (em && !bound.has(low) && !seen.has(low)) {
        seen.add(low);
        localEmails.push(em);
      }
    });

    const options = localEmails.map((em) => ({
      value: em,
      label: `绑定 ${em}`,
      desc: '使用客户端已登录的同邮箱账号快速授权',
    }));
    options.push({
      value: '',
      label: '绑定其他 Google 账号…',
      desc: '在浏览器中选择任意账号完成网页授权',
    });

    openPopoverHost(anchorBtn, options, undefined, (picked) => {
      triggerColabLogin(picked.value);
    });
  }

  // ---- 视图切换 ----
  function switchView(view) {
    state.view = view;
    hideTip(true);
    closePop();
    document.querySelectorAll('#main-nav .opt').forEach((btn) => {
      const on = btn.dataset.view === view;
      btn.classList.toggle('on', on);
      btn.setAttribute('aria-selected', on ? 'true' : 'false');
    });
    syncSegThumb($('main-nav'), false);
    document.querySelectorAll('.view-panel').forEach((sec) => {
      sec.hidden = sec.id !== `view-${view}`;
    });
    requestAnimationFrame(() => syncAllSegs(true));
    if (view === 'telemetry') loadTelemetry();
    if (view === 'sessions') loadSessions();
    if (view === 'doctor') loadDoctor();
    if (view === 'storage') loadCache();
    if (view === 'settings') loadConfig();
  }

  function syncSSEPillTooltip() {
    const pill = $('sse-pill');
    if (!pill) return;
    const chip = $('top-running-chip')?.textContent || '0/0';
    const syncPart = state.syncMeta ? ` · ${state.syncMeta}` : '';
    if (pill.classList.contains('ok')) {
      pill.dataset.tip = `实时数据流已连接 · 在线 ${chip}${syncPart}`;
    } else if (pill.classList.contains('err')) {
      pill.dataset.tip = '后台实时连接中断，正在自动重连…';
    } else {
      pill.dataset.tip = '正在连接后台实时数据流…';
    }
  }

  // ---- 视图 1：总览 ----
  function renderOverview() {
    const ov = state.overview;
    if (!ov) return;

    if (ov.pricing) {
      state.usdToCny = ov.pricing.usdToCny || 7.2;
      if (!state.curOverridden && ov.pricing.currency) {
        state.currency = ov.pricing.currency;
      }
      state.mode = ov.pricing.mode || state.mode;
      syncFieldLabel('tel-mode-select');
    }
    syncCurrencyButtons();

    const ts = ov.updatedAt ? new Date(ov.updatedAt * 1000).toLocaleTimeString('zh-CN', { hour12: false }) : '--';
    state.syncMeta = `同步于 ${ts} (${Math.round(ov.pollMs || 0)}ms)`;

    const accounts = ov.accounts || [];
    const instances = ov.instances || [];
    syncAccountSelectOptions(accounts, instances);
    const onlineAccounts = accounts.filter((a) => (a.onlineCount || 0) > 0).length;
    const onlineInstances = instances.filter((i) => i.running).length;
    $('top-running-chip').textContent = accounts.length > 0
      ? `${onlineAccounts}/${accounts.length}`
      : `${onlineInstances}/${instances.length}`;
    syncSSEPillTooltip();

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

    const subUsd = roi.subUsd || 0;
    const acctCount = accounts.length || instances.length;
    const roiKpiHtml = subUsd > 0
      ? `<div class="kpi" data-tip="订阅基准 ${fmtMoney(subUsd)} · ${acctCount} 个账号">
          <b>${fmtMoney(roi.allMonthUsd)} <i class="u">${(roi.roiMultiple || 0).toFixed(1)}倍</i></b>
          <span>本月等值 / 回本率</span>
        </div>`
      : `<div class="kpi" data-tip="${acctCount} 个账号 · 在线 ${onlineInstances}/${instances.length}">
          <b>${fmtMoney(roi.allMonthUsd)}</b>
          <span>本月累计等值</span>
        </div>`;

    $('overview-kpis').innerHTML = `
      <div class="kpi" data-tip="近15分钟速率：${fmtMoney(burn.usdPerHour)}/小时 · ${fmtTokens(burn.tokensPerMin)}/分">
        <b>${fmtMoney(sum.totalUsd)}</b>
        <span>今日等效账单</span>
      </div>
      ${roiKpiHtml}
      <div class="kpi" data-tip="今日共 ${sum.calls || 0} 次模型调用">
        <b>${runCount} / ${waitCount > 0 ? `<span class="warn">${waitCount}</span>` : '0'}</b>
        <span>运行中 / 待确认</span>
      </div>
      <div class="kpi" data-tip="缓存已节省 ${fmtMoney(sum.savedByCacheUsd)}">
        <b>${(sum.cacheHitRate || 0).toFixed(1)}%</b>
        <span>缓存命中率</span>
      </div>
    `;

    renderColabRadar(ov.colab);

    const renderSlotButtons = (inst) => {
      if (!inst) return '';
      const isRO = !!inst.readOnlyLifecycle;
      if (isRO) {
        return inst.running
          ? `<button type="button" class="text" data-act="focus" data-id="${esc(inst.id)}" data-tip="将该客户端窗口置于前台">聚焦</button>`
          : '';
      }
      return inst.running
        ? `<button type="button" class="text" data-act="focus" data-id="${esc(inst.id)}" data-tip="将该客户端窗口置于前台">聚焦</button>
           <button type="button" class="text" data-act="heal" data-id="${esc(inst.id)}" data-tip="检查并修复实例软链与钩子">自愈</button>
           <button type="button" class="text danger" data-act="stop" data-id="${esc(inst.id)}" data-tip="安全关闭该客户端实例">停止</button>`
        : `<button type="button" class="text primary" data-act="launch" data-id="${esc(inst.id)}" data-tip="拉起该隔离客户端实例">启动</button>
           <button type="button" class="text" data-act="heal" data-id="${esc(inst.id)}" data-tip="检查并修复实例软链与钩子">自愈</button>
           <button type="button" class="text danger" data-act="delete" data-id="${esc(inst.id)}" data-tip="移除该隔离实例">删除</button>`;
    };

    const gridEl = $('instance-grid');
    const openFolds = new Set(
      Array.from(gridEl.querySelectorAll('details.acct-slots-fold[open][data-acct-fold]'))
        .map((d) => d.dataset.acctFold),
    );

    gridEl.innerHTML = accounts.length ? accounts.map((acct) => {
      const slots = acct.instances || [];
      const isOnline = (acct.onlineCount || 0) > 0;
      const isWait = acct.liveState === 'waiting_approval';
      const dotCls = !isOnline ? '' : isWait ? 'warn' : 'ok';
      const stateLabel = !isOnline
        ? '<span class="badge">离线</span>'
        : isWait
          ? '<span class="badge warn">待确认</span>'
          : acct.liveState === 'running'
            ? `<span class="badge ok">运行中 ${acct.activeSessions || 1}</span>`
            : '<span class="badge ok">在线空闲</span>';
      const cachedBadge = acct.cachedQuota ? '<span class="badge">离线快照</span>' : '';
      const plan = acct.planName ? `<span class="plan">${esc(acct.planName)}</span>` : '';

      let subText = '';
      if (slots.length === 1) {
        const s0 = slots[0];
        const metaVer = [s0.hub?.lsVersion ? `内核 ${s0.hub.lsVersion}` : '', s0.cdpPort ? `端口 ${s0.cdpPort}` : ''].filter(Boolean).join(' · ');
        subText = [s0.label || s0.id, metaVer, s0.readOnlyLifecycle ? '宿主实例' : '隔离实例'].filter(Boolean).join(' · ');
      } else {
        subText = `共 ${slots.length} 个客户端实例 · ${acct.onlineCount || 0} 个在线`;
      }
      const todayDetail = `今日 ${fmtMoney(acct.todayUsd)} · ${fmtTokens(acct.todayTokens)} · 缓存命中 ${(acct.todayCacheHitRate || 0).toFixed(0)}%`;
      const headTip = [subText, todayDetail].filter(Boolean).join(' | ');

      const buckets = sortQuotaBuckets(acct.buckets || []);
      const ctx = acct.context;
      const winCount = Math.min(4, buckets.length) + (ctx ? 1 : 0);
      const bars = buckets.slice(0, 4).map((b) => {
        if (b.disabled) {
          const qName = localizeQuotaName(b);
          const qTip = b.description || '当前周期暂不适用';
          return `
            <div class="quota muted" data-tip-title="${esc(qName)}" data-tip="${esc(qTip)}">
              <div class="quota-labels">
                <span class="quota-name">${esc(qName)}</span>
                <span class="quota-n">不适用</span>
              </div>
              <div class="quota-track"><i style="width:0%"></i></div>
            </div>`;
        }
        const pct = Math.max(0, Math.min(100, b.remainingPct || 0));
        const cls = pct <= 15 ? 'danger' : pct <= 35 ? 'warn' : '';
        const sparkCol = cls === 'danger' ? 'var(--red)' : cls === 'warn' ? 'var(--amber)' : 'var(--green)';
        const slotId = slots[0]?.id || '';
        const seriesKey = slotId && b.bucketId ? `${slotId}:${b.bucketId}` : '';
        const sparkPts = (seriesKey && state.quotaSeries?.[seriesKey]) || null;
        const sparkSVG = renderQuotaSparkSVG(sparkPts, sparkCol);
        const reset = fmtResetLeft(b.resetUnix);
        const resetText = reset ? (reset === '即将重置' ? reset : `${reset}后重置`) : (acct.cachedQuota ? '周期已重置' : '');
        const exhaustText = (b.exhaustsBeforeReset && b.hoursUntilEmpty > 0)
          ? `预计 ${b.hoursUntilEmpty.toFixed(1)} 小时耗尽`
          : '';
        const qName = localizeQuotaName(b);
        const qTip = [resetText, exhaustText].filter(Boolean).join(' · ') || '配额正常';
        return `
          <div class="quota" data-tip-title="${esc(qName)}" data-tip="${esc(qTip)}">
            <div class="quota-labels">
              <span class="quota-name">${esc(qName)}</span>
              ${sparkSVG}
              <span class="quota-n">${pct.toFixed(0)}%${exhaustText ? '<span class="q-warn-dot" aria-label="即将耗尽">!</span>' : ''}</span>
            </div>
            <div class="quota-track"><i class="${cls}" style="width:${pct}%"></i></div>
          </div>`;
      }).join('');

      let ctxBlock = '';
      if (ctx) {
        const cpct = Math.max(0, Math.min(100, ctx.usagePct || 0));
        const cCls = cpct >= 80 ? 'danger' : cpct >= 60 ? 'warn' : '';
        const ctxTitle = ctx.title || ctx.cascadeId || '当前会话';
        ctxBlock = `
          <div class="quota" data-tip-title="${esc(ctxTitle)}" data-tip="上下文 ${fmtTokens(ctx.estimatedTokensUsed)} / ${fmtTokens(ctx.maxContextTokens)}">
            <div class="quota-labels">
              <span class="quota-name">上下文</span>
              <span class="quota-n">${cpct.toFixed(0)}%</span>
            </div>
            <div class="quota-track"><i class="${cCls}" style="width:${cpct}%"></i></div>
          </div>`;
      }

      const slotsHTML = slots.length > 1
        ? `<details class="acct-slots-fold" data-acct-fold="${esc(acct.key)}"${openFolds.has(acct.key) ? ' open' : ''}>
            <summary>
              <span>${slots.length} 个客户端实例（${acct.onlineCount || 0} 在线）</span>
              <span class="fold-chev"><svg width="10" height="10" viewBox="0 0 10 10"><path d="M2.5 3.75 5 6.25 7.5 3.75" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg></span>
            </summary>
            <div class="acct-slots">${slots.map((s) => {
              const sDot = !s.running ? '' : s.liveState === 'waiting_approval' ? 'warn' : 'ok';
              const sMeta = [s.hub?.lsVersion ? `内核 ${s.hub.lsVersion}` : '', s.cdpPort ? `端口 ${s.cdpPort}` : '', s.readOnlyLifecycle ? '宿主' : '隔离'].filter(Boolean).join(' · ');
              return `
                <div class="acct-slot">
                  <span class="acct-slot-main"${sMeta ? ` data-tip="${esc(sMeta)}"` : ''}>
                    <span class="ci-dot ${sDot}"></span>
                    <b>${esc(s.label || s.id)}</b>
                  </span>
                  <div class="acts">${renderSlotButtons(s)}</div>
                </div>`;
            }).join('')}</div>
          </details>`
        : '';

      const singleActs = slots.length === 1 ? renderSlotButtons(slots[0]) : '';
      const hasUsage = Number(acct.todayUsd || 0) > 0 || Number(acct.todayTokens || 0) > 0;
      const footHTML = singleActs
        ? `<div class="subscription-foot">
            ${hasUsage ? `<span data-tip="吞吐 ${fmtTokens(acct.todayTokens)} · 缓存命中 ${(acct.todayCacheHitRate || 0).toFixed(0)}%">今日 ${fmtMoney(acct.todayUsd)}</span>` : '<span></span>'}
            <div class="acts">${singleActs}</div>
          </div>`
        : '';

      return `
        <article class="subscription-card ${isWait ? 'waiting' : ''}">
          <div class="subscription-head" data-tip-title="${esc(acct.displayName)}" data-tip="${esc(headTip)}">
            <span class="ci-dot ${dotCls}"></span>
            <b>${esc(acct.displayName)}</b>
            ${stateLabel}
            ${cachedBadge}
            ${plan}
          </div>
          <div class="quota-windows ${winCount <= 1 ? 'single' : ''}">
            ${bars}${ctxBlock}
            ${(!bars && !ctxBlock) ? `<span class="badge">${isOnline ? '同步中…' : '未运行'}</span>` : ''}
          </div>
          ${slotsHTML}
          ${footHTML}
        </article>`;
    }).join('') : `<div class="list" style="grid-column:1/-1">${renderEmptyState(
      '暂未发现客户端账号',
      '请先启动 Antigravity 客户端，或点击下方按钮创建多开隔离实例',
      '<button type="button" class="text primary" data-open-create="1">+ 新建隔离实例</button>',
    )}</div>`;
    syncActiveTipAfterRender();
  }

  function renderColabRadar(colab) {
    const host = $('colab-radar-host');
    if (!host) return;
    if (!colab || !colab.enabled) {
      host.innerHTML = '';
      return;
    }

    const cards = Array.isArray(colab.accounts) && colab.accounts.length > 0
      ? colab.accounts
      : (colab.status === 'empty' || (!colab.ok && colab.status === 'missing_cli'))
        ? []
        : [colab];

    const poolBal = Number(colab.currentBalance || 0);
    const poolRate = Number(colab.consumptionRateHourly || 0);
    const poolBurning = Number(colab.assignmentsCount || 0) > 0 || poolRate > 0.01;
    const poolOrphan = Number(colab.orphanCount || 0) > 0;

    const poolMetaHtml = cards.length
      ? `<span class="badge mono" data-tip="${poolBurning ? `实时消耗 ${poolRate.toFixed(2)} 点/小时` : '当前无活跃虚拟机'}">${cards.length} 账号 · ${poolBal.toFixed(1)} 点</span>`
      : '';

    const renderColabCard = (ca) => {
      const bal = Number(ca.currentBalance || 0);
      const allowance = Number(ca.monthlyAllowance || colab.monthlyAllowance || 2000);
      const rate = Number(ca.consumptionRateHourly || 0);
      const activeCount = Number(ca.assignmentsCount || 0);
      const orphanCount = Number(ca.orphanCount || 0);
      const burning = activeCount > 0 || rate > 0.01;
      const hasOrphan = orphanCount > 0;
      const email = ca.account || '';

      const checkedTime = ca.checkedAt ? new Date(ca.checkedAt * 1000).toLocaleTimeString('zh-CN', { hour12: false }) : '--';
      const subMsg = ca.ok ? `巡检于 ${checkedTime} · 运行中 ${activeCount} 台` : (ca.lastError || '授权已过期，请重新登录');

      let statusBadge = `<span class="badge ok" data-tip="${esc(subMsg)}">空闲</span>`;
      if (!ca.ok) {
        const label = ca.status === 'unauthenticated' ? '需重新授权' : ca.status === 'missing_cli' ? '待配置' : '巡检异常';
        statusBadge = `<span class="badge ${ca.status === 'unauthenticated' ? 'warn' : 'err'}" data-tip="${esc(subMsg)}">${label}</span>`;
      } else if (hasOrphan) {
        statusBadge = `<span class="badge err" data-tip="${esc(subMsg)}">孤儿实例 ${orphanCount} 台</span>`;
      } else if (burning) {
        statusBadge = `<span class="badge warn" data-tip="${esc(subMsg)}">运行中 ${activeCount || 1} 台 · ${ca.activeMinutes || 0}分</span>`;
      }

      const srcBadge = ca.source === 'oauth'
        ? '<span class="badge accent">网页授权</span>'
        : ca.source === 'colab-safe'
          ? '<span class="badge">系统钥匙串</span>'
          : '';
      const pct = allowance > 0 ? Math.max(0, Math.min(100, (bal / allowance) * 100)) : 0;
      const fillCls = ca.ok ? (pct <= 10 ? 'danger' : pct <= 25 ? 'warn' : '') : '';

      const gpuPills = (ca.eligibleGpus || []).map((g) => `<span class="badge accent">${esc(g)}</span>`).join('');
      const tpuPills = (ca.eligibleTpus || []).map((t) => `<span class="badge">TPU ${esc(t)}</span>`).join('');

      const sessions = ca.sessions || [];
      const sessHTML = sessions.length
        ? `<div class="colab-sess">${sessions.map((s) => `
            <div class="colab-sess-item">
              <span><b>${s.orphan ? '孤儿实例' : esc(s.name || '计算节点')}</b> <span class="badge ${s.orphan ? 'err' : 'warn'}">${esc(s.accelerator || '加速卡')}</span> ${esc(s.shape || '')}</span>
              <span class="mono" style="color:var(--muted)">${esc(s.endpoint)}</span>
            </div>`).join('')}</div>`
        : '';

      const reloginBtn = (ca.source === 'oauth' || ca.status === 'unauthenticated')
        ? `<button type="button" class="text" data-colab-act="relogin" data-email="${esc(email)}">重新授权</button>`
        : '';
      const nukeBtn = ca.ok
        ? `<button type="button" class="text ${burning || hasOrphan ? 'danger' : ''}" data-colab-act="nuke" data-email="${esc(email)}" data-active="${activeCount}" data-orphan="${orphanCount}">一键清场</button>`
        : '';
      const removeBtn = (ca.removable && email)
        ? `<button type="button" class="text" data-colab-act="remove" data-email="${esc(email)}" data-source="${esc(ca.source || '')}">${ca.source === 'colab-safe' ? '隐藏卡片' : '解绑账号'}</button>`
        : '';
      const hasActs = Boolean(reloginBtn || nukeBtn || removeBtn);
      const balTip = ca.ok ? `估值 ${fmtMoney(ca.estimatedValueUsd)} / 月度基准 ${Math.round(allowance)}` : '';
      const rateTip = !ca.ok ? '' : burning ? `${fmtMoney(ca.burnUsdPerHour)}/小时 · ${ca.hoursUntilEmpty > 0 ? `约 ${ca.hoursUntilEmpty.toFixed(1)} 小时耗尽` : '消耗中'}` : '当前无活跃虚拟机';

      return `
        <article class="subscription-card${hasOrphan ? ' orphan' : burning ? ' burning' : ''}">
          <div class="subscription-head">
            <span class="ci-dot ${!ca.ok ? 'err' : hasOrphan ? 'err' : burning ? 'warn' : 'ok'}"></span>
            <b data-tip="${esc(email || '云算力账号')}">${esc(email || '云算力账号')}</b>
            ${statusBadge}
            ${srcBadge}
          </div>
          <div class="colab-cols">
            <div class="colab-col"${balTip ? ` data-tip="${esc(balTip)}"` : ''}>
              <span class="k">剩余算力点</span>
              <span class="v">${ca.ok ? `${bal.toFixed(1)}` : '--'}</span>
              <div class="quota-track"><i class="${fillCls}" style="width:${pct}%"></i></div>
            </div>
            <div class="colab-col"${rateTip ? ` data-tip="${esc(rateTip)}"` : ''}>
              <span class="k">实时消耗速率</span>
              <span class="v" style="${burning ? 'color:var(--amber)' : ''}">${ca.ok ? `${rate.toFixed(2)}/小时` : '--'}</span>
            </div>
            <div class="colab-col">
              <span class="k">可用加速型号</span>
              <div class="colab-chips">${gpuPills || tpuPills || '<span class="mono" style="color:var(--muted)">--</span>'}</div>
            </div>
          </div>
          ${sessHTML}
          ${hasActs ? `<div class="subscription-foot"><span></span><div class="acts">${reloginBtn}${nukeBtn}${removeBtn}</div></div>` : ''}
        </article>`;
    };

    const bodyHTML = cards.length > 0
      ? cards.map(renderColabCard).join('')
      : `<div class="list">${renderEmptyState(
          '暂未绑定云算力账号',
          '绑定 Google 账号后可实时巡检算力点余额、燃烧速率与残留孤儿虚拟机，防止忘记关机白扣配额',
          '<button type="button" class="text primary" data-colab-act="bind-menu">+ 绑定云算力账号</button>',
        )}</div>`;

    host.innerHTML = `
      <div class="row-head">
        <span class="label">云端算力（Colab）</span>
        <div class="acts colab-head-tools">
          ${poolMetaHtml}
          <button type="button" class="text" data-colab-act="refresh">立即巡检</button>
          <button type="button" class="text primary" data-colab-act="bind-menu" id="btn-colab-bind">+ 绑定账号</button>
          ${(poolBurning || poolOrphan) ? `<button type="button" class="text danger" data-colab-act="nuke" data-email="" data-active="${colab.assignmentsCount || 0}" data-orphan="${colab.orphanCount || 0}">全池清场</button>` : ''}
        </div>
      </div>
      <div class="subscription-usage${cards.length <= 1 ? ' single' : ''}">
        ${bodyHTML}
      </div>
    `;
  }

  // ---- 视图 2：账单与趋势 ----
  async function loadTelemetry() {
    try {
      const qsStr = new URLSearchParams({
        preset: state.preset,
        mode: state.mode,
        instance: state.telInst,
      }).toString();
      const [res, hm] = await Promise.all([
        api('/api/telemetry?' + qsStr),
        api('/api/telemetry/heatmap?' + new URLSearchParams({ instance: state.telInst, mode: state.mode, weeks: '26' }).toString()).catch(() => null),
      ]);
      state.telemetry = res;
      if (hm && hm.ok) state.heatmap = hm;
      if (res.pricing) state.usdToCny = res.pricing.usdToCny || state.usdToCny;
      renderTelemetry();
      syncSegThumb($('telemetry-presets'), true);
    } catch (e) {
      showStatus('加载账单失败：' + humanizeError(e), 'err');
    }
  }

  function renderTelemetry() {
    const data = state.telemetry;
    if (!data || !data.report) return;
    const rep = data.report;
    const sum = rep.summary || {};

    const pLabel = localizePresetLabel(rep.presetLabel, state.preset);
    const gLabel = localizeGranularity(rep.granularity);
    state.defaultChartMeta = [pLabel, gLabel].filter(Boolean).join(' · ');
    const chartTipEl = $('chart-tooltip');
    if (chartTipEl) {
      chartTipEl.textContent = state.defaultChartMeta;
      chartTipEl.dataset.tip = `扫描耗时 ${(rep.scanMs || 0).toFixed(1)}ms`;
    }

    $('tel-kpis').innerHTML = `
      <div class="kpi" data-tip="共 ${sum.calls || 0} 次调用${sum.imageCalls ? ` · ${sum.imageCalls} 次生图` : ''}">
        <b>${fmtMoney(sum.totalUsd)}</b>
        <span>${esc(pLabel)}等效账单</span>
      </div>
      <div class="kpi" data-tip="输入 ${fmtTokens(sum.inputTokens)} · 输出 ${fmtTokens(sum.outputTokens)}">
        <b>${fmtTokens(sum.totalTokens)}</b>
        <span>总吞吐量</span>
      </div>
      <div class="kpi" data-tip="命中率 ${(sum.cacheHitRate || 0).toFixed(1)}% · 已省 ${fmtMoney(sum.savedByCacheUsd)}">
        <b>${fmtTokens(sum.cacheTokens)}</b>
        <span>缓存命中读取</span>
      </div>
      <div class="kpi" data-tip="${fmtTokens(rep.burnRate15m?.tokensPerMin)}/分 · ${rep.burnRate15m?.calls15m || 0} 次调用">
        <b>${fmtMoney(rep.burnRate15m?.usdPerHour)}/小时</b>
        <span>近15分钟速率</span>
      </div>
    `;

    renderTrendSVG(rep.trend || []);
    renderHeatmapCard(state.heatmap);

    const models = rep.byModel || [];
    const maxModelUsd = Math.max(0.0001, ...models.map((m) => m.totalUsd || 0));
    $('tel-models-list').innerHTML = models.length ? models.map((m) => {
      const sharePct = Math.max(2, Math.round(((m.totalUsd || 0) / maxModelUsd) * 100));
      const mTip = `${m.calls || 0} 次调用 · 缓存命中 ${fmtTokens(m.cacheTokens)} · 输入 ${fmtTokens(m.inputTokens)} / 输出 ${fmtTokens(m.outputTokens)}`;
      return `
        <div class="row stat" data-tip-title="${esc(m.modelName)}" data-tip="${esc(mTip)}">
          <div class="who">
            <div class="name">${esc(m.modelName)}</div>
          </div>
          <div class="share"><i style="width:${sharePct}%"></i></div>
          <div class="num"><b>${fmtTokens(m.totalTokens)}</b></div>
          <div class="cost">${fmtMoney(m.totalUsd)}</div>
        </div>`;
    }).join('') : renderEmptyState('暂无模型统计', '所选时间区间内没有模型调用记录');

    const accts = (rep.byAccount && rep.byAccount.length) ? rep.byAccount : (rep.byInstance || []);
    const maxAcctUsd = Math.max(0.0001, ...accts.map((a) => a.totalUsd || 0));
    $('tel-inst-list').innerHTML = accts.length ? accts.map((a) => {
      const sharePct = Math.max(2, Math.round(((a.totalUsd || 0) / maxAcctUsd) * 100));
      const label = a.account || a.instanceId || '--';
      const slots = (a.instanceIds && a.instanceIds.length > 1) ? ` · 实例 ${a.instanceIds.join(' + ')}` : '';
      const aTip = `${a.calls || 0} 次调用 · 缓存命中 ${(a.cacheRate || 0).toFixed(1)}%${slots}`;
      return `
        <div class="row stat" data-tip-title="${esc(label)}" data-tip="${esc(aTip)}">
          <div class="who">
            <div class="name">${esc(label)}</div>
          </div>
          <div class="share"><i style="width:${sharePct}%"></i></div>
          <div class="num"><b>${fmtTokens(a.totalTokens)}</b></div>
          <div class="cost">${fmtMoney(a.totalUsd)}</div>
        </div>`;
    }).join('') : renderEmptyState('暂无账号统计', '所选时间区间内没有账号消耗记录');

    const calls = rep.recentCalls || [];
    $('tel-calls-body').innerHTML = calls.length ? calls.map((rc, idx) => `
      <tr class="clickable" data-call-idx="${idx}" data-tip="点击查看此次调用详情">
        <td class="mono">${esc(rc.timeStr)}</td>
        <td class="clip" data-tip="${esc(rc.account || rc.instanceId)}">${esc(rc.account || rc.instanceId)}</td>
        <td class="clip" data-tip="${esc(rc.title || rc.cascadeId || '未命名会话')}">${esc(rc.title || rc.cascadeId || '未命名会话')}</td>
        <td class="clip">${esc(rc.modelName)}</td>
        <td>${fmtTokens(rc.inputTokens)}</td>
        <td>${fmtTokens(rc.cacheTokens)}</td>
        <td>${fmtTokens(rc.outputTokens)}</td>
        <td class="mono">${rc.tps > 0 ? `${rc.tps.toFixed(1)} t/s` : '--'}${rc.ttftMs > 0 ? ` · ${fmtMsShort(rc.ttftMs)}` : ''}</td>
        <td style="color:var(--green);font-weight:600">${fmtMoney(rc.usd)}</td>
      </tr>
    `).join('') : '<tr><td colspan="9" style="padding:22px 12px;color:var(--muted);text-align:center">暂无调用明细</td></tr>';
    syncActiveTipAfterRender();
  }

  function renderHeatmapCard(hm) {
    const card = $('tel-heatmap-card');
    const host = $('tel-heatmap-host');
    if (!card || !host) return;
    const days = hm?.days || [];
    if (!days.length) {
      card.hidden = true;
      return;
    }
    card.hidden = false;
    let cols = '';
    for (let i = 0; i < days.length; i += 7) {
      const colDays = days.slice(i, i + 7);
      cols += `<div class="hm-col-7">${colDays.map((d) => {
        const tip = `${d.date} · ${d.calls} 次调用 · ${fmtTokens(d.tokens)} tok · ${fmtMoney(d.costUsd)}`;
        return `<span class="hm-dot" data-l="${d.level || 0}" data-tip="${esc(tip)}"></span>`;
      }).join('')}</div>`;
    }
    host.innerHTML = cols;
    host.onmouseover = (e) => {
      const cell = e.target.closest?.('.hm-dot[data-tip]');
      if (cell && $('hm-tooltip')) $('hm-tooltip').textContent = cell.getAttribute('data-tip') || '';
    };
    host.onmouseleave = () => {
      if ($('hm-tooltip')) $('hm-tooltip').textContent = '';
    };
  }

  function renderTrendSVG(trend) {
    const host = $('tel-chart-host');
    const pop = $('chart-hover-pop');
    if (pop) pop.hidden = true;
    if (!trend.length) {
      host.innerHTML = renderEmptyState('暂无趋势数据', '可切换顶部时间区间（如「近7天」或「本月」）查看用量分布');
      return;
    }

    const availW = Math.max(296, (host.clientWidth || 340));
    const maxTok = Math.max(1, ...trend.map((b) => (b.inputTokens || 0) + (b.cacheTokens || 0) + (b.outputTokens || 0)));
    const H = 152;
    const padB = 22;
    const padT = 8;
    const chartH = H - padB - padT;
    const gap = trend.length > 24 ? 4 : 6;
    const idealBarW = Math.floor((availW - 16 - Math.max(0, trend.length - 1) * gap) / Math.max(trend.length, 6));
    const barW = Math.max(9, Math.min(28, idealBarW));
    const W = Math.max(availW, trend.length * (barW + gap) + 16);

    const bars = trend.map((b, i) => {
      const x = 8 + i * (barW + gap);
      const hCache = Math.round(((b.cacheTokens || 0) / maxTok) * chartH);
      const hIn = Math.round(((b.inputTokens || 0) / maxTok) * chartH);
      const hOut = Math.round(((b.outputTokens || 0) / maxTok) * chartH);
      const yCache = H - padB - hCache;
      const yIn = yCache - hIn;
      const yOut = yIn - hOut;
      const maxLabels = availW < 420 ? 6 : 12;
      const showLbl = trend.length <= maxLabels || i % Math.ceil(trend.length / maxLabels) === 0;
      return `
        <g class="tbar" data-tbar-idx="${i}" data-x="${x + barW / 2}">
          <rect x="${x}" y="${padT}" width="${barW}" height="${chartH}" rx="3" fill="transparent"></rect>
          <rect x="${x}" y="${yCache}" width="${barW}" height="${Math.max(hCache > 0 ? 2 : 0, hCache)}" rx="2" fill="var(--c3)"></rect>
          <rect x="${x}" y="${yIn}" width="${barW}" height="${hIn}" fill="var(--c2)"></rect>
          <rect x="${x}" y="${yOut}" width="${barW}" height="${hOut}" rx="2" fill="var(--c4)"></rect>
          ${showLbl ? `<text x="${x + barW / 2}" y="${H - 6}" text-anchor="middle" font-size="10" fill="var(--muted)">${esc(b.label)}</text>` : ''}
        </g>`;
    }).join('');

    host.innerHTML = `<svg viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" style="display:block;min-width:100%">${bars}</svg>`;

    const showBarTip = (g) => {
      const idx = Number(g.dataset.tbarIdx);
      const b = trend[idx];
      if (!b) return;
      const tot = (b.inputTokens || 0) + (b.cacheTokens || 0) + (b.outputTokens || 0);
      $('chart-tooltip').textContent = `${b.label} · ${b.calls || 0} 次调用 · ${fmtMoney(b.totalUsd)} · 共 ${fmtTokens(tot)}`;
      if (!pop) return;
      pop.innerHTML = `
        <div class="chart-pop-head">
          <span>${esc(b.label)}（${b.calls || 0} 次调用）</span>
          <em>${fmtMoney(b.totalUsd)}</em>
        </div>
        <div class="chart-pop-row"><span><i style="background:var(--c3)"></i>缓存命中</span><b>${fmtTokens(b.cacheTokens)}</b></div>
        <div class="chart-pop-row"><span><i style="background:var(--c2)"></i>输入</span><b>${fmtTokens(b.inputTokens)}</b></div>
        <div class="chart-pop-row"><span><i style="background:var(--c4)"></i>输出</span><b>${fmtTokens(b.outputTokens)}</b></div>
      `;
      pop.hidden = false;
      const stageW = host.parentElement ? host.parentElement.clientWidth : availW;
      const popW = pop.offsetWidth || 184;
      const barCenter = Number(g.dataset.x || 0) - (host.scrollLeft || 0);
      const left = Math.max(6, Math.min(stageW - popW - 6, Math.round(barCenter - popW / 2)));
      pop.style.left = left + 'px';
    };

    host.querySelectorAll('.tbar').forEach((g) => {
      g.addEventListener('mouseenter', () => showBarTip(g));
      g.addEventListener('click', () => showBarTip(g));
    });
    host.addEventListener('mouseleave', () => {
      if (pop) pop.hidden = true;
      $('chart-tooltip').textContent = state.defaultChartMeta;
    });
  }

  // ---- 视图 3：会话与上下文 ----
  async function loadSessions() {
    try {
      const q = $('session-search-input').value.trim();
      const data = await api('/api/sessions?' + new URLSearchParams({ q, instance: state.searchInst, limit: '30' }).toString());
      state.sessions = data;
      renderSessions();
    } catch (e) {
      showStatus('加载会话失败：' + humanizeError(e), 'err');
    }
  }

  function renderSessions() {
    const data = state.sessions;
    if (!data) return;

    const live = data.live || [];
    $('live-sessions-list').innerHTML = live.length ? live.map((s) => {
      const wait = s.state === 'waiting_approval';
      const cls = wait ? 'warn' : s.state === 'running' ? 'ok' : s.state === 'error' ? 'err' : '';
      const label = wait ? '待确认' : s.state === 'running' ? '运行中' : s.state === 'error' ? '运行异常' : '空闲';
      const modelLabel = (!s.model || s.model.toLowerCase() === 'unknown') ? '默认模型' : s.model;
      const liveTip = `${s.workspace || '未绑定工作区'} · ${modelLabel} · 已运行 ${fmtDurationZh(s.durationSec)}${s.lastTool ? ` · 最近工具 ${s.lastTool}` : ''}`;
      return `
        <div class="row" data-tip="${esc(liveTip)}">
          <span class="ci-dot ${cls}"></span>
          <div class="who">
            <div class="name"><span class="mono">${esc(s.instanceId)}</span> · ${esc(s.title || s.conversationId || '活跃会话')}</div>
          </div>
          <span class="badge ${cls}">${label}</span>
          <button type="button" class="text" data-act="focus" data-id="${esc(s.instanceId)}">聚焦窗口</button>
        </div>`;
    }).join('') : renderEmptyState('当前没有活跃会话', '当客户端正在执行对话任务或等待人工确认时，会在此处实时显示进度');

    const ctxs = data.contexts || [];
    $('context-cards').innerHTML = ctxs.length ? ctxs.map((entry, idx) => {
      const c = entry.context || {};
      const pct = Math.max(0, Math.min(100, c.usagePct || 0));
      const cls = pct >= 80 ? 'danger' : pct >= 60 ? 'warn' : '';
      const owner = entry.account || entry.instanceId;
      const topGroups = (c.tokenBreakdown || []).slice(0, 4).map((g) => `${localizeContextGroup(g.name)} ${fmtTokens(g.numTokens)}`).join(' · ');
      const ctxTip = [c.model || '默认模型', topGroups, '点击查看 /context 构成与增长曲线'].filter(Boolean).join(' | ');
      return `
        <div class="subscription-card clickable" data-ctx-idx="${idx}" data-tip="${esc(ctxTip)}">
          <div class="subscription-head">
            <b>${esc(owner)}</b>
            <span class="inst-name">${esc(c.title || c.cascadeId || '当前会话')}</span>
            <span class="badge ${pct >= 80 ? 'err' : pct >= 60 ? 'warn' : 'accent'}" style="margin-left:auto">${pct.toFixed(1)}%</span>
          </div>
          <div class="quota-track" style="margin:6px 0"><i class="${cls}" style="width:${pct}%"></i></div>
          <div class="ctx-foot-meta">
            <span class="mono">${fmtTokens(c.estimatedTokensUsed)} / ${fmtTokens(c.maxContextTokens)}</span>
            ${pct >= 80 ? '<span class="badge warn">建议压缩</span>' : `<span>${esc(c.model || '默认模型')}</span>`}
          </div>
        </div>`;
    }).join('') : `<div class="list" style="grid-column:1/-1">${renderEmptyState('暂无活跃上下文窗口', '客户端启动并加载会话后，将自动展示实时上下文水位与构成占比')}</div>`;

    const hits = data.hits || [];
    const hasQuery = $('session-search-input').value.trim().length > 0;
    $('session-hits').innerHTML = hits.length ? hits.map((h, idx) => {
      const hitTip = `${h.workspace || '无工作区路径'} · ${h.stepCount || 0} 步对话${h.modifiedAt ? ` · ${h.modifiedAt}` : ''}`;
      return `
        <div class="row clickable" data-hit-idx="${idx}" data-tip="${esc(hitTip)}">
          <span class="badge">${esc(h.account || h.instanceId)}</span>
          <div class="who">
            <div class="name">${esc(h.title || h.cascadeId || '未命名会话')}</div>
            ${hasQuery && h.snippet ? `<div class="sub hit-snippet" style="color:var(--fg-2)">${esc(h.snippet)}</div>` : ''}
          </div>
        </div>`;
    }).join('') : renderEmptyState(
      hasQuery ? '未找到匹配的会话记录' : '暂无历史会话记录',
      hasQuery ? '请尝试更换搜索关键词，或将右侧筛选切换为「全部账号」' : '本地会话数据库产生记录后可在此全文检索',
    );
    syncActiveTipAfterRender();
  }

  // ---- 视图 4：体检与窗口注入 ----
  async function loadDoctor() {
    try {
      state.doctor = await api('/api/doctor');
      renderDoctor();
    } catch (e) {
      showStatus('执行体检失败：' + humanizeError(e), 'err');
    }
  }

  function renderDoctor() {
    const rep = state.doctor;
    if (!rep) return;
    const verParts = [`客户端版本 ${rep.mainVersion || '未检测'}`];
    if (rep.ideVersion) verParts.push(`内核 ${rep.ideVersion}`);
    const warnErr = (rep.warnCount || 0) + (rep.errorCount || 0);
    verParts.push(`${rep.okCount || 0} 项正常 · ${warnErr} 项需关注`);
    const docVerEl = $('doctor-versions-sub');
    if (docVerEl) {
      docVerEl.textContent = `${rep.mainVersion || '--'} · ${rep.okCount || 0}/${(rep.okCount || 0) + warnErr} 正常`;
      docVerEl.dataset.tip = verParts.join(' · ');
    }

    const docGrid = $('doctor-grid');
    const openDocs = new Set(
      Array.from(docGrid.querySelectorAll('details.doc-item[open][data-doc-key]'))
        .map((d) => d.dataset.docKey),
    );
    const statusTextMap = { ok: '正常', warn: '警告', error: '异常' };
    docGrid.innerHTML = (rep.checks || []).length ? (rep.checks || []).map((c) => {
      const cls = c.status === 'ok' ? 'ok' : c.status === 'error' ? 'err' : 'warn';
      const label = statusTextMap[c.status] || '提示';
      const detailText = [c.summary, c.detail].filter(Boolean).join(' · ');
      const docKey = String(c.id || c.title || '');
      return `
        <details class="doc-item" data-doc-key="${esc(docKey)}"${openDocs.has(docKey) ? ' open' : ''}>
          <summary class="row"${detailText ? ` data-tip="${esc(detailText)}"` : ''}>
            <span class="ci-dot ${cls}"></span>
            <div class="who"><div class="name">${esc(c.title)}</div></div>
            <span class="badge ${cls}">${label}</span>
          </summary>
          ${detailText ? `<div class="doc-detail">${esc(detailText)}</div>` : ''}
        </details>`;
    }).join('') : renderEmptyState('暂无体检项目', '正在等待后台返回环境诊断结果');

    const targets = state.overview?.cdpTargets || [];
    const zhEnabled = !!state.overview?.cdpZhEnabled;
    $('cdp-table-body').innerHTML = targets.length ? targets.map((t, idx) => `
      <tr class="clickable" data-cdp-idx="${idx}" data-tip="点击查看窗口注入诊断详情">
        <td class="mono">${esc(t.instanceId)}</td>
        <td class="mono">${t.port}</td>
        <td class="clip" data-tip="${esc(t.title)}">${esc(t.title || '客户端窗口')}</td>
        <td>${t.zhInjected ? '<span class="badge ok">已加载</span>' : zhEnabled ? '<span class="badge warn">待注入</span>' : '<span class="badge">未开启</span>'}</td>
        <td>${t.hudActive ? '<span class="badge ok">已就绪</span>' : '<span class="badge">未挂载</span>'}</td>
        <td>
          <button type="button" class="text ${t.lastError ? 'danger' : ''}" data-cdp-idx="${idx}" style="height:22px;padding:0 8px;font-size:11.5px">
            ${t.lastError ? '查看异常' : '查看详情'}
          </button>
        </td>
      </tr>
    `).join('') : '<tr><td colspan="6" style="padding:22px 12px;color:var(--muted);text-align:center">暂无运行中的 CDP 注入目标窗口（Antigravity 2.33+ 已关闭默认 CDP 端口，可直接使用插件 MCP App 或 Web 控制台）</td></tr>';
    syncActiveTipAfterRender();
  }

  // ---- 视图 5：本地缓存管理 ----
  async function loadCache() {
    try {
      const keep = Number($('cache-keep-input').value || 20);
      state.cache = await api(`/api/cache?keep=${keep}`);
      renderCache();
    } catch (e) {
      showStatus('扫描缓存失败：' + humanizeError(e), 'err');
    }
  }

  function getFilteredCacheItems(allItems) {
    return (allItems || []).filter((it) => {
      if (state.cacheInst === 'all') return true;
      if (state.cacheInst.startsWith('acct:')) {
        const target = state.cacheInst.slice(5).toLowerCase();
        return (it.account || '').toLowerCase() === target;
      }
      return it.instanceId === state.cacheInst;
    });
  }

  function renderCache() {
    const sum = state.cache;
    if (!sum) return;
    $('cache-kpis').innerHTML = `
      <div class="kpi"><b>${fmtBytes(sum.totalBytes)}</b><span>本地缓存总占用</span></div>
      <div class="kpi"><b style="color:var(--accent)">${fmtBytes(sum.reclaimableBytes)}</b><span>可安全释放</span></div>
      <div class="kpi"><b>${sum.keepCount} 条</b><span>单实例保留数</span></div>
      <div class="kpi"><b>${(sum.items || []).length} 条</b><span>已扫描会话</span></div>
    `;

    const items = getFilteredCacheItems(sum.items).slice(0, 120);
    $('cache-body').innerHTML = items.length ? items.map((it) => `
      <tr>
        <td class="clip" data-tip="${esc(it.instanceId)}">${esc(it.account || it.instanceId)}</td>
        <td class="clip" data-tip="${esc(it.uuid)}">${esc(it.title || it.uuid)}</td>
        <td>${fmtBytes(it.brainBytes)}</td>
        <td>${fmtBytes(it.convBytes)}</td>
        <td><b>${fmtBytes(it.totalBytes)}</b></td>
        <td>${it.protected ? '<span class="badge ok">受保护保留</span>' : '<span class="badge warn">可安全清理</span>'}</td>
      </tr>
    `).join('') : '<tr><td colspan="6" style="padding:22px 12px;color:var(--muted);text-align:center">暂无本地缓存条目</td></tr>';
    syncActiveTipAfterRender();
  }

  // ---- 视图 6：系统设置 ----
  async function loadConfig(notifyLoaded = false) {
    try {
      const cfg = await api('/api/config?masked=1');
      state.config = cfg;
      state.cfgLang = cfg.language || 'auto';
      if (window.AgyI18n && cfg.language && cfg.language !== 'auto') {
        window.AgyI18n.setPreference(cfg.language, { persist: false });
      }
      setSwitch('cfg-cdp-enabled', !!cfg.cdp?.enabled);
      setSwitch('cfg-cdp-zh', !!cfg.cdp?.zhInject);
      setSwitch('cfg-cdp-hud', !!cfg.cdp?.windowHud);
      state.cdpPos = cfg.cdp?.hudPosition || 'top-right';
      $('cfg-cdp-path').value = cfg.cdp?.zhScriptPath || '';
      $('cfg-cdp-insts').value = (cfg.cdp?.hudInstances || []).join(',');

      setSwitch('cfg-tg-enabled', !!cfg.telegram?.enabled);
      $('cfg-tg-token').value = cfg.telegram?.botToken || '';
      $('cfg-tg-chatid').value = cfg.telegram?.chatId || '';
      $('cfg-tg-mindur').value = cfg.telegram?.notifyMinDurationSec ?? 60;
      $('cfg-tg-stuck').value = cfg.telegram?.notifyOnStuckSec ?? 45;
      setSwitch('cfg-tg-stop', !!cfg.telegram?.notifyOnStop);
      setSwitch('cfg-tg-err', !!cfg.telegram?.notifyOnError);
      setSwitch('cfg-tg-lowq', !!cfg.telegram?.notifyLowQuota);

      setSwitch('cfg-wh-enabled', !!cfg.webhook?.enabled);
      if ($('cfg-wh-url')) $('cfg-wh-url').value = cfg.webhook?.url || '';
      state.cfgWhFormat = cfg.webhook?.format || 'auto';
      if ($('cfg-wh-secret')) $('cfg-wh-secret').value = cfg.webhook?.secret || '';

      state.cfgPriceMode = cfg.pricing?.mode || 'std';
      state.cfgPriceCur = cfg.pricing?.currency || 'USD';
      $('cfg-price-rate').value = cfg.pricing?.usdToCny ?? 7.20;
      $('cfg-price-sub').value = cfg.pricing?.ultraMonthlyUsd ?? 0;
      $('cfg-warn-quota').value = cfg.quotaWarnPct ?? 20;
      $('cfg-warn-ctx').value = cfg.contextWarnPct ?? 80;

      setSwitch('cfg-colab-enabled', !!cfg.colab?.enabled);
      $('cfg-colab-poll').value = cfg.colab?.pollIntervalSec ?? 60;
      $('cfg-colab-allowance').value = cfg.colab?.monthlyAllowance ?? 2000;
      $('cfg-colab-warnmin').value = cfg.colab?.warnActiveMinutes ?? 30;
      $('cfg-colab-lowccu').value = cfg.colab?.lowBalanceCcu ?? 200;
      $('cfg-colab-cli').value = cfg.colab?.cliPath || '~/.local/bin/colab-safe';
      syncAllSelectLabels();
      if (notifyLoaded) showStatus('已重新读取当前配置', 'ok');
    } catch (e) {
      showStatus('读取配置失败：' + humanizeError(e), 'err');
    }
  }

  function syncCurrencyButtons() {
    document.querySelectorAll('#cur-seg .opt[data-currency]').forEach((b) => {
      b.classList.toggle('on', b.dataset.currency === state.currency);
    });
    syncSegThumb($('cur-seg'), false);
  }

  async function loadOverview(force = false) {
    const refreshBtn = $('btn-refresh');
    if (force && refreshBtn) refreshBtn.classList.add('spin');
    try {
      const [ov, qh] = await Promise.all([
        api('/api/overview' + (force ? '?refresh=1' : '')),
        api('/api/quota/history?max=36').catch(() => null),
      ]);
      state.overview = ov;
      if (qh && qh.series) state.quotaSeries = qh.series;
      renderOverview();
      if (state.view === 'doctor') renderDoctor();
    } catch (e) {
      showStatus('刷新数据失败：' + humanizeError(e), 'err');
    } finally {
      if (refreshBtn) refreshBtn.classList.remove('spin');
    }
  }

  // ---- 详情弹窗视图组装（调用详情 / 上下文详情 / 历史会话 / 窗口注入诊断） ----
  function render100DotGridHTML(groups, maxTok) {
    if (!Array.isArray(groups) || !groups.length || !maxTok) return '';
    const colors = {
      system: '#64748b',
      custom: '#8b5cf6',
      user: '#0ea5e9',
      agent: '#3b82f6',
      tool: '#f59e0b',
    };
    const buckets = { system: 0, custom: 0, user: 0, agent: 0, tool: 0 };
    for (const g of groups) {
      const key = String(g.key || '').toLowerCase();
      const name = String(g.name || '').toLowerCase();
      const tok = Number(g.numTokens || 0);
      if (key === 'mcp' || key === 'skills' || key === 'rules' || key === 'subagents' || key === 'workflows' || key === 'plugins' || /mcp|技能|规则|子代理|插件/i.test(name)) buckets.custom += tok;
      else if (key === 'user' || key === 'chat' || /用户输入|对话/i.test(name)) buckets.user += tok;
      else if (key === 'agent' || key === 'thinking' || /模型|思考/i.test(name)) buckets.agent += tok;
      else if (key === 'tool_output' || /工具结果|工具输出/i.test(name)) buckets.tool += tok;
      else buckets.system += tok;
    }
    const order = [
      ['system', '系统与工具'],
      ['custom', '规则/技能/MCP'],
      ['user', '用户与对话'],
      ['agent', '模型与思考'],
      ['tool', '工具输出'],
    ];
    let counts = order.map(([k]) => (buckets[k] > 0 ? Math.max(1, Math.floor((buckets[k] / maxTok) * 100)) : 0));
    const sum = counts.reduce((a, b) => a + b, 0);
    if (sum > 100) counts = counts.map((c) => Math.floor((c * 100) / sum));
    const free = Math.max(0, 100 - counts.reduce((a, b) => a + b, 0));
    let html = '<div class="dot-grid-100">';
    counts.forEach((n, idx) => {
      const [k, label] = order[idx];
      for (let i = 0; i < n; i++) html += `<span class="dg-dot" style="background:${colors[k]}" data-tip="${esc(label)}: ${fmtTokens(buckets[k])}"></span>`;
    });
    for (let i = 0; i < free; i++) html += '<span class="dg-dot" data-tip="空闲"></span>';
    html += '</div>';
    return html;
  }

  function renderTurnCurveAndListHTML(detail, maxTok) {
    const turns = detail?.turns || [];
    if (!turns.length) return '';
    const W = 440;
    const H = 92;
    const padL = 6;
    const padR = 8;
    const padT = 8;
    const padB = 16;
    const maxY = Math.max(maxTok ? maxTok * 0.25 : 0, ...turns.map((t) => t.promptTokens || 0), 1000);
    const pts = turns.map((t, i) => {
      const x = padL + (i / Math.max(1, turns.length - 1)) * (W - padL - padR);
      const y = padT + (H - padT - padB) - (Math.min(maxY, t.promptTokens || 0) / maxY) * (H - padT - padB);
      return { x, y, t };
    });
    const poly = pts.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');
    const area = `${padL},${H - padB} ${poly} ${pts[pts.length - 1].x.toFixed(1)},${H - padB}`;
    const markers = pts.filter((p) => p.t.compacted).map((p) => `<line x1="${p.x.toFixed(1)}" y1="${padT}" x2="${p.x.toFixed(1)}" y2="${H - padB}" stroke="#a78bfa" stroke-dasharray="3 2" stroke-width="1"></line>`).join('');
    const dots = pts.map((p) => `<circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="2.2" fill="${p.t.compacted ? '#a78bfa' : 'var(--accent)'}"><title>Turn #${p.t.turn} · ${fmtTokens(p.t.promptTokens)}${p.t.tps ? ` · ${p.t.tps} t/s` : ''}</title></circle>`).join('');
    const curveHtml = turns.length >= 2 ? `
      <div class="dtl-section-title">上下文增长曲线（轮次 × Prompt）</div>
      <div class="curve-box">
        <svg viewBox="0 0 ${W} ${H}">
          ${markers}
          <polygon points="${area}" fill="color-mix(in srgb, var(--accent) 16%, transparent)"></polygon>
          <polyline fill="none" stroke="var(--accent)" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" points="${poly}"></polyline>
          ${dots}
          <text x="${padL}" y="${H - 3}" font-size="9.5" fill="var(--muted)">Turn ${turns[0].turn}</text>
          <text x="${W - padR}" y="${H - 3}" font-size="9.5" text-anchor="end" fill="var(--muted)">Turn ${turns[turns.length - 1].turn} · 均速 ${detail.avgTps || '--'} t/s · 峰值 ${detail.peakTps || '--'} t/s</text>
        </svg>
      </div>` : '';

    const listHtml = `
      <div class="dtl-section-title">每轮调用与速度审计（共 ${turns.length} 轮）</div>
      <div class="turn-acc-list">
        ${turns.slice().reverse().slice(0, 16).map((t) => {
          const delta = t.contextDelta > 0 ? `+${fmtTokens(t.contextDelta)}` : (t.contextDelta < 0 ? fmtTokens(t.contextDelta) : '0');
          const toolsHtml = (t.tools || []).map((tl) => `<div style="display:flex;justify-content:space-between;gap:8px;font-size:11px;color:var(--muted)"><span class="mono" style="color:var(--fg)">${esc(tl.name)}</span><span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(tl.summary || '')}</span><span class="mono">${tl.tokens ? fmtTokens(tl.tokens) : ''}</span></div>`).join('');
          return `
            <details class="turn-acc">
              <summary>
                <span class="mono" style="color:var(--muted)">#${t.turn}${t.compacted ? ' ⚡' : ''}</span>
                <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(t.userSnippet || t.modelName || '模型生成')}</span>
                <span class="mono" style="color:var(--muted)">${esc(delta)}</span>
                <span class="mono" style="color:var(--accent);font-weight:600">${t.tps > 0 ? `${t.tps.toFixed(1)} t/s` : '--'}</span>
                <span class="mono" style="color:var(--muted)">${fmtMsShort(t.ttftMs)}</span>
              </summary>
              <div class="t-body">
                <div style="display:flex;justify-content:space-between;flex-wrap:wrap;gap:6px;color:var(--muted)">
                  <span>输入/缓存：<b style="color:var(--fg)">${fmtTokens(t.inputTokens)} / ${fmtTokens(t.cacheTokens)}</b>（命中 ${Math.round(t.cacheHitPct || 0)}%）</span>
                  <span>输出(思/答)：<b style="color:var(--fg)">${fmtTokens(t.thinkingTokens)} / ${fmtTokens(t.responseTokens)}</b></span>
                  <span>TTFT/流式：<b style="color:var(--fg)">${fmtMsShort(t.ttftMs)} / ${fmtMsShort(t.streamMs)}</b></span>
                  <span>费用：<b style="color:var(--green)">${fmtMoney(t.costUsd)}</b></span>
                </div>
                ${toolsHtml}
              </div>
            </details>`;
        }).join('')}
      </div>`;
    return curveHtml + listHtml;
  }

  function showCallDetailModal(rc) {
    if (!rc) return;
    const tot = (rc.inputTokens || 0) + (rc.cacheTokens || 0) + (rc.outputTokens || 0);
    openDetailModal({
      title: '模型调用详情',
      sub: `${rc.timeStr || '--'} · ${rc.modelName || '未知模型'}`,
      tone: 'accent',
      bodyHtml: `
        <div class="dtl-grid">
          <div class="dtl-row"><span class="dtl-k">所属账号</span><span class="dtl-val">${esc(rc.account || rc.instanceId || '--')}</span></div>
          <div class="dtl-row"><span class="dtl-k">会话标题</span><span class="dtl-val">${esc(rc.title || '未命名会话')}</span></div>
          <div class="dtl-row"><span class="dtl-k">调用模型</span><span class="dtl-val">${esc(rc.modelName || '--')}</span></div>
          <div class="dtl-row"><span class="dtl-k">输入量</span><span class="dtl-val mono">${fmtTokens(rc.inputTokens)}（${(rc.inputTokens || 0).toLocaleString()}）</span></div>
          <div class="dtl-row"><span class="dtl-k">缓存命中</span><span class="dtl-val mono">${fmtTokens(rc.cacheTokens)}（${(rc.cacheTokens || 0).toLocaleString()}）</span></div>
          <div class="dtl-row"><span class="dtl-k">输出量</span><span class="dtl-val mono">${fmtTokens(rc.outputTokens)}（思 ${fmtTokens(rc.thinkingTokens)} / 答 ${fmtTokens(rc.responseTokens)}）</span></div>
          <div class="dtl-row"><span class="dtl-k">生成速度 · TTFT</span><span class="dtl-val mono">${rc.tps > 0 ? `${rc.tps.toFixed(1)} tok/s` : '--'} · 首字 ${fmtMsShort(rc.ttftMs)} · 流式 ${fmtMsShort(rc.streamMs)}</span></div>
          <div class="dtl-row"><span class="dtl-k">Prefill · 总吞吐</span><span class="dtl-val mono">${rc.prefillTps > 0 ? `${fmtTokens(rc.prefillTps)}/s` : '--'} · 总计 ${fmtTokens(tot)}</span></div>
          <div class="dtl-row"><span class="dtl-k">等效费用</span><span class="dtl-val mono" style="color:var(--green)">${fmtMoney(rc.usd)}</span></div>
        </div>
        ${rc.cascadeId ? `
          <div class="dtl-codebox">
            <div class="dtl-codebox-head">
              <span>会话标识</span>
              <button type="button" class="text" data-copy-text="${esc(rc.cascadeId)}" data-copy-label="会话标识">复制</button>
            </div>
            <code>${esc(rc.cascadeId)}</code>
          </div>` : ''}
      `,
    });
  }

  async function showContextDetailModal(entry) {
    if (!entry || !entry.context) return;
    const c = entry.context;
    const pct = Math.max(0, Math.min(100, c.usagePct || 0));
    const maxTok = Math.max(1, c.maxContextTokens || 1);
    const groups = c.tokenBreakdown || [];
    const gridHtml = render100DotGridHTML(groups, maxTok);
    const barsHtml = groups.length ? `
      <div class="dtl-section-title">上下文构成明细</div>
      <div class="dtl-bars">
        ${groups.map((g) => {
          const gPct = Math.min(100, ((g.numTokens || 0) / maxTok) * 100);
          const chTip = (g.children || []).slice(0, 8).map((ch) => `${localizeContextGroup(ch.name)}: ${fmtTokens(ch.numTokens)}`).join(' · ');
          return `
            <div${chTip ? ` data-tip="${esc(chTip)}"` : ''}>
              <div class="quota-labels">
                <span class="quota-name" style="color:var(--fg);font-weight:500">${esc(localizeContextGroup(g.name))}</span>
                <span class="quota-n">${fmtTokens(g.numTokens)}（${gPct.toFixed(1)}%）</span>
              </div>
              <div class="quota-track"><i style="width:${Math.max(1, gPct)}%"></i></div>
            </div>`;
        }).join('')}
      </div>` : '';

    openDetailModal({
      title: '上下文 /context 与遥测详情',
      sub: `${entry.account || entry.instanceId} · 占用 ${pct.toFixed(1)}%`,
      tone: pct >= 80 ? 'warn' : 'accent',
      bodyHtml: `
        <div class="dtl-grid">
          <div class="dtl-row"><span class="dtl-k">会话标题</span><span class="dtl-val">${esc(c.title || '当前活跃会话')}</span></div>
          <div class="dtl-row"><span class="dtl-k">当前模型</span><span class="dtl-val">${esc(c.model || '默认模型')}</span></div>
          <div class="dtl-row"><span class="dtl-k">容量水位</span><span class="dtl-val mono">${fmtTokens(c.estimatedTokensUsed)} / ${fmtTokens(c.maxContextTokens)}（${pct.toFixed(1)}%）</span></div>
          <div class="dtl-row"><span class="dtl-k">对话步数</span><span class="dtl-val">${c.stepCount || 0} 步</span></div>
          ${c.workspace ? `<div class="dtl-row"><span class="dtl-k">工作区目录</span><span class="dtl-val mono">${esc(c.workspace)}</span></div>` : ''}
        </div>
        ${gridHtml}
        ${barsHtml}
        <div id="dtl-turns-slot"><div style="font-size:11.5px;color:var(--muted);padding:4px 0">加载中…</div></div>
        ${c.cascadeId ? `
          <div class="dtl-codebox">
            <div class="dtl-codebox-head">
              <span>会话标识</span>
              <button type="button" class="text" data-copy-text="${esc(c.cascadeId)}" data-copy-label="会话标识">复制</button>
            </div>
            <code>${esc(c.cascadeId)}</code>
          </div>` : ''}
      `,
    });

    if (c.cascadeId) {
      try {
        const res = await api('/api/conversation?' + new URLSearchParams({ cascadeId: c.cascadeId, instance: entry.instanceId || '' }).toString());
        const slot = $('dtl-turns-slot');
        if (slot && res?.ok) {
          slot.innerHTML = renderTurnCurveAndListHTML(res.detail, res.context?.maxContextTokens || maxTok) || '<div style="font-size:11px;color:var(--muted)">暂无多轮记录</div>';
        }
      } catch {
        const slot = $('dtl-turns-slot');
        if (slot) slot.innerHTML = '';
      }
    }
  }

  async function showSessionHitDetailModal(h) {
    if (!h) return;
    openDetailModal({
      title: '历史会话详情',
      sub: h.account || h.instanceId || '',
      tone: 'accent',
      bodyHtml: `
        <div class="dtl-grid">
          <div class="dtl-row"><span class="dtl-k">会话标题</span><span class="dtl-val">${esc(h.title || '未命名会话')}</span></div>
          <div class="dtl-row"><span class="dtl-k">所属账号</span><span class="dtl-val">${esc(h.account || h.instanceId || '--')}</span></div>
          <div class="dtl-row"><span class="dtl-k">对话步数</span><span class="dtl-val">${h.stepCount || 0} 步对话</span></div>
          ${h.modifiedAt ? `<div class="dtl-row"><span class="dtl-k">最后活跃</span><span class="dtl-val mono">${esc(h.modifiedAt)}</span></div>` : ''}
          ${h.workspace ? `<div class="dtl-row"><span class="dtl-k">工作区路径</span><span class="dtl-val mono">${esc(h.workspace)}</span></div>` : ''}
        </div>
        ${h.snippet ? `
          <div class="dtl-codebox">
            <div class="dtl-codebox-head"><span>匹配内容摘要</span></div>
            <code>${esc(h.snippet)}</code>
          </div>` : ''}
        <div id="dtl-hit-turns-slot"><div style="font-size:11.5px;color:var(--muted);padding:4px 0">加载中…</div></div>
        ${h.cascadeId ? `
          <div class="dtl-codebox">
            <div class="dtl-codebox-head">
              <span>会话标识</span>
              <button type="button" class="text" data-copy-text="${esc(h.cascadeId)}" data-copy-label="会话标识">复制</button>
            </div>
            <code>${esc(h.cascadeId)}</code>
          </div>` : ''}
      `,
    });

    if (h.cascadeId) {
      try {
        const res = await api('/api/conversation?' + new URLSearchParams({ cascadeId: h.cascadeId, instance: h.instanceId || '' }).toString());
        const slot = $('dtl-hit-turns-slot');
        if (slot && res?.ok) {
          const c = res.context;
          const grid = c ? render100DotGridHTML(c.tokenBreakdown, c.maxContextTokens) : '';
          const turnHtml = renderTurnCurveAndListHTML(res.detail, c?.maxContextTokens || 256000);
          slot.innerHTML = grid + turnHtml;
        }
      } catch {
        const slot = $('dtl-hit-turns-slot');
        if (slot) slot.innerHTML = '';
      }
    }
  }

  function showCDPTargetDetailModal(t) {
    if (!t) return;
    const hasErr = !!t.lastError;
    openDetailModal({
      title: '窗口注入诊断详情',
      sub: `实例 ${t.instanceId} · 调试端口 ${t.port}`,
      tone: hasErr ? 'danger' : 'ok',
      bodyHtml: `
        <div class="dtl-grid">
          <div class="dtl-row"><span class="dtl-k">窗口标题</span><span class="dtl-val">${esc(t.title || '客户端窗口')}</span></div>
          <div class="dtl-row"><span class="dtl-k">所属实例</span><span class="dtl-val mono">${esc(t.instanceId)}</span></div>
          <div class="dtl-row"><span class="dtl-k">远程端口</span><span class="dtl-val mono">${t.port}</span></div>
          <div class="dtl-row"><span class="dtl-k">监控浮窗</span><span class="dtl-val">${t.hudActive ? '已挂载就绪' : '未挂载'}</span></div>
          <div class="dtl-row"><span class="dtl-k">中文汉化</span><span class="dtl-val">${t.zhInjected ? '已成功注入' : '未注入'}</span></div>
        </div>
        ${hasErr ? `
          <div class="dtl-codebox" style="border-color:color-mix(in srgb, var(--red) 35%, var(--line))">
            <div class="dtl-codebox-head">
              <span style="color:var(--red)">最近异常信息</span>
              <button type="button" class="text" data-copy-text="${esc(t.lastError)}" data-copy-label="异常信息">复制</button>
            </div>
            <code style="color:var(--red)">${esc(t.lastError)}</code>
          </div>` : ''}
        ${t.url ? `
          <div class="dtl-codebox">
            <div class="dtl-codebox-head">
              <span>宿主页面地址</span>
              <button type="button" class="text" data-copy-text="${esc(t.url)}" data-copy-label="页面地址">复制</button>
            </div>
            <code>${esc(t.url)}</code>
          </div>` : ''}
      `,
      actions: [
        {
          label: '立即重新注入',
          cls: 'primary',
          closeOnDone: true,
          onClick: async () => {
            const res = await syncCDPNow();
            if (state.overview) state.overview.cdpTargets = res.targets;
            renderDoctor();
            showStatus(`窗口浮窗已同步（共 ${(res.targets || []).length} 个窗口）`, 'ok');
          },
        },
      ],
    });
  }

  async function syncCDPNow() {
    try {
      return await api('/api/cdp/sync', { method: 'POST', body: '{}' });
    } catch {
      return api('/api/cdp/inject-now', { method: 'POST', body: '{}' });
    }
  }

  // ---- 新建隔离实例弹窗打开 / 关闭 ----
  function openCreateModal() {
    $('cp-error').hidden = true;
    syncAllSelectLabels();
    openModal($('create-modal'), $('cp-name'));
  }
  function closeCreateModal() {
    closeModal($('create-modal'));
    $('cp-error').hidden = true;
  }

  // ---- 全局交互事件绑定 ----
  function wireEvents() {
    document.querySelectorAll('#main-nav .opt').forEach((btn) => {
      btn.addEventListener('click', () => switchView(btn.dataset.view));
    });

    document.querySelectorAll('#cur-seg .opt[data-currency]').forEach((btn) => {
      btn.addEventListener('click', () => {
        state.currency = btn.dataset.currency;
        state.curOverridden = true;
        syncCurrencyButtons();
        renderOverview();
        renderTelemetry();
      });
    });

    $('btn-refresh').addEventListener('click', () => loadOverview(true));
    $('btn-cdp-now').addEventListener('click', async () => {
      const btn = $('btn-cdp-now');
      btn.disabled = true;
      try {
        const res = await syncCDPNow();
        showStatus(`窗口浮窗已同步（共 ${(res.targets || []).length} 个窗口）`, 'ok');
        loadOverview(false);
      } catch (e) {
        showStatus('同步浮窗失败：' + humanizeError(e), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    // 模态框关闭与确认绑定
    $('btn-open-create-top').addEventListener('click', openCreateModal);
    $('btn-open-create').addEventListener('click', openCreateModal);
    $('cp-close-btn').addEventListener('click', closeCreateModal);
    $('cp-cancel-btn').addEventListener('click', closeCreateModal);

    $('cfm-close-btn').addEventListener('click', () => closeModal($('confirm-modal')));
    $('cfm-cancel-btn').addEventListener('click', () => closeModal($('confirm-modal')));
    $('cfm-ok-btn').addEventListener('click', () => {
      if (activeConfirmAction) activeConfirmAction();
    });

    $('dtl-close-btn').addEventListener('click', () => closeModal($('detail-modal')));

    // 点击遮罩关闭模态框
    ['create-modal', 'confirm-modal', 'detail-modal'].forEach((mid) => {
      const mEl = $(mid);
      if (!mEl) return;
      mEl.addEventListener('click', (e) => {
        if (e.target === mEl && !modalBusy) closeModal(mEl);
      });
    });

    // 滚动或调整窗口大小时自动收起悬浮下拉菜单与提示，避免位置脱节
    $('view-scroll').addEventListener('scroll', () => {
      if (!$('pop-host').hidden) closePop();
      hideTip(true);
    }, { passive: true });
    window.addEventListener('resize', () => {
      if (!$('pop-host').hidden) closePop();
      hideTip(true);
      syncAllSegs(true);
    });

    // 全局悬停 / 键盘焦点提示委托
    document.addEventListener('mouseover', (e) => {
      const target = e.target.closest?.('[data-tip]');
      if (target && target.dataset.tip && !(target.tagName === 'SUMMARY' && target.parentElement?.open)) {
        if (target !== activeTipTarget) showTip(target);
      } else if (activeTipTarget) {
        hideTip(false);
      }
    });
    document.addEventListener('mouseout', (e) => {
      if (!activeTipTarget) return;
      const next = e.relatedTarget?.closest?.('[data-tip]');
      if (!next) hideTip(false);
    });
    document.addEventListener('focusin', (e) => {
      const help = e.target.closest?.('button.help-dot[data-tip]');
      if (help) showTip(help, true);
    });
    document.addEventListener('focusout', (e) => {
      if (e.target.closest?.('button.help-dot[data-tip]')) hideTip(true);
    });

    // 全局点击事件委托
    document.body.addEventListener('click', async (e) => {
      const helpBtn = e.target.closest('button.help-dot[data-tip]');
      if (helpBtn) {
        e.preventDefault();
        e.stopPropagation();
        if (activeTipTarget === helpBtn && !$('tip-host').hidden) {
          hideTip(true);
        } else {
          showTip(helpBtn, true);
        }
        return;
      }

      if (activeTipTarget && !e.target.closest('[data-tip]')) {
        hideTip(true);
      }

      const copyBtn = e.target.closest('button[data-copy-text]');
      if (copyBtn) {
        e.stopPropagation();
        const ok = await copyText(copyBtn.dataset.copyText, copyBtn.dataset.copyLabel || '内容');
        if (ok) {
          const orig = copyBtn.textContent;
          copyBtn.textContent = '已复制';
          setTimeout(() => { if (copyBtn.isConnected) copyBtn.textContent = orig; }, 1600);
        }
        return;
      }

      if (e.target.closest('button[data-open-create]')) {
        openCreateModal();
        return;
      }

      const sw = e.target.closest('button[role="switch"]');
      if (sw) {
        const next = sw.getAttribute('aria-checked') !== 'true';
        sw.setAttribute('aria-checked', next ? 'true' : 'false');
        return;
      }

      const fieldBtn = e.target.closest('button.field[data-select]');
      if (fieldBtn) {
        e.stopPropagation();
        openPop(fieldBtn);
        return;
      }

      const bindMenuBtn = e.target.closest('button[data-colab-act="bind-menu"]');
      if (bindMenuBtn) {
        e.stopPropagation();
        openColabBindPop(bindMenuBtn);
        return;
      }

      if (!e.target.closest('#pop-host')) {
        closePop();
      }

      // 详情点击委托（账单调用行、上下文卡片、会话命中行、CDP 目标行）
      const callRow = e.target.closest('tr[data-call-idx]');
      if (callRow && !e.target.closest('button')) {
        const rc = state.telemetry?.report?.recentCalls?.[Number(callRow.dataset.callIdx)];
        showCallDetailModal(rc);
        return;
      }

      const ctxCard = e.target.closest('[data-ctx-idx]');
      if (ctxCard && !e.target.closest('button')) {
        const entry = state.sessions?.contexts?.[Number(ctxCard.dataset.ctxIdx)];
        showContextDetailModal(entry);
        return;
      }

      const hitRow = e.target.closest('[data-hit-idx]');
      if (hitRow && !e.target.closest('button')) {
        const h = state.sessions?.hits?.[Number(hitRow.dataset.hitIdx)];
        showSessionHitDetailModal(h);
        return;
      }

      const cdpTrigger = e.target.closest('[data-cdp-idx]');
      if (cdpTrigger) {
        const t = state.overview?.cdpTargets?.[Number(cdpTrigger.dataset.cdpIdx)];
        showCDPTargetDetailModal(t);
        return;
      }

      // Colab 云算力操作
      const colabBtn = e.target.closest('button[data-colab-act]');
      if (colabBtn) {
        const cact = colabBtn.dataset.colabAct;
        const email = colabBtn.dataset.email || '';
        if (cact === 'relogin') {
          colabBtn.disabled = true;
          try {
            await triggerColabLogin(email);
          } finally {
            colabBtn.disabled = false;
          }
          return;
        }
        if (cact === 'refresh') {
          colabBtn.disabled = true;
          try {
            const snap = await api('/api/colab?refresh=1');
            if (state.overview) {
              state.overview.colab = snap;
              state.overview.colabAccounts = snap.accounts || [];
              renderOverview();
            } else {
              await loadOverview(false);
            }
            showStatus('云算力状态已更新', 'ok');
          } catch (err) {
            showStatus('巡检失败：' + humanizeError(err), 'err');
          } finally {
            colabBtn.disabled = false;
          }
          return;
        }
        if (cact === 'nuke') {
          const activeCnt = Number(colabBtn.dataset.active || 0);
          const orphanCnt = Number(colabBtn.dataset.orphan || 0);
          const isAll = !email;
          openConfirmModal({
            title: isAll ? '确认执行全池清场' : '确认终止云端算力实例',
            sub: isAll ? '将作用于所有已绑定的云算力账号' : `目标账号：${email}`,
            desc: isAll
              ? '此操作将强制释放全部已绑定账号下正在运行或孤儿残留的云端虚拟机实例，虚拟机内存中未持久化保存的数据将会丢失。'
              : '此操作将立即终止并释放该账号下所有运行中与孤儿残留的云端虚拟机实例，防止继续消耗算力点。',
            tone: 'danger',
            danger: true,
            okLabel: isAll ? '全部清场' : '确认清场',
            busyLabel: '正在清场…',
            meta: [
              { k: '作用范围', v: isAll ? '全部已绑定账号' : email },
              { k: '运行中实例', v: `${activeCnt} 台`, cls: activeCnt > 0 ? 'warn' : '' },
              { k: '孤儿残留实例', v: `${orphanCnt} 台`, cls: orphanCnt > 0 ? 'danger' : '' },
            ],
            onConfirm: async () => {
              const res = await api('/api/colab/nuke', { method: 'POST', body: JSON.stringify({ account: email }) });
              if (state.overview && res.colab) {
                state.overview.colab = res.colab;
                state.overview.colabAccounts = res.colab.accounts || [];
                renderOverview();
              } else {
                await loadOverview(false);
              }
              showStatus(`清场完成 · 已终止 ${res.nuke?.terminatedCount || 0} 台云端虚拟机`, 'ok');
            },
          });
          return;
        }
        if (cact === 'remove') {
          const isKeychain = colabBtn.dataset.source === 'colab-safe';
          openConfirmModal({
            title: isKeychain ? '隐藏云算力账号卡片' : '解绑云算力账号',
            sub: email,
            desc: isKeychain
              ? '该账号来自本地系统钥匙串，确认后将从总览面板隐藏其云算力状态卡片。'
              : '解绑后将从本地安全移除该账号的网页授权凭据，后续如需监控可随时重新绑定。',
            tone: isKeychain ? 'warn' : 'danger',
            danger: !isKeychain,
            okLabel: isKeychain ? '确认隐藏' : '确认解绑',
            busyLabel: '处理中…',
            meta: [
              { k: '账号邮箱', v: email },
              { k: '凭据来源', v: isKeychain ? '系统钥匙串' : '网页授权' },
            ],
            onConfirm: async () => {
              const res = await api('/api/colab/accounts/remove', { method: 'POST', body: JSON.stringify({ email }) });
              if (state.overview && res.colab) {
                state.overview.colab = res.colab;
                state.overview.colabAccounts = res.colab.accounts || [];
                renderOverview();
              } else {
                await loadOverview(false);
              }
              showStatus(`${res.removed ? '已解绑账号' : '已隐藏卡片'} ${email}`, 'ok');
            },
          });
          return;
        }
      }

      // 客户端实例生命周期操作
      const btn = e.target.closest('button[data-act]');
      if (!btn) return;
      const act = btn.dataset.act;
      const id = btn.dataset.id;
      const card = btn.closest('.subscription-card');
      const instObj = (state.overview?.instances || []).find((it) => it.id === id);
      const instLabel = instObj?.label ? `${instObj.label} (${id})` : id;

      if (act === 'stop') {
        const hasBusy = instObj && (instObj.liveState === 'running' || instObj.liveState === 'waiting_approval');
        openConfirmModal({
          title: '确认停止客户端实例',
          sub: instLabel,
          desc: hasBusy
            ? '检测到该客户端实例当前仍有正在执行或等待确认的对话任务！停止实例将直接关闭其客户端窗口，请确认是否继续。'
            : '确认关闭该隔离客户端实例窗口吗？关闭后可随时在总览页重新启动。',
          tone: hasBusy ? 'danger' : 'warn',
          danger: true,
          okLabel: '停止实例',
          busyLabel: '正在停止…',
          meta: [
            { k: '目标实例', v: instLabel },
            { k: '当前状态', v: hasBusy ? '有活跃任务运行中' : '在线空闲', cls: hasBusy ? 'danger' : '' },
          ],
          onConfirm: async () => {
            await api(`/api/instances/${encodeURIComponent(id)}/stop`, { method: 'POST', body: '{}' });
            showStatus(`已停止实例 ${id}`, 'ok');
            flashEl(card);
            setTimeout(() => loadOverview(true), 600);
          },
        });
        return;
      }

      if (act === 'delete') {
        openConfirmModal({
          title: '确认删除多开隔离实例',
          sub: instLabel,
          desc: '删除后将移除该隔离实例的配置注册与目录档案，此操作不可撤销。若仅需临时关闭窗口，请使用「停止」功能。',
          tone: 'danger',
          danger: true,
          okLabel: '确认删除',
          busyLabel: '正在删除…',
          meta: [
            { k: '实例标识', v: id },
            ...(instObj?.account ? [{ k: '关联账号', v: instObj.account }] : []),
          ],
          onConfirm: async () => {
            await api(`/api/instances/${encodeURIComponent(id)}/delete`, { method: 'POST', body: '{}' });
            showStatus(`已删除隔离实例 ${id}`, 'ok');
            setTimeout(() => loadOverview(true), 300);
          },
        });
        return;
      }

      btn.disabled = true;
      try {
        if (act === 'focus') {
          await api(`/api/instances/${encodeURIComponent(id)}/focus`, { method: 'POST', body: '{}' });
          showStatus(`已将窗口置于前台：${id}`, 'ok');
        } else if (act === 'launch') {
          const res = await api(`/api/instances/${encodeURIComponent(id)}/launch`, { method: 'POST', body: '{}' });
          if (res?.oauthHint) {
            showStatus(res.oauthHint, 'ok', 6000);
          } else {
            showStatus(`正在启动实例：${id}`, 'ok');
          }
          flashEl(card);
          setTimeout(() => loadOverview(true), 1200);
        } else if (act === 'heal') {
          await api(`/api/instances/${encodeURIComponent(id)}/heal`, { method: 'POST', body: '{}' });
          showStatus(`实例 ${id} 自愈修复完成`, 'ok');
          flashEl(card);
          setTimeout(() => loadOverview(true), 400);
        }
      } catch (err) {
        showStatus('操作失败：' + humanizeError(err), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    // 全局键盘导航：下拉菜单方向键、Escape 关闭、模态框焦点陷阱（Focus Trap）
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && $('tip-host') && !$('tip-host').hidden) {
        hideTip(true);
      }
      const popHost = $('pop-host');
      if (popHost && !popHost.hidden && activePopOptions.length > 0) {
        if (e.key === 'Escape') {
          e.preventDefault();
          closePop(true);
          return;
        }
        if (e.key === 'ArrowDown') {
          e.preventDefault();
          activePopFocusIdx = (activePopFocusIdx + 1) % activePopOptions.length;
          updatePopFocusVisual();
          return;
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault();
          activePopFocusIdx = (activePopFocusIdx - 1 + activePopOptions.length) % activePopOptions.length;
          updatePopFocusVisual();
          return;
        }
        if (e.key === 'Home') {
          e.preventDefault();
          activePopFocusIdx = 0;
          updatePopFocusVisual();
          return;
        }
        if (e.key === 'End') {
          e.preventDefault();
          activePopFocusIdx = activePopOptions.length - 1;
          updatePopFocusVisual();
          return;
        }
        if (e.key === 'Enter' || e.key === ' ') {
          if (activePopFocusIdx >= 0 && activePopFocusIdx < activePopOptions.length) {
            e.preventDefault();
            const picked = activePopOptions[activePopFocusIdx];
            const cb = activePopOnPick;
            closePop(true);
            if (picked && cb) cb(picked);
            return;
          }
        }
      }

      if (modalStack.length > 0) {
        const topModal = modalStack[modalStack.length - 1].el;
        if (e.key === 'Escape') {
          e.preventDefault();
          if (!modalBusy) closeModal(topModal);
          return;
        }
        if (e.key === 'Tab') {
          const focusables = getFocusableElements(topModal);
          if (focusables.length === 0) {
            e.preventDefault();
            return;
          }
          const first = focusables[0];
          const last = focusables[focusables.length - 1];
          if (e.shiftKey && document.activeElement === first) {
            e.preventDefault();
            last.focus();
          } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    });

    // 账单区间切换与 CSV 导出
    $('telemetry-presets').addEventListener('click', (e) => {
      const b = e.target.closest('button[data-preset]');
      if (!b) return;
      state.preset = b.dataset.preset;
      document.querySelectorAll('#telemetry-presets .opt').forEach((t) => t.classList.toggle('on', t === b));
      syncSegThumb($('telemetry-presets'), false);
      loadTelemetry();
    });

    $('btn-export-csv').addEventListener('click', async () => {
      const btn = $('btn-export-csv');
      btn.disabled = true;
      try {
        const res = await api('/api/telemetry/save-csv', {
          method: 'POST',
          body: JSON.stringify({ preset: state.preset, mode: state.mode, instance: state.telInst }),
        });
        showStatus(`账单已导出至 ${res.path}`, 'ok', 4200);
      } catch (e) {
        showStatus('导出账单失败：' + humanizeError(e), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    // 会话检索提交
    $('session-search-form').addEventListener('submit', (e) => {
      e.preventDefault();
      loadSessions();
    });

    // 体检页操作
    $('btn-doctor-fix').addEventListener('click', async () => {
      const btn = $('btn-doctor-fix');
      btn.disabled = true;
      showStatus('正在执行安全修复…', 'busy');
      try {
        const res = await api('/api/doctor/fix', { method: 'POST', body: '{}' });
        state.doctor = res.doctor;
        renderDoctor();
        const fixedList = res.fix?.fixed || [];
        showStatus(fixedList.length ? `修复完成：${fixedList.join('、')}` : '环境检查无异常，无需变更', 'ok');
      } catch (e) {
        showStatus('修复失败：' + humanizeError(e), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    $('btn-doctor-cdp').addEventListener('click', async () => {
      const btn = $('btn-doctor-cdp');
      btn.disabled = true;
      try {
        const res = await syncCDPNow();
        if (state.overview) state.overview.cdpTargets = res.targets;
        renderDoctor();
        showStatus(`窗口浮窗重注完成（共 ${(res.targets || []).length} 个窗口）`, 'ok');
      } catch (e) {
        showStatus('重注浮窗失败：' + humanizeError(e), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    // 本地缓存扫描与中文确认清理弹窗
    $('btn-cache-scan').addEventListener('click', async () => {
      await loadCache();
      showStatus('缓存目录扫描完成', 'ok');
    });

    $('btn-cache-clean').addEventListener('click', async () => {
      const keepCount = Math.max(1, Number($('cache-keep-input').value || 20));
      if (!state.cache || state.cache.keepCount !== keepCount) {
        await loadCache();
      }
      const sum = state.cache;
      const filtered = getFilteredCacheItems(sum?.items || []);
      const reclaimable = filtered.filter((it) => !it.protected);
      if (reclaimable.length === 0) {
        showStatus('当前没有可清理的历史缓存，所有会话均在受保护保留范围内', 'ok', 3600);
        return;
      }
      const estBytes = reclaimable.reduce((acc, it) => acc + Number(it.totalBytes || 0), 0);
      const scopeOpt = SELECT_DEFS['cache-inst-filter'].options.find((o) => o.value === state.cacheInst);
      const scopeLabel = (scopeOpt?.label || '全部账号').trim();

      openConfirmModal({
        title: '确认清理历史会话缓存',
        sub: `清理范围：${scopeLabel}`,
        desc: '系统将自动保留每个客户端实例最近活跃的受保护会话与正在运行的会话，仅移除早期闲置历史缓存文件。清理后对应的旧会话记录将不可恢复。',
        tone: 'danger',
        danger: true,
        okLabel: '立即清理',
        busyLabel: '正在清理…',
        meta: [
          { k: '目标范围', v: scopeLabel },
          { k: '单实例保留策略', v: `最近 ${keepCount} 条会话` },
          { k: '预计清理条目', v: `${reclaimable.length} 条`, cls: 'warn' },
          { k: '预计释放空间', v: fmtBytes(estBytes), cls: 'danger' },
        ],
        onConfirm: async () => {
          const res = await api('/api/cache/clean', {
            method: 'POST',
            body: JSON.stringify({ keepCount, instance: state.cacheInst }),
          });
          state.cache = res.scan;
          renderCache();
          showStatus(`清理完成 · 已移除 ${res.cleaned?.deletedCount || 0} 条缓存，释放 ${fmtBytes(res.cleaned?.freedBytes || 0)}`, 'ok', 4000);
        },
      });
    });

    // 系统设置页测试推送与保存
    $('btn-tg-test').addEventListener('click', async () => {
      const btn = $('btn-tg-test');
      btn.disabled = true;
      try {
        await api('/api/notify/test', {
          method: 'POST',
          body: JSON.stringify({
            channel: 'telegram',
            botToken: $('cfg-tg-token').value.trim(),
            chatId: $('cfg-tg-chatid').value.trim(),
          }),
        });
        showStatus('测试通知已发送，请检查接收会话', 'ok');
      } catch (e) {
        showStatus('发送测试通知失败：' + humanizeError(e), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    $('btn-wh-test')?.addEventListener('click', async () => {
      const btn = $('btn-wh-test');
      btn.disabled = true;
      try {
        await api('/api/notify/test', {
          method: 'POST',
          body: JSON.stringify({
            channel: 'webhook',
            webhookUrl: $('cfg-wh-url').value.trim(),
            webhookFormat: state.cfgWhFormat || 'auto',
            webhookSecret: $('cfg-wh-secret').value.trim(),
          }),
        });
        showStatus('Webhook 测试推送已发送', 'ok');
      } catch (e) {
        showStatus('Webhook 测试失败：' + humanizeError(e), 'err');
      } finally {
        btn.disabled = false;
      }
    });

    $('btn-cfg-reset')?.addEventListener('click', () => loadConfig(true));

    $('settings-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const saveBtn = $('btn-cfg-save');
      setBtnBusy(saveBtn, true, '保存中…');
      const base = state.config || {};
      const next = {
        ...base,
        language: state.cfgLang || 'auto',
        contextWarnPct: Number($('cfg-warn-ctx').value || 80),
        quotaWarnPct: Number($('cfg-warn-quota').value || 20),
        cdp: {
          enabled: getSwitch('cfg-cdp-enabled'),
          zhInject: getSwitch('cfg-cdp-zh'),
          windowHud: getSwitch('cfg-cdp-hud'),
          hudPosition: state.cdpPos || 'top-right',
          zhScriptPath: $('cfg-cdp-path').value.trim(),
          hudInstances: $('cfg-cdp-insts').value.split(',').map((s) => s.trim()).filter(Boolean),
        },
        telegram: {
          enabled: getSwitch('cfg-tg-enabled'),
          botToken: $('cfg-tg-token').value.trim(),
          chatId: $('cfg-tg-chatid').value.trim(),
          notifyOnStop: getSwitch('cfg-tg-stop'),
          notifyOnError: getSwitch('cfg-tg-err'),
          notifyMinDurationSec: Number($('cfg-tg-mindur').value || 60),
          notifyOnStuckSec: Number($('cfg-tg-stuck').value || 45),
          notifyLowQuota: getSwitch('cfg-tg-lowq'),
        },
        webhook: {
          enabled: getSwitch('cfg-wh-enabled'),
          url: $('cfg-wh-url')?.value.trim() || '',
          format: state.cfgWhFormat || 'auto',
          secret: $('cfg-wh-secret')?.value.trim() || '',
        },
        pricing: {
          mode: state.cfgPriceMode,
          currency: state.cfgPriceCur,
          usdToCny: Number($('cfg-price-rate').value || 7.20),
          ultraMonthlyUsd: $('cfg-price-sub').value.trim() === '' ? 0 : Math.max(0, Number($('cfg-price-sub').value)),
        },
        paths: base.paths || {},
        colab: {
          enabled: getSwitch('cfg-colab-enabled'),
          cliPath: $('cfg-colab-cli').value.trim() || '~/.local/bin/colab-safe',
          pollIntervalSec: Number($('cfg-colab-poll').value || 60),
          monthlyAllowance: Number($('cfg-colab-allowance').value || 2000),
          warnActiveMinutes: Number($('cfg-colab-warnmin').value || 30),
          lowBalanceCcu: Number($('cfg-colab-lowccu').value ?? 200),
        },
      };
      try {
        state.config = await api('/api/config', { method: 'PUT', body: JSON.stringify(next) });
        if (window.AgyI18n) {
          window.AgyI18n.setPreference(next.language || 'auto', { persist: true });
        }
        showStatus('系统设置已保存生效', 'ok');
        document.querySelectorAll('#view-settings .list').forEach(flashEl);
        loadOverview(false);
      } catch (err) {
        showStatus('保存设置失败：' + humanizeError(err), 'err');
      } finally {
        setBtnBusy(saveBtn, false);
      }
    });

    // 新建隔离实例模态框提交（带防重复提交与内联中文错误反馈）
    $('create-profile-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      if (modalBusy) return;
      const errBox = $('cp-error');
      const submitBtn = $('cp-submit-btn');
      const cancelBtn = $('cp-cancel-btn');
      const closeBtn = $('cp-close-btn');
      errBox.hidden = true;

      const nameVal = $('cp-name').value.trim();
      if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(nameVal)) {
        errBox.textContent = '实例标识须以字母或数字开头，仅限字母、数字、点、下划线与连字符（最长 64 位）';
        errBox.hidden = false;
        $('cp-name').focus();
        return;
      }

      const body = {
        name: nameVal,
        description: $('cp-desc').value.trim(),
        linksPolicy: state.cpLinks,
        inheritFrom: state.cpInherit,
        inheritMcp: getSwitch('cp-mcp'),
        launch: getSwitch('cp-launch'),
      };

      modalBusy = true;
      cancelBtn.disabled = true;
      closeBtn.disabled = true;
      setBtnBusy(submitBtn, true, '正在创建…');
      try {
        await api('/api/profiles/create', { method: 'POST', body: JSON.stringify(body) });
        modalBusy = false;
        cancelBtn.disabled = false;
        closeBtn.disabled = false;
        setBtnBusy(submitBtn, false);
        closeCreateModal();
        $('create-profile-form').reset();
        setSwitch('cp-mcp', true);
        setSwitch('cp-launch', false);
        state.cpLinks = 'minimal';
        state.cpInherit = 'host';
        syncAllSelectLabels();
        showStatus(`隔离实例「${body.name}」已创建${body.launch ? '并正在启动' : ''}`, 'ok');
        loadOverview(true);
      } catch (err) {
        modalBusy = false;
        cancelBtn.disabled = false;
        closeBtn.disabled = false;
        setBtnBusy(submitBtn, false);
        errBox.textContent = humanizeError(err);
        errBox.hidden = false;
      }
    });

    if (typeof ResizeObserver !== 'undefined') {
      const ro = new ResizeObserver(() => syncAllSegs(true));
      document.querySelectorAll('.segs').forEach((s) => ro.observe(s));
    }
  }

  function connectSSE() {
    const pill = $('sse-pill');
    const es = new EventSource('/api/events');
    es.addEventListener('connected', () => {
      pill.className = 'sse-dot-btn ok';
      syncSSEPillTooltip();
    });
    es.addEventListener('update', () => {
      loadOverview(false);
      if (state.view === 'sessions') loadSessions();
    });
    es.onerror = () => {
      pill.className = 'sse-dot-btn err';
      syncSSEPillTooltip();
    };
  }

  initThemeAndBridge();
  syncAllSelectLabels();
  wireEvents();
  requestAnimationFrame(() => syncAllSegs(true));
  connectSSE();
  loadOverview(false);
  if (qs.get('mode') === 'sidecar' && qs.get('conversationId')) {
    switchView('sessions');
  }
})();
