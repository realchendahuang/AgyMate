(() => {
  const HOST_ID = 'agymate-hud-host';
  const POS_KEY = 'agymate_hud_pos_v1';
  const COLLAPSE_KEY = 'agymate_hud_collapsed_v1';

  const CSS = `
    :host { all: initial; }
    .wrap {
      font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", system-ui, sans-serif;
      font-size: 11px;
      line-height: 1.3;
      color: #ededf1;
      user-select: none;
      -webkit-user-select: none;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 7px;
      height: 28px;
      padding: 0 10px 0 9px;
      border-radius: 999px;
      background: rgba(24, 24, 29, 0.88);
      backdrop-filter: blur(14px);
      -webkit-backdrop-filter: blur(14px);
      border: 1px solid rgba(255, 255, 255, 0.14);
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.38);
      cursor: grab;
      white-space: nowrap;
      transition: border-color 0.15s, background 0.15s;
    }
    .pill:hover {
      border-color: rgba(165, 180, 252, 0.45);
      background: rgba(28, 28, 34, 0.95);
    }
    .pill.dragging { cursor: grabbing; }
    .dot {
      width: 7px;
      height: 7px;
      border-radius: 50%;
      flex: none;
      background: #8b8b94;
    }
    .dot.running {
      background: #6fd99b;
      box-shadow: 0 0 0 3px rgba(111, 217, 155, 0.22);
    }
    .dot.waiting {
      background: #f2b544;
      box-shadow: 0 0 0 3px rgba(242, 181, 68, 0.25);
    }
    .badge {
      font-family: ui-monospace, "SF Mono", Menlo, monospace;
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.03em;
      padding: 1px 5px;
      border-radius: 4px;
      background: rgba(165, 180, 252, 0.18);
      color: #c7d2fe;
    }
    .sep { color: rgba(255, 255, 255, 0.22); }
    .metric {
      font-family: ui-monospace, "SF Mono", Menlo, monospace;
      font-variant-numeric: tabular-nums;
      color: #d4d4dc;
    }
    .metric.warn { color: #f2b544; font-weight: 600; }
    .metric.danger { color: #f28b82; font-weight: 600; }
    .btn {
      all: unset;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 16px;
      height: 16px;
      border-radius: 50%;
      color: #9e9ea8;
      cursor: pointer;
      font-size: 12px;
      line-height: 1;
    }
    .btn:hover { background: rgba(255, 255, 255, 0.12); color: #fff; }
    .mini {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 28px;
      height: 28px;
      border-radius: 50%;
      background: rgba(24, 24, 29, 0.88);
      backdrop-filter: blur(12px);
      border: 1px solid rgba(255, 255, 255, 0.16);
      box-shadow: 0 6px 18px rgba(0, 0, 0, 0.35);
      cursor: pointer;
      font-size: 13px;
      color: #a5b4fc;
    }
    .pop {
      position: absolute;
      bottom: 36px;
      right: 0;
      width: 276px;
      padding: 11px 12px;
      border-radius: 10px;
      background: rgba(28, 28, 34, 0.96);
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
      border: 1px solid rgba(255, 255, 255, 0.14);
      box-shadow: 0 16px 40px rgba(0, 0, 0, 0.55);
      display: none;
      color: #ededf1;
    }
    .wrap:hover .pop.openable { display: block; }
    .pop.top-pos { bottom: auto; top: 36px; }
    .pop-head {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 8px;
      padding-bottom: 6px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.08);
      font-weight: 600;
      font-size: 11.5px;
    }
    .pop-link {
      color: #a5b4fc;
      text-decoration: none;
      font-size: 10.5px;
      cursor: pointer;
    }
    .pop-link:hover { text-decoration: underline; }
    .row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin: 4px 0;
      font-size: 11px;
    }
    .k { color: #9a9aa4; }
    .v {
      font-family: ui-monospace, "SF Mono", Menlo, monospace;
      font-variant-numeric: tabular-nums;
      color: #ededf1;
    }
    .bar {
      height: 4px;
      border-radius: 2px;
      background: rgba(255, 255, 255, 0.1);
      overflow: hidden;
      margin: 3px 0 7px;
    }
    .bar > i {
      display: block;
      height: 100%;
      background: #6fd99b;
      border-radius: 2px;
    }
    .bar > i.warn { background: #f2b544; }
    .bar > i.danger { background: #f28b82; }
  `;

  function applyCorner(host, posName) {
    try {
      const saved = JSON.parse(localStorage.getItem(POS_KEY) || 'null');
      if (saved && typeof saved.left === 'number' && typeof saved.top === 'number') {
        const maxX = Math.max(20, window.innerWidth - 180);
        const maxY = Math.max(20, window.innerHeight - 40);
        host.style.left = Math.min(Math.max(8, saved.left), maxX) + 'px';
        host.style.top = Math.min(Math.max(8, saved.top), maxY) + 'px';
        host.style.right = 'auto';
        host.style.bottom = 'auto';
        return;
      }
    } catch {}
    host.style.left = 'auto';
    host.style.top = 'auto';
    host.style.right = '16px';
    host.style.bottom = '14px';
    if (posName === 'top-right') {
      host.style.top = '44px';
      host.style.bottom = 'auto';
    } else if (posName === 'bottom-left') {
      host.style.left = '16px';
      host.style.right = 'auto';
    } else if (posName === 'top-left') {
      host.style.left = '16px';
      host.style.top = '44px';
      host.style.right = 'auto';
      host.style.bottom = 'auto';
    }
  }

  function ensureHost(posName) {
    let host = document.getElementById(HOST_ID);
    if (host && host.shadowRoot) {
      return host;
    }
    if (host) host.remove();
    host = document.createElement('div');
    host.id = HOST_ID;
    host.style.cssText = 'position:fixed;z-index:2147483647;margin:0;padding:0;pointer-events:auto;';
    applyCorner(host, posName);

    const shadow = host.attachShadow({ mode: 'open' });
    let styled = false;
    try {
      if (typeof CSSStyleSheet !== 'undefined' && shadow.adoptedStyleSheets) {
        const sheet = new CSSStyleSheet();
        sheet.replaceSync(CSS);
        shadow.adoptedStyleSheets = [sheet];
        styled = true;
      }
    } catch {}
    if (!styled) {
      try {
        const st = document.createElement('style');
        st.textContent = CSS;
        shadow.appendChild(st);
      } catch {}
    }

    const wrap = document.createElement('div');
    wrap.className = 'wrap';
    wrap.innerHTML = `
      <div class="pop openable" id="pop"></div>
      <div class="pill" id="pill">
        <span class="dot" id="dot"></span>
        <span class="badge" id="inst">AGY</span>
        <span class="metric" id="ctx">Ctx --</span>
        <span class="sep">·</span>
        <span class="metric" id="quota">5h --</span>
        <span class="sep">·</span>
        <span class="metric" id="cost">$0.00</span>
        <button class="btn" id="toggle" title="折叠">×</button>
      </div>
      <button class="mini" id="mini" title="展开 AgyMate HUD" style="display:none">⚡</button>
    `;
    shadow.appendChild(wrap);
    document.documentElement.appendChild(host);

    const pill = shadow.getElementById('pill');
    const mini = shadow.getElementById('mini');
    const toggle = shadow.getElementById('toggle');

    function syncCollapseUI() {
      const collapsed = localStorage.getItem(COLLAPSE_KEY) === '1';
      pill.style.display = collapsed ? 'none' : 'inline-flex';
      mini.style.display = collapsed ? 'inline-flex' : 'none';
      const pop = shadow.getElementById('pop');
      if (collapsed) pop.classList.remove('openable');
      else pop.classList.add('openable');
    }
    syncCollapseUI();

    toggle.addEventListener('click', (e) => {
      e.stopPropagation();
      localStorage.setItem(COLLAPSE_KEY, '1');
      syncCollapseUI();
    });
    mini.addEventListener('click', (e) => {
      e.stopPropagation();
      localStorage.setItem(COLLAPSE_KEY, '0');
      syncCollapseUI();
    });

    let drag = null;
    pill.addEventListener('mousedown', (e) => {
      if (e.target === toggle) return;
      const rect = host.getBoundingClientRect();
      drag = { sx: e.clientX, sy: e.clientY, ox: rect.left, oy: rect.top, moved: false };
      pill.classList.add('dragging');
    });
    window.addEventListener('mousemove', (e) => {
      if (!drag) return;
      const dx = e.clientX - drag.sx;
      const dy = e.clientY - drag.sy;
      if (Math.abs(dx) > 3 || Math.abs(dy) > 3) drag.moved = true;
      if (!drag.moved) return;
      const left = Math.min(Math.max(8, drag.ox + dx), Math.max(20, window.innerWidth - 180));
      const top = Math.min(Math.max(8, drag.oy + dy), Math.max(20, window.innerHeight - 36));
      host.style.left = left + 'px';
      host.style.top = top + 'px';
      host.style.right = 'auto';
      host.style.bottom = 'auto';
    });
    window.addEventListener('mouseup', () => {
      if (!drag) return;
      pill.classList.remove('dragging');
      if (drag.moved) {
        const rect = host.getBoundingClientRect();
        try { localStorage.setItem(POS_KEY, JSON.stringify({ left: rect.left, top: rect.top })); } catch {}
      }
      drag = null;
    });

    return host;
  }

  function esc(s) {
    return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  function fmtTok(n) {
    n = Number(n || 0);
    if (n >= 1000000) return (n / 1000000).toFixed(2) + 'M';
    if (n >= 1000) return Math.round(n / 1000) + 'k';
    return String(n);
  }

  window.__agymateHudUpdate = function(data) {
    if (!data) return;
    const host = ensureHost(data.position || 'bottom-right');
    const s = host.shadowRoot;
    const dot = s.getElementById('dot');
    const inst = s.getElementById('inst');
    const ctx = s.getElementById('ctx');
    const quota = s.getElementById('quota');
    const cost = s.getElementById('cost');
    const pop = s.getElementById('pop');

    dot.className = 'dot ' + (data.state === 'waiting_approval' ? 'waiting' : (data.state === 'running' ? 'running' : ''));
    inst.textContent = data.badge || 'AGY';

    const ctxPct = Number(data.contextPct || 0);
    const ctxWarn = Number(data.contextWarnPct || 80);
    ctx.textContent = data.contextMax > 0 ? `Ctx ${Math.round(ctxPct)}% (${fmtTok(data.contextUsed)})` : 'Ctx --';
    ctx.className = 'metric' + (ctxPct >= 92 ? ' danger' : (ctxPct >= ctxWarn ? ' warn' : ''));

    const qPct = Number(data.quotaPct ?? -1);
    quota.textContent = qPct >= 0 ? `${data.quotaLabel || '5h'} ${Math.round(qPct)}%` : '配额 --';
    quota.className = 'metric' + (qPct >= 0 && qPct <= 10 ? ' danger' : (qPct >= 0 && qPct <= 25 ? ' warn' : ''));

    cost.textContent = data.todayCostLabel || '$0.00';

    const rect = host.getBoundingClientRect();
    if (rect.top < 210) pop.classList.add('top-pos');
    else pop.classList.remove('top-pos');

    let rows = '';
    if (Array.isArray(data.buckets)) {
      for (const b of data.buckets) {
        const pct = Math.max(0, Math.min(100, Number(b.pct || 0)));
        const cls = pct <= 10 ? 'danger' : (pct <= 25 ? 'warn' : '');
        rows += `
          <div class="row"><span class="k">${esc(b.name)}</span><span class="v">${pct.toFixed(1)}% · ${esc(b.resetLeft || '')}</span></div>
          <div class="bar"><i class="${cls}" style="width:${pct}%"></i></div>
        `;
      }
    }
    let bd = '';
    if (Array.isArray(data.breakdown) && data.breakdown.length > 0) {
      bd = '<div style="margin-top:6px;padding-top:6px;border-top:1px solid rgba(255,255,255,0.08)">';
      for (const g of data.breakdown) {
        bd += `<div class="row"><span class="k">${esc(g.name)}</span><span class="v">${fmtTok(g.tokens)}</span></div>`;
      }
      bd += '</div>';
    }
    pop.innerHTML = `
      <div class="pop-head">
        <span>${esc(data.badge || 'AGY')} · ${esc(data.account || '未登录')}</span>
        <a class="pop-link" href="${esc(data.hubUrl || 'http://127.0.0.1:3778')}" target="_blank" rel="noopener">控制台 ↗</a>
      </div>
      ${rows || '<div class="row"><span class="k">暂无本地配额数据</span></div>'}
      <div class="row"><span class="k">今日 Token / 缓存命中</span><span class="v">${fmtTok(data.todayTokens)} · ${Math.round(data.cacheHitRate || 0)}%</span></div>
      ${bd}
    `;
  };

  window.__agymateHudRemove = function() {
    const host = document.getElementById(HOST_ID);
    if (host) host.remove();
  };
})();
