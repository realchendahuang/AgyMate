(() => {
  const HOST_ID = 'agymate-hud-host';
  const OVERLAY_ID = 'agymate-overlay-host';
  const COMPOSER_ATTR = 'data-agymate-composer-host';
  const LEGACY_POS_KEY = 'agymate_hud_pos_v1';
  const POS_KEY = 'agymate_hud_pos_v2';
  const TAB_KEY = 'agymate_hud_tab_v1';

  // Teardown previous hot-reloaded instance cleanly
  if (typeof window.__agymateHudRemove === 'function') {
    try { window.__agymateHudRemove(); } catch {}
  }
  try { localStorage.removeItem(LEGACY_POS_KEY); } catch {}
  try {
    document.getElementById(HOST_ID)?.remove();
    document.getElementById(OVERLAY_ID)?.remove();
    document.querySelectorAll?.(`[${COMPOSER_ATTR}]`).forEach((el) => el.remove());
  } catch {}

  const VALID_POSITIONS = new Set(['top-right', 'top-left', 'bottom-right', 'bottom-left']);
  function normalizePos(pos) {
    const p = String(pos || '').trim().toLowerCase();
    return VALID_POSITIONS.has(p) ? p : 'top-right';
  }

  // ---- Theme tokens (Light / Dark adaptive, synced with Antigravity host vars) ----
  const THEME_VARS_CSS = `
    :host {
      all: initial;
      -webkit-app-region: no-drag;
      --agy-fg: #e4e4e7;
      --agy-fg-strong: #fafafa;
      --agy-muted: #94a3b8;
      --agy-dim: #71717a;
      --agy-hover: rgba(255, 255, 255, 0.08);
      --agy-active: rgba(255, 255, 255, 0.13);
      --agy-border: rgba(255, 255, 255, 0.11);
      --agy-divider: rgba(255, 255, 255, 0.09);
      --agy-panel-bg: rgba(26, 27, 33, 0.96);
      --agy-card-bg: rgba(255, 255, 255, 0.035);
      --agy-card-border: rgba(255, 255, 255, 0.075);
      --agy-track: rgba(255, 255, 255, 0.14);
      --agy-shadow: 0 18px 42px rgba(0, 0, 0, 0.56), 0 2px 10px rgba(0, 0, 0, 0.30);
      --agy-tip-shadow: 0 10px 28px rgba(0, 0, 0, 0.45);
      --agy-accent: #60a5fa;
      --agy-accent-soft: rgba(96, 165, 250, 0.16);
      --agy-ok: #34d399;
      --agy-warn: #f59e0b;
      --agy-orange: #fb923c;
      --agy-danger: #ef4444;
      --agy-compacted: #a78bfa;
      --agy-seg-sys: #94a3b8;
      --agy-seg-custom: #a78bfa;
      --agy-seg-user: #38bdf8;
      --agy-seg-agent: #3b82f6;
      --agy-seg-tool: #f59e0b;
      --agy-heat-0: rgba(255, 255, 255, 0.06);
      --agy-heat-1: #3b2369;
      --agy-heat-2: #5b21b6;
      --agy-heat-3: #7c3aed;
      --agy-heat-4: #a78bfa;
      color-scheme: dark;
      font-family: -apple-system, BlinkMacSystemFont, "Google Sans Flex", "PingFang SC", "Hiragino Sans GB", "Inter", system-ui, sans-serif;
    }
    :host([data-theme="light"]) {
      --agy-fg: #27272a;
      --agy-fg-strong: #09090b;
      --agy-muted: #71717a;
      --agy-dim: #a1a1aa;
      --agy-hover: rgba(10, 10, 12, 0.055);
      --agy-active: rgba(10, 10, 12, 0.095);
      --agy-border: rgba(10, 10, 12, 0.10);
      --agy-divider: rgba(10, 10, 12, 0.08);
      --agy-panel-bg: rgba(255, 255, 255, 0.97);
      --agy-card-bg: rgba(10, 10, 12, 0.028);
      --agy-card-border: rgba(10, 10, 12, 0.075);
      --agy-track: rgba(10, 10, 12, 0.13);
      --agy-shadow: 0 14px 36px rgba(0, 0, 0, 0.13), 0 2px 8px rgba(0, 0, 0, 0.06);
      --agy-tip-shadow: 0 8px 24px rgba(0, 0, 0, 0.12), 0 1px 4px rgba(0, 0, 0, 0.06);
      --agy-accent: #2563eb;
      --agy-accent-soft: rgba(37, 99, 235, 0.10);
      --agy-ok: #16a34a;
      --agy-warn: #d97706;
      --agy-orange: #ea580c;
      --agy-danger: #dc2626;
      --agy-compacted: #7c3aed;
      --agy-seg-sys: #64748b;
      --agy-seg-custom: #7c3aed;
      --agy-seg-user: #0284c7;
      --agy-seg-agent: #2563eb;
      --agy-seg-tool: #d97706;
      --agy-heat-0: rgba(10, 10, 12, 0.06);
      --agy-heat-1: #ede9fe;
      --agy-heat-2: #c4b5fd;
      --agy-heat-3: #8b5cf6;
      --agy-heat-4: #6d28d9;
      color-scheme: light;
    }
    *, *::before, *::after { box-sizing: border-box; }
  `;

  // ---- Top Header Toolbar Dock CSS (antigravity-context-viewer style) ----
  const TOPBAR_CSS = THEME_VARS_CSS + `
    :host {
      display: inline-flex;
      align-items: center;
      height: 28px;
      flex-shrink: 0;
      vertical-align: middle;
      user-select: none;
      -webkit-user-select: none;
      -webkit-app-region: no-drag;
    }
    :host([data-dock="flow"]) {
      position: relative !important;
      top: auto !important;
      right: auto !important;
      left: auto !important;
      bottom: auto !important;
      margin-right: 4px;
    }
    :host([data-dock="fixed"]) {
      position: fixed;
      top: 6px;
      right: 16px;
      z-index: 2147483000;
      padding: 2px 4px;
      border-radius: 10px;
      background: var(--agy-panel-bg);
      border: 1px solid var(--agy-border);
      box-shadow: var(--agy-tip-shadow);
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
    }
    .tb {
      display: inline-flex;
      align-items: center;
      gap: 2px;
      height: 28px;
      font-size: 12px;
      line-height: 1;
      color: var(--agy-fg);
      -webkit-font-smoothing: antialiased;
    }
    .unit {
      all: unset;
      display: inline-flex;
      align-items: center;
      gap: 5px;
      height: 28px;
      padding: 0 7px;
      border-radius: 7px;
      border: 1px solid transparent;
      color: var(--agy-fg);
      font-size: 12px;
      font-weight: 500;
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
      cursor: pointer;
      -webkit-app-region: no-drag;
      transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
    }
    .unit:hover, .unit:focus-visible {
      background: var(--agy-hover);
      color: var(--agy-fg-strong);
    }
    .unit[data-active="true"] {
      background: var(--agy-active);
      border-color: var(--agy-border);
      color: var(--agy-fg-strong);
    }
    .dot {
      width: 6.5px;
      height: 6.5px;
      border-radius: 50%;
      flex: none;
      background: var(--agy-dim);
      transition: background 0.2s ease, box-shadow 0.2s ease;
    }
    .dot.running {
      background: var(--agy-ok);
      box-shadow: 0 0 0 2.5px rgba(52, 211, 153, 0.22);
    }
    .dot.waiting {
      background: var(--agy-warn);
      box-shadow: 0 0 0 2.5px rgba(245, 158, 11, 0.24);
    }
    .dot.offline {
      background: var(--agy-dim);
      opacity: 0.6;
    }
    .inst-lbl {
      font-size: 11px;
      font-weight: 600;
      letter-spacing: 0.01em;
      color: var(--agy-muted);
      max-width: 72px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .unit:hover .inst-lbl, .unit[data-active="true"] .inst-lbl {
      color: var(--agy-fg-strong);
    }
    .div {
      width: 1px;
      height: 12px;
      background: var(--agy-divider);
      margin: 0 1px;
      flex: none;
    }
    .ring-svg {
      width: 16px;
      height: 16px;
      flex: none;
      display: block;
    }
    .ring-track {
      stroke: var(--agy-track);
    }
    .ring-fill {
      transition: stroke-dashoffset 0.28s cubic-bezier(0.22, 1, 0.36, 1), stroke 0.2s ease;
    }
    .val {
      font-variant-numeric: tabular-nums;
      font-weight: 600;
      font-size: 11.5px;
    }
    .lbl {
      font-size: 11px;
      color: var(--agy-muted);
      font-weight: 500;
    }
    .val.ok { color: var(--agy-ok); }
    .val.warn { color: var(--agy-warn); }
    .val.danger { color: var(--agy-danger); }
    .val.compacted { color: var(--agy-compacted); }
    .bolt {
      display: inline-flex;
      align-items: center;
      color: var(--agy-warn);
      opacity: 0.9;
    }
    .icon-btn {
      padding: 0 6px;
      color: var(--agy-muted);
    }
    .icon-btn:hover {
      color: var(--agy-fg-strong);
    }
    .alert-pill {
      display: none;
      width: 5px;
      height: 5px;
      border-radius: 50%;
      background: var(--agy-danger);
    }
    .alert-pill.on { display: inline-block; }

    /* Responsive 4-stage collapsing: wide -> compact -> minimal -> ring-only */
    :host([data-layout="compact"]) .unit { padding: 0 5px; gap: 4px; }
    :host([data-layout="compact"]) .lbl,
    :host([data-layout="compact"]) #u-inst .inst-lbl { display: none; }
    :host([data-layout="minimal"]) #u-inst,
    :host([data-layout="minimal"]) #u-speed,
    :host([data-layout="minimal"]) #u-cost,
    :host([data-layout="minimal"]) #d-1,
    :host([data-layout="minimal"]) #d-2 { display: none !important; }
    :host([data-layout="ring-only"]) #u-inst,
    :host([data-layout="ring-only"]) #u-quota,
    :host([data-layout="ring-only"]) #u-speed,
    :host([data-layout="ring-only"]) #u-cost,
    :host([data-layout="ring-only"]) #u-console,
    :host([data-layout="ring-only"]) .div { display: none !important; }
    :host([data-layout="ring-only"]) #u-ctx .val { display: none; }
  `;

  // ---- Composer Inline Ring + Pulse Capsule CSS (brien9 + antigravity-pulse style) ----
  const COMPOSER_CSS = THEME_VARS_CSS + `
    :host {
      display: inline-flex;
      align-items: center;
      margin-left: auto;
      margin-right: 4px;
      flex-shrink: 0;
      user-select: none;
      -webkit-user-select: none;
    }
    .cap {
      all: unset;
      display: inline-flex;
      align-items: center;
      gap: 5px;
      height: 24px;
      padding: 0 7px;
      border-radius: 999px;
      border: 1px solid transparent;
      color: var(--agy-muted);
      font-size: 11.5px;
      font-weight: 500;
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
      cursor: pointer;
      transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
    }
    .cap:hover, .cap[data-active="true"] {
      background: var(--agy-hover);
      border-color: var(--agy-border);
      color: var(--agy-fg-strong);
    }
    .cap-ring {
      width: 15px;
      height: 15px;
      flex: none;
      display: block;
    }
    .cap-ring .ring-track {
      stroke: var(--agy-track);
    }
    .cap-ring .ring-fill {
      transition: stroke-dashoffset 0.28s cubic-bezier(0.22, 1, 0.36, 1), stroke 0.2s ease;
    }
    .cap-pct {
      font-weight: 600;
      color: var(--agy-fg);
    }
    .cap-pct.warn { color: var(--agy-warn); }
    .cap-pct.orange { color: var(--agy-orange); }
    .cap-pct.danger { color: var(--agy-danger); }
    .cap-pct.compacted { color: var(--agy-compacted); }
    .cap-sep {
      width: 1px;
      height: 10px;
      background: var(--agy-divider);
      flex: none;
    }
    .cap-tps {
      font-size: 11px;
      color: var(--agy-muted);
    }
    .cap-tps.live { color: var(--agy-fg); }
  `;

  // ---- Overlay Portal CSS: 3-Layer Frosted Tooltip + Unified 4-Tab Popover ----
  const OVERLAY_CSS = THEME_VARS_CSS + `
    :host {
      position: fixed;
      inset: 0;
      z-index: 2147483647;
      pointer-events: none;
      display: block;
    }
    /* Crisp 3-layer hover tooltip (brien9 style) */
    .tip {
      position: fixed;
      min-width: 184px;
      max-width: 290px;
      padding: 9px 12px;
      border-radius: 10px;
      background: var(--agy-panel-bg);
      border: 1px solid var(--agy-border);
      box-shadow: var(--agy-tip-shadow);
      backdrop-filter: blur(20px) saturate(150%);
      -webkit-backdrop-filter: blur(20px) saturate(150%);
      color: var(--agy-fg);
      font-size: 11.5px;
      line-height: 1.4;
      font-variant-numeric: tabular-nums;
      pointer-events: none;
      opacity: 0;
      visibility: hidden;
      transform: translateY(3px);
      transition: opacity 0.12s ease, transform 0.12s ease, visibility 0.12s ease;
    }
    .tip.open {
      opacity: 1;
      visibility: visible;
      transform: translateY(0);
    }
    .tip-title {
      font-size: 11px;
      color: var(--agy-muted);
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      margin-bottom: 2px;
    }
    .tip-hero {
      font-size: 13px;
      font-weight: 600;
      color: var(--agy-fg-strong);
      margin-bottom: 2px;
    }
    .tip-hero.warn { color: var(--agy-warn); }
    .tip-hero.danger { color: var(--agy-danger); }
    .tip-hero.compacted { color: var(--agy-compacted); }
    .tip-sub {
      font-size: 11px;
      color: var(--agy-muted);
    }

    /* Unified 4-tab Interactive Popover Card */
    .pop {
      position: fixed;
      width: 420px;
      max-width: calc(100vw - 16px);
      border-radius: 14px;
      background: var(--agy-panel-bg);
      border: 1px solid var(--agy-border);
      box-shadow: var(--agy-shadow);
      backdrop-filter: blur(24px) saturate(165%);
      -webkit-backdrop-filter: blur(24px) saturate(165%);
      color: var(--agy-fg);
      display: flex;
      flex-direction: column;
      pointer-events: none;
      opacity: 0;
      visibility: hidden;
      transform: translateY(-4px) scale(0.985);
      transition: opacity 0.14s cubic-bezier(0.16, 1, 0.3, 1), transform 0.15s cubic-bezier(0.16, 1, 0.3, 1), visibility 0.14s;
      overflow: hidden;
    }
    .pop.up {
      transform: translateY(4px) scale(0.985);
    }
    .pop.open {
      pointer-events: auto;
      opacity: 1;
      visibility: visible;
      transform: translateY(0) scale(1);
    }
    .pop-head {
      padding: 10px 13px 8px;
      border-bottom: 1px solid var(--agy-divider);
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    .head-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
    }
    .head-id {
      display: flex;
      align-items: center;
      gap: 6px;
      min-width: 0;
    }
    .pill-tag {
      font-size: 10.5px;
      font-weight: 600;
      padding: 2px 7px;
      border-radius: 6px;
      background: var(--agy-accent-soft);
      color: var(--agy-accent);
      white-space: nowrap;
    }
    .state-tag {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      font-size: 10.5px;
      font-weight: 600;
      padding: 2px 7px;
      border-radius: 999px;
      background: var(--agy-card-bg);
      border: 1px solid var(--agy-card-border);
      color: var(--agy-muted);
      white-space: nowrap;
    }
    .state-tag i {
      width: 5px;
      height: 5px;
      border-radius: 50%;
      background: currentColor;
      display: block;
    }
    .state-tag.running { color: var(--agy-ok); }
    .state-tag.waiting { color: var(--agy-warn); }
    .acct-meta {
      font-size: 11px;
      color: var(--agy-muted);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      flex: 1;
    }
    .head-acts {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      flex: none;
    }
    .h-btn {
      all: unset;
      display: inline-flex;
      align-items: center;
      gap: 4px;
      height: 23px;
      padding: 0 8px;
      border-radius: 6px;
      font-size: 11px;
      font-weight: 500;
      color: var(--agy-fg);
      background: var(--agy-card-bg);
      border: 1px solid var(--agy-card-border);
      text-decoration: none;
      cursor: pointer;
      white-space: nowrap;
      transition: background 0.12s ease, border-color 0.12s ease;
    }
    .h-btn:hover { background: var(--agy-hover); border-color: var(--agy-border); color: var(--agy-fg-strong); }
    .h-btn.primary {
      background: var(--agy-accent-soft);
      border-color: transparent;
      color: var(--agy-accent);
      font-weight: 600;
    }
    .h-btn.primary:hover { filter: brightness(1.06); }

    /* Segmented Tabs */
    .tabs {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      gap: 3px;
      padding: 3px;
      border-radius: 8px;
      background: var(--agy-card-bg);
      border: 1px solid var(--agy-card-border);
    }
    .tab {
      all: unset;
      text-align: center;
      padding: 4px 4px;
      border-radius: 6px;
      font-size: 11px;
      font-weight: 500;
      color: var(--agy-muted);
      cursor: pointer;
      transition: background 0.12s ease, color 0.12s ease;
    }
    .tab:hover { color: var(--agy-fg); }
    .tab.on {
      background: var(--agy-panel-bg);
      color: var(--agy-fg-strong);
      font-weight: 600;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.10);
    }

    .pop-body {
      padding: 11px 13px 13px;
      overflow-y: auto;
      overscroll-behavior: contain;
      display: flex;
      flex-direction: column;
      gap: 10px;
      font-size: 11.5px;
    }
    .pop-body::-webkit-scrollbar { width: 5px; }
    .pop-body::-webkit-scrollbar-thumb { background: var(--agy-track); border-radius: 3px; }

    /* Shared UI primitives inside popover */
    .card {
      padding: 9px 10px;
      border-radius: 10px;
      background: var(--agy-card-bg);
      border: 1px solid var(--agy-card-border);
      display: flex;
      flex-direction: column;
      gap: 7px;
    }
    .card-h {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
    }
    .c-title {
      font-size: 11px;
      font-weight: 600;
      color: var(--agy-muted);
      letter-spacing: 0.02em;
    }
    .mono {
      font-family: ui-monospace, "SF Mono", Menlo, monospace;
      font-variant-numeric: tabular-nums;
    }
    .big-stat {
      display: flex;
      align-items: baseline;
      gap: 7px;
    }
    .big-num {
      font-size: 20px;
      font-weight: 700;
      letter-spacing: -0.02em;
      color: var(--agy-fg-strong);
      font-variant-numeric: tabular-nums;
    }
    .big-of { font-size: 12px; color: var(--agy-muted); font-variant-numeric: tabular-nums; }
    .big-pct { margin-left: auto; font-size: 14px; font-weight: 700; font-variant-numeric: tabular-nums; }
    .big-pct.warn { color: var(--agy-warn); }
    .big-pct.danger { color: var(--agy-danger); }
    .big-pct.compacted { color: var(--agy-compacted); }

    /* Segmented bar */
    .seg-bar {
      height: 6px;
      border-radius: 999px;
      background: var(--agy-track);
      overflow: hidden;
      display: flex;
    }
    .seg-bar > span { height: 100%; transition: width 0.25s ease; }

    /* /context 100-dot Usage Grid (20x5) */
    .dot-grid {
      display: grid;
      grid-template-columns: repeat(20, 1fr);
      gap: 3.5px;
      padding: 2px 0;
    }
    .dot-cell {
      aspect-ratio: 1;
      border-radius: 50%;
      background: var(--agy-track);
    }

    /* Breakdown rows */
    .bd-list { display: flex; flex-direction: column; gap: 4px; }
    .bd-row {
      display: grid;
      grid-template-columns: 9px 1fr auto 42px;
      align-items: center;
      gap: 7px;
      font-size: 11.5px;
      padding: 2px 0;
    }
    .bd-swatch { width: 8px; height: 8px; border-radius: 2px; }
    .bd-swatch.rnd { border-radius: 50%; }
    .bd-swatch.free { background: transparent; border: 1.5px solid var(--agy-track); }
    .bd-name { color: var(--agy-fg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .bd-tok { font-variant-numeric: tabular-nums; font-weight: 500; color: var(--agy-fg-strong); text-align: right; }
    .bd-pct { font-variant-numeric: tabular-nums; color: var(--agy-muted); text-align: right; font-size: 11px; }
    .bd-children {
      display: flex;
      flex-wrap: wrap;
      gap: 4px;
      padding-left: 16px;
      margin-bottom: 2px;
    }
    .child-chip {
      font-size: 10px;
      padding: 1px 6px;
      border-radius: 4px;
      background: var(--agy-hover);
      color: var(--agy-muted);
      font-variant-numeric: tabular-nums;
    }

    .badge-mini {
      font-size: 10px;
      font-weight: 600;
      padding: 1px 6px;
      border-radius: 5px;
      background: var(--agy-accent-soft);
      color: var(--agy-accent);
      white-space: nowrap;
    }
    .badge-mini.compacted {
      background: rgba(139, 92, 246, 0.14);
      color: var(--agy-compacted);
    }
    .badge-mini.warn {
      background: rgba(245, 158, 11, 0.14);
      color: var(--agy-warn);
    }
    .badge-mini.danger {
      background: rgba(239, 68, 68, 0.14);
      color: var(--agy-danger);
    }

    /* 4-tile metric grid */
    .kpi4 {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      gap: 6px;
    }
    .kpi-tile {
      padding: 6px 7px;
      border-radius: 8px;
      background: var(--agy-card-bg);
      border: 1px solid var(--agy-card-border);
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .kpi-tile .k { font-size: 10px; color: var(--agy-muted); }
    .kpi-tile .v { font-size: 12.5px; font-weight: 700; color: var(--agy-fg-strong); font-variant-numeric: tabular-nums; white-space: nowrap; }

    /* Interactive Context Curve */
    .curve-svg {
      width: 100%;
      height: 86px;
      display: block;
      overflow: visible;
    }

    /* Per-Turn Accordion */
    .turn-list { display: flex; flex-direction: column; gap: 3px; }
    .turn-item {
      border-radius: 7px;
      border: 1px solid var(--agy-card-border);
      background: var(--agy-card-bg);
      overflow: hidden;
    }
    .turn-sum {
      display: grid;
      grid-template-columns: 32px 1fr auto auto auto;
      align-items: center;
      gap: 6px;
      padding: 5.5px 8px;
      cursor: pointer;
      font-size: 11px;
      font-variant-numeric: tabular-nums;
    }
    .turn-sum:hover { background: var(--agy-hover); }
    .turn-idx { font-weight: 600; color: var(--agy-muted); }
    .turn-snip { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--agy-fg); }
    .turn-delta { font-size: 10.5px; color: var(--agy-muted); }
    .turn-tps { font-weight: 600; color: var(--agy-accent); }
    .turn-detail {
      border-top: 1px solid var(--agy-divider);
      padding: 7px 9px;
      display: flex;
      flex-direction: column;
      gap: 5px;
      font-size: 11px;
    }
    .td-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 4px 10px;
    }
    .td-kv { display: flex; justify-content: space-between; gap: 6px; font-variant-numeric: tabular-nums; }
    .td-kv .k { color: var(--agy-muted); }
    .td-kv .v { color: var(--agy-fg-strong); font-weight: 500; }
    .tool-row {
      display: flex;
      justify-content: space-between;
      gap: 8px;
      font-size: 10.5px;
      padding: 2px 5px;
      border-radius: 4px;
      background: var(--agy-hover);
    }

    /* Quota Pool + Sparkline Cards */
    .q-card {
      padding: 8px 10px;
      border-radius: 9px;
      background: var(--agy-card-bg);
      border: 1px solid var(--agy-card-border);
      display: flex;
      flex-direction: column;
      gap: 5px;
    }
    .q-top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
    .q-name { font-weight: 600; color: var(--agy-fg-strong); font-size: 11.5px; }
    .q-pct { font-variant-numeric: tabular-nums; font-weight: 700; font-size: 12px; }
    .q-pct.ok { color: var(--agy-ok); }
    .q-pct.warn { color: var(--agy-warn); }
    .q-pct.danger { color: var(--agy-danger); }
    .bar-track { height: 5px; border-radius: 999px; background: var(--agy-track); overflow: hidden; }
    .bar-fill { display: block; height: 100%; border-radius: 999px; background: var(--agy-ok); transition: width 0.25s ease; }
    .bar-fill.warn { background: var(--agy-warn); }
    .bar-fill.danger { background: var(--agy-danger); }
    .bar-fill.ctx { background: var(--agy-accent); }
    .bar-fill.compacted { background: var(--agy-compacted); }
    .q-foot { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 10.5px; color: var(--agy-muted); }
    .spark-svg { width: 78px; height: 20px; flex: none; display: block; }

    /* Multi-account strip */
    .acct-chips { display: flex; flex-wrap: wrap; gap: 5px; }
    .acct-chip {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      padding: 3px 8px;
      border-radius: 999px;
      border: 1px solid var(--agy-card-border);
      background: var(--agy-card-bg);
      font-size: 10.5px;
      font-variant-numeric: tabular-nums;
    }
    .acct-chip.on { border-color: var(--agy-accent); background: var(--agy-accent-soft); }

    /* 12-week Activity Heatmap */
    .hm-wrap {
      display: grid;
      grid-template-columns: repeat(12, 1fr);
      gap: 3px;
    }
    .hm-col { display: grid; grid-template-rows: repeat(7, 1fr); gap: 3px; }
    .hm-cell {
      aspect-ratio: 1;
      border-radius: 2.5px;
      background: var(--agy-heat-0);
      cursor: pointer;
    }
    .hm-cell[data-l="1"] { background: var(--agy-heat-1); }
    .hm-cell[data-l="2"] { background: var(--agy-heat-2); }
    .hm-cell[data-l="3"] { background: var(--agy-heat-3); }
    .hm-cell[data-l="4"] { background: var(--agy-heat-4); }

    .empty-msg {
      padding: 16px 8px;
      text-align: center;
      color: var(--agy-muted);
      font-size: 11.5px;
    }
    .alert-strip {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 6px 9px;
      border-radius: 8px;
      background: rgba(245, 158, 11, 0.12);
      border: 1px solid rgba(245, 158, 11, 0.25);
      color: var(--agy-warn);
      font-size: 11px;
    }
  `;

  // ---- Utilities ----
  const GUID_RE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

  function esc(s) {
    return String(s ?? '').replace(/[&<>"']/g, (c) => (
      { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
    ));
  }

  function fmtTok(n) {
    n = Number(n || 0);
    if (!Number.isFinite(n) || n <= 0) return '0';
    if (n >= 1000000) return (n / 1000000).toFixed(n >= 10000000 ? 1 : 2).replace(/\.0+$/, '') + 'M';
    if (n >= 1000) return (n >= 100000 ? Math.round(n / 1000) : (n / 1000).toFixed(1).replace(/\.0$/, '')) + 'K';
    return String(Math.round(n));
  }

  function fmtMs(ms) {
    ms = Number(ms || 0);
    if (!Number.isFinite(ms) || ms <= 0) return '--';
    if (ms < 1000) return `${Math.round(ms)}ms`;
    return `${(ms / 1000).toFixed(2).replace(/\.?0+$/, '')}s`;
  }

  function hasChinese(s) {
    return /[\u4e00-\u9fa5]/.test(String(s || ''));
  }

  function zhBadge(raw) {
    const s = String(raw || '').trim();
    if (!s || /^main$/i.test(s)) return '主窗口';
    if (/^ide$/i.test(s)) return 'IDE';
    return s;
  }

  function zhStateClass(state) {
    const s = String(state || '').toLowerCase();
    if (s === 'waiting' || s === 'waiting_approval') return 'waiting';
    if (s === 'running') return 'running';
    if (s === 'offline') return 'offline';
    return '';
  }

  function zhStateText(state, stateLabel) {
    if (stateLabel && hasChinese(stateLabel)) return String(stateLabel).trim();
    const s = String(state || '').toLowerCase();
    if (s === 'waiting' || s === 'waiting_approval') return '待确认';
    if (s === 'running') return '运行中';
    if (s === 'offline') return '离线';
    return '空闲';
  }

  function zhPlan(planName) {
    const s = String(planName || '').trim();
    if (!s) return '';
    if (hasChinese(s)) return s;
    if (/ultra/i.test(s)) return 'Ultra 套餐';
    if (/\bpro\b/i.test(s)) return 'Pro 套餐';
    if (/team|enterprise|business/i.test(s)) return '团队套餐';
    if (/free|basic/i.test(s)) return '免费版';
    return s;
  }

  function zhResetLeft(raw) {
    let s = String(raw || '').trim();
    if (!s) return '';
    if (s === '即将重置' || s === '已重置') return s;
    if (s === '不足1分钟') return '1分钟内重置';
    if (s.includes('重置')) return s;
    return `${s}后重置`;
  }

  function zhShortWindow(raw, winHint) {
    const combined = `${raw || ''} ${winHint || ''}`.trim();
    if (/five\s*hour|5\s*-?\s*h(our)?s?|5\s*小时/i.test(combined)) return '5小时';
    if (/week(ly)?|7\s*-?\s*d(ay)?s?|周配额|周限额|每周/i.test(combined)) return '周配额';
    if (/day|daily|24\s*-?\s*h|日配额/i.test(combined)) return '日配额';
    if (/month(ly)?|30\s*-?\s*d|月配额/i.test(combined)) return '月配额';
    return hasChinese(raw) ? String(raw).trim() : '配额';
  }

  // ---- Theme detection from Antigravity host document ----
  function detectHostTheme() {
    try {
      const root = document.documentElement;
      const cs = getComputedStyle(root);
      const scheme = (cs.colorScheme || '').toLowerCase();
      if (scheme.includes('light') && !scheme.includes('dark')) return 'light';
      if (scheme.includes('dark') && !scheme.includes('light')) return 'dark';
      const nodes = [document.body, document.querySelector('div.h-screen.w-screen'), root];
      for (const n of nodes) {
        if (!n) continue;
        const bg = getComputedStyle(n).backgroundColor || '';
        const m = /^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)(?:[,\s/]+([\d.]+))?/.exec(bg);
        if (m && (m[4] === undefined || Number(m[4]) > 0.25)) {
          const lum = (0.2126 * Number(m[1]) + 0.7152 * Number(m[2]) + 0.0722 * Number(m[3])) / 255;
          return lum > 0.5 ? 'light' : 'dark';
        }
      }
      if (window.matchMedia?.('(prefers-color-scheme: light)')?.matches) return 'light';
    } catch {}
    return 'dark';
  }

  // ---- Conversation ID & React Fiber per-pane isolation ----
  function getUrlCascadeId() {
    try {
      const p = typeof location !== 'undefined' ? String(location.pathname || '') : '';
      const m = p.match(GUID_RE);
      return m ? m[0].toLowerCase() : '';
    } catch {}
    return '';
  }

  function resolveComposerPaneCascadeId(footerEl) {
    try {
      let n = footerEl;
      for (let d = 0; d < 12 && n && n !== document.body; d++, n = n.parentElement) {
        const fk = Object.keys(n).find((k) => k.startsWith('__reactFiber$'));
        if (fk) {
          let fiber = n[fk];
          for (let i = 0; i < 16 && fiber; i++, fiber = fiber.return) {
            const p = fiber.memoizedProps;
            if (p) {
              const cand = p.cascadeId || p.conversationId || p.value?.cascadeId || p.value?.conversationId;
              if (typeof cand === 'string' && GUID_RE.test(cand)) {
                return cand.match(GUID_RE)[0].toLowerCase();
              }
              const prov = p.value?.agentStateProvider;
              if (prov && typeof prov.getState === 'function') {
                try {
                  const st = prov.getState();
                  if (st && typeof st.conversationId === 'string' && GUID_RE.test(st.conversationId)) {
                    return st.conversationId.match(GUID_RE)[0].toLowerCase();
                  }
                  if (st && Array.isArray(st.turns) && st.turns.length === 0 && !st.conversationId) {
                    return '__draft__';
                  }
                } catch {}
              }
            }
          }
        }
      }
    } catch {}
    return getUrlCascadeId();
  }

  function contextColorTier(pct, warnPct, compacted) {
    if (pct >= 92) return { cls: 'danger', stroke: 'var(--agy-danger)' };
    if (pct >= (warnPct || 80)) return { cls: 'orange', stroke: 'var(--agy-orange)' };
    if (pct >= 60) return { cls: 'warn', stroke: 'var(--agy-warn)' };
    if (compacted) return { cls: 'compacted', stroke: 'var(--agy-compacted)' };
    return { cls: '', stroke: 'var(--agy-accent)' };
  }

  function quotaColorTier(pct) {
    if (pct < 0) return { cls: '', stroke: 'var(--agy-muted)' };
    if (pct <= 10) return { cls: 'danger', stroke: 'var(--agy-danger)' };
    if (pct <= 25) return { cls: 'warn', stroke: 'var(--agy-warn)' };
    return { cls: 'ok', stroke: 'var(--agy-ok)' };
  }

  function pickCapsuleQuota(data) {
    const buckets = Array.isArray(data.buckets) ? data.buckets : [];
    const activeModel = String(data.activeModel || '').toLowerCase();
    const scoreFamily = (b) => {
      const grp = String(b.group || b.name || '').toLowerCase();
      if (!activeModel) return false;
      if (activeModel.includes('claude')) return grp.includes('claude');
      if (activeModel.includes('gpt') || activeModel.includes('o1') || activeModel.includes('o3')) return grp.includes('gpt');
      if (activeModel.includes('gemini') || activeModel.includes('orchestrator')) return grp.includes('gemini');
      return false;
    };
    if (buckets.length > 0) {
      let lowestAll = null;
      let matching5h = null;
      for (const b of buckets) {
        const p = Number(b.pct);
        if (!Number.isFinite(p)) continue;
        if (!lowestAll || p < lowestAll.pct) lowestAll = { b, pct: p };
        const is5h = b.window === '5h' || /5\s*小时|five\s*hour/i.test(b.name || '');
        if (is5h && scoreFamily(b) && (!matching5h || p < matching5h.pct)) {
          matching5h = { b, pct: p };
        }
      }
      const chosen = (lowestAll && lowestAll.pct <= 10) ? lowestAll : (matching5h || lowestAll);
      if (chosen) {
        return {
          label: zhShortWindow(chosen.b.name, chosen.b.window),
          pct: chosen.pct,
          exhausting: Boolean(chosen.b.exhaustsBeforeReset || chosen.pct <= 10),
        };
      }
    }
    const rawPct = Number(data.quotaPct ?? -1);
    if (rawPct >= 0) {
      return {
        label: zhShortWindow(data.quotaLabel || '5小时', ''),
        pct: rawPct,
        exhausting: rawPct <= 10,
      };
    }
    return null;
  }

  // ---- Runtime State ----
  let abortCtrl = null;
  let domObserver = null;
  let resizeObserver = null;
  let themeObserver = null;
  let relayoutRAF = 0;
  let domRelayoutTimer = null;

  let latestData = null;
  let convCache = new Map(); // cascadeId -> { detail, context, fetchedAt }
  let activeTab = 'context';
  try {
    const savedTab = localStorage.getItem(TAB_KEY);
    if (['context', 'turns', 'quota', 'heatmap'].includes(savedTab)) activeTab = savedTab;
  } catch {}

  let popoverOpen = false;
  let popoverAnchorEl = null;
  let popoverDirection = 'down'; // 'down' | 'up'
  let popoverCascadeId = '';
  let tipTargetEl = null;
  let tipTimer = null;
  let currentTheme = detectHostTheme();
  let lastPopBodyKey = '';

  function createShadowHost(tag, id, css) {
    const host = document.createElement(tag);
    if (id) host.id = id;
    host.setAttribute('data-theme', currentTheme);
    const shadow = host.attachShadow({ mode: 'open' });
    let styled = false;
    try {
      if (typeof CSSStyleSheet !== 'undefined' && shadow.adoptedStyleSheets) {
        const sheet = new CSSStyleSheet();
        sheet.replaceSync(css);
        shadow.adoptedStyleSheets = [sheet];
        styled = true;
      }
    } catch {}
    if (!styled) {
      const st = document.createElement('style');
      st.textContent = css;
      shadow.appendChild(st);
    }
    return { host, shadow };
  }

  function applyThemeToAllHosts() {
    const next = detectHostTheme();
    currentTheme = next;
    const composerHosts = typeof document.querySelectorAll === 'function'
      ? Array.from(document.querySelectorAll(`[${COMPOSER_ATTR}]`))
      : [];
    const hosts = [
      document.getElementById(HOST_ID),
      document.getElementById(OVERLAY_ID),
      ...composerHosts,
    ].filter(Boolean);
    for (const h of hosts) {
      if (h.getAttribute?.('data-theme') !== next) h.setAttribute?.('data-theme', next);
    }
  }

  // ---- Overlay Portal (Tooltip + 4-Tab Popover) ----
  function ensureOverlay() {
    let ov = document.getElementById(OVERLAY_ID);
    if (ov && ov.shadowRoot && ov.dataset.v === '3') return ov;
    if (ov) ov.remove();

    const { host, shadow } = createShadowHost('div', OVERLAY_ID, OVERLAY_CSS);
    host.dataset.v = '3';
    const wrap = document.createElement('div');
    wrap.innerHTML = `
      <div class="tip" id="tip" role="tooltip">
        <div class="tip-title"><span id="tip-t"></span><span id="tip-tag"></span></div>
        <div class="tip-hero" id="tip-hero"></div>
        <div class="tip-sub" id="tip-sub"></div>
      </div>
      <div class="pop" id="pop" role="dialog" aria-label="AgyMate 遥测详情">
        <div class="pop-head">
          <div class="head-row">
            <div class="head-id">
              <span class="pill-tag" id="pop-badge">主窗口</span>
              <span class="state-tag" id="pop-state"><i></i><span id="pop-state-txt">空闲</span></span>
              <span class="acct-meta" id="pop-acct">未登录</span>
            </div>
            <div class="head-acts">
              <button type="button" class="h-btn" id="pop-refresh" title="刷新当前会话与历史遥测">刷新</button>
              <button type="button" class="h-btn" id="pop-sidecar" title="打开 AgyMate 侧边栏视图">侧栏</button>
              <a class="h-btn primary" id="pop-console" href="http://127.0.0.1:3778" target="_blank" rel="noopener noreferrer">控制台 ↗</a>
            </div>
          </div>
          <div class="tabs" role="tablist">
            <button type="button" class="tab" data-tab="context">上下文 /context</button>
            <button type="button" class="tab" data-tab="turns">每轮遥测</button>
            <button type="button" class="tab" data-tab="quota">额度与趋势</button>
            <button type="button" class="tab" data-tab="heatmap">用量与热力</button>
          </div>
        </div>
        <div class="pop-body" id="pop-body"></div>
      </div>
    `;
    shadow.appendChild(wrap);
    document.documentElement.appendChild(host);

    const pop = shadow.getElementById('pop');
    pop.addEventListener('pointerdown', (e) => e.stopPropagation());
    pop.addEventListener('click', (e) => {
      const tabBtn = e.target.closest?.('.tab[data-tab]');
      if (tabBtn) {
        const nextTab = tabBtn.getAttribute('data-tab');
        if (nextTab && nextTab !== activeTab) {
          activeTab = nextTab;
          try { localStorage.setItem(TAB_KEY, activeTab); } catch {}
          renderPopover();
          if (activeTab === 'turns' || activeTab === 'context') {
            maybeFetchConversation(popoverCascadeId);
          }
        }
        return;
      }
      const refBtn = e.target.closest?.('#pop-refresh');
      if (refBtn) {
        maybeFetchConversation(popoverCascadeId, true);
        return;
      }
      const scBtn = e.target.closest?.('#pop-sidecar');
      if (scBtn) {
        popoverOpen = false;
        syncOverlayVisibility();
        openSidecarOrFallback();
      }
    });

    shadow.getElementById('pop-body').addEventListener('mouseover', (e) => {
      const cell = e.target.closest?.('.hm-cell[data-tip]');
      if (cell) {
        const lbl = shadow.getElementById('hm-hover-lbl');
        if (lbl) lbl.textContent = cell.getAttribute('data-tip') || '';
      }
    });
    shadow.getElementById('pop-body').addEventListener('mouseleave', () => {
      const lbl = shadow.getElementById('hm-hover-lbl');
      if (lbl) lbl.textContent = '';
    });

    return host;
  }

  function showTooltip(anchorEl, title, tag, hero, heroCls, sub) {
    if (popoverOpen) return;
    const ov = ensureOverlay();
    const s = ov.shadowRoot;
    const tip = s.getElementById('tip');
    s.getElementById('tip-t').textContent = title || '';
    s.getElementById('tip-tag').textContent = tag || '';
    const heroEl = s.getElementById('tip-hero');
    heroEl.textContent = hero || '';
    heroEl.className = 'tip-hero' + (heroCls ? ' ' + heroCls : '');
    s.getElementById('tip-sub').textContent = sub || '';
    tipTargetEl = anchorEl;

    const r = anchorEl.getBoundingClientRect();
    const vw = window.innerWidth || 1280;
    const vh = window.innerHeight || 800;
    const tipW = Math.max(196, tip.offsetWidth || 240);
    const tipH = tip.offsetHeight || 72;
    const left = Math.max(8, Math.min(vw - tipW - 8, Math.round(r.left + r.width / 2 - tipW / 2)));
    const placeAbove = r.top > vh * 0.55 && r.top - tipH - 10 > 8;
    tip.style.left = left + 'px';
    tip.style.top = (placeAbove ? Math.round(r.top - tipH - 8) : Math.round(r.bottom + 8)) + 'px';
    tip.classList.add('open');
  }

  function hideTooltip(force) {
    if (tipTimer && force) {
      clearTimeout(tipTimer);
      tipTimer = null;
    }
    const ov = document.getElementById(OVERLAY_ID);
    if (!ov || !ov.shadowRoot) return;
    ov.shadowRoot.getElementById('tip')?.classList.remove('open');
    tipTargetEl = null;
  }

  function positionPopoverOverlay() {
    const ov = document.getElementById(OVERLAY_ID);
    if (!ov || !ov.shadowRoot) return;
    const pop = ov.shadowRoot.getElementById('pop');
    const body = ov.shadowRoot.getElementById('pop-body');
    if (!pop || !popoverAnchorEl || !popoverAnchorEl.isConnected) return;

    const vw = Math.max(340, window.innerWidth || 1280);
    const vh = Math.max(280, window.innerHeight || 800);
    const r = popoverAnchorEl.getBoundingClientRect();
    const popW = Math.min(420, vw - 16);
    pop.style.width = popW + 'px';

    const spaceBelow = vh - r.bottom - 14;
    const spaceAbove = r.top - 14;
    const openUp = popoverDirection === 'up' || (r.top > vh * 0.55 && spaceAbove >= spaceBelow);
    pop.classList.toggle('up', openUp);

    const maxBodyH = openUp
      ? Math.max(180, Math.min(490, spaceAbove - 92))
      : Math.max(180, Math.min(490, spaceBelow - 92));
    if (body) body.style.maxHeight = maxBodyH + 'px';

    const left = Math.max(8, Math.min(vw - popW - 8, Math.round(r.right - popW)));
    pop.style.left = left + 'px';
    if (openUp) {
      pop.style.bottom = Math.max(8, Math.round(vh - r.top + 8)) + 'px';
      pop.style.top = 'auto';
      pop.style.transformOrigin = 'bottom right';
    } else {
      pop.style.top = Math.max(8, Math.round(r.bottom + 8)) + 'px';
      pop.style.bottom = 'auto';
      pop.style.transformOrigin = 'top right';
    }
  }

  function syncOverlayVisibility() {
    const ov = document.getElementById(OVERLAY_ID);
    if (ov && ov.shadowRoot) {
      const pop = ov.shadowRoot.getElementById('pop');
      if (pop) {
        if (popoverOpen) {
          positionPopoverOverlay();
          pop.classList.add('open');
        } else {
          pop.classList.remove('open');
        }
      }
    }
    const topHost = document.getElementById(HOST_ID);
    topHost?.shadowRoot?.querySelectorAll?.('.unit').forEach((u) => {
      u.setAttribute?.('data-active', popoverOpen && u === popoverAnchorEl ? 'true' : 'false');
    });
    document.querySelectorAll?.(`[${COMPOSER_ATTR}]`).forEach((ch) => {
      const btn = ch.shadowRoot?.getElementById('cap');
      if (btn) btn.setAttribute?.('data-active', popoverOpen && ch === popoverAnchorEl ? 'true' : 'false');
    });
  }

  function openPopover(anchorEl, tabName, direction, cascadeId) {
    hideTooltip(true);
    if (popoverOpen && popoverAnchorEl === anchorEl && (!tabName || activeTab === tabName)) {
      popoverOpen = false;
      syncOverlayVisibility();
      return;
    }
    if (tabName) {
      activeTab = tabName;
      try { localStorage.setItem(TAB_KEY, activeTab); } catch {}
    }
    popoverAnchorEl = anchorEl;
    popoverDirection = direction || 'down';
    popoverCascadeId = cascadeId || getUrlCascadeId() || (latestData?.activeCascadeId || '');
    popoverOpen = true;
    ensureOverlay();
    renderPopover();
    syncOverlayVisibility();
    maybeFetchConversation(popoverCascadeId);
  }

  // ---- Sidecar Activation Helper ----
  function openSidecarOrFallback() {
    const hubURL = latestData?.hubUrl || 'http://127.0.0.1:3778';
    try {
      const root = document.getElementById('root') || document.querySelector('#app, body > div');
      const fk = root ? Object.keys(root).find((k) => k.startsWith('__reactFiber$') || k.startsWith('__reactContainer$')) : null;
      if (fk) {
        const q = [root[fk]];
        const visited = new Set();
        let store = null;
        let featCtx = null;
        while (q.length && visited.size < 4500) {
          const f = q.shift();
          if (!f || visited.has(f)) continue;
          visited.add(f);
          const v = f.memoizedProps?.value;
          if (v && typeof v === 'object') {
            if (v.store?.dispatch && !store) store = v.store;
            if (typeof v.get === 'function' && Array.isArray(v.featureNames) && v.featureNames.length > 0 && !featCtx) featCtx = v;
            if (store && featCtx) break;
          }
          if (f.child) q.push(f.child);
          if (f.sibling) q.push(f.sibling);
        }
        const ext = featCtx?.get?.('extensibility');
        const list = ext?.provider?.getState?.();
        const sc = Array.isArray(list) ? list.find((s) => s?.sidecarId === 'agymate/agymate' || String(s?.sidecarId || '').includes('agymate')) : null;
        if (sc) {
          const existingTab = document.querySelector('[data-tab-id*="agymate"]');
          if (existingTab) {
            existingTab.click();
            return;
          }
          const toggleAux = document.querySelector('[data-testid="toggle-aux-sidebar"]');
          if (toggleAux) {
            toggleAux.click();
            setTimeout(() => { document.querySelector('[data-tab-id*="agymate"]')?.click(); }, 180);
            return;
          }
        }
      }
    } catch {}
    window.open(hubURL + '/?mode=sidecar', '_blank', 'noopener,noreferrer');
  }

  // ---- Async Deep Conversation Fetch (/api/conversation) ----
  function maybeFetchConversation(cascadeId, force) {
    if (typeof fetch !== 'function' || !latestData?.hubUrl || !cascadeId || cascadeId === '__draft__') return;
    const cached = convCache.get(cascadeId);
    if (!force && cached && Date.now() - cached.fetchedAt < 3500) return;

    const query = `cascadeId=${encodeURIComponent(cascadeId)}`
      + (latestData.instanceId ? `&instance=${encodeURIComponent(latestData.instanceId)}` : '');

    fetch(`${latestData.hubUrl}/api/conversation?${query}`, { mode: 'cors' })
      .then((r) => (r.ok ? r.json() : null))
      .then((payload) => {
        if (!payload || !payload.ok) return;
        convCache.set(cascadeId, {
          fetchedAt: Date.now(),
          context: payload.context || null,
          detail: payload.detail || null,
        });
        if (popoverOpen) renderPopover();
      })
      .catch(() => {});
  }

  // ---- SVG Renderers ----
  const RING_R = 6.2;
  const RING_C = (2 * Math.PI * RING_R).toFixed(2);
  function setRingStroke(circleEl, pct, strokeColor) {
    if (!circleEl) return;
    const clamped = Math.max(0, Math.min(100, Number(pct || 0)));
    const offset = Number(RING_C) * (1 - clamped / 100);
    circleEl.setAttribute('stroke-dasharray', RING_C);
    circleEl.setAttribute('stroke-dashoffset', offset.toFixed(2));
    circleEl.style.stroke = strokeColor;
  }

  function buildSparklineSVG(points, strokeColor) {
    if (!Array.isArray(points) || points.length < 2) return '';
    const W = 78;
    const H = 20;
    const coords = points.map((v, i) => {
      const x = (i / (points.length - 1)) * (W - 2) + 1;
      const clamped = Math.max(0, Math.min(100, Number(v || 0)));
      const y = H - 2 - (clamped / 100) * (H - 4);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    const last = coords[coords.length - 1].split(',');
    return `
      <svg class="spark-svg" viewBox="0 0 ${W} ${H}" aria-hidden="true">
        <polyline fill="none" stroke="${strokeColor}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" points="${coords.join(' ')}"></polyline>
        <circle cx="${last[0]}" cy="${last[1]}" r="2" fill="${strokeColor}"></circle>
      </svg>
    `;
  }

  function buildGrowthCurveSVG(turns, limitTok) {
    if (!Array.isArray(turns) || turns.length < 2) return '';
    const W = 380;
    const H = 84;
    const padL = 6;
    const padR = 8;
    const padT = 8;
    const padB = 14;
    const innerW = W - padL - padR;
    const innerH = H - padT - padB;
    const maxY = Math.max(limitTok > 0 ? limitTok * 0.25 : 0, ...turns.map((t) => Number(t.promptTokens || 0)), 1000);

    const pts = turns.map((t, idx) => {
      const x = padL + (idx / Math.max(1, turns.length - 1)) * innerW;
      const y = padT + innerH - (Math.min(maxY, Number(t.promptTokens || 0)) / maxY) * innerH;
      return { x, y, t };
    });
    const poly = pts.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');
    const area = `${padL},${padT + innerH} ${poly} ${pts[pts.length - 1].x.toFixed(1)},${padT + innerH}`;
    const compactMarkers = pts
      .filter((p) => p.t.compacted)
      .map((p) => `<line x1="${p.x.toFixed(1)}" y1="${padT}" x2="${p.x.toFixed(1)}" y2="${padT + innerH}" stroke="var(--agy-compacted)" stroke-width="1" stroke-dasharray="3 2"></line>`)
      .join('');
    const dots = pts
      .map((p, idx) => {
        const isLast = idx === pts.length - 1;
        const fill = p.t.compacted ? 'var(--agy-compacted)' : 'var(--agy-accent)';
        return `<circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="${isLast ? 3 : 2}" fill="${fill}"><title>Turn #${p.t.turn} · ${fmtTok(p.t.promptTokens)}${p.t.tps ? ` · ${p.t.tps} tok/s` : ''}</title></circle>`;
      })
      .join('');

    return `
      <svg class="curve-svg" viewBox="0 0 ${W} ${H}">
        <defs>
          <linearGradient id="agyCurveGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="var(--agy-accent)" stop-opacity="0.24"></stop>
            <stop offset="100%" stop-color="var(--agy-accent)" stop-opacity="0.02"></stop>
          </linearGradient>
        </defs>
        ${compactMarkers}
        <polygon points="${area}" fill="url(#agyCurveGrad)"></polygon>
        <polyline fill="none" stroke="var(--agy-accent)" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" points="${poly}"></polyline>
        ${dots}
        <text x="${padL}" y="${H - 2}" font-size="9" fill="var(--agy-muted)">T${turns[0].turn}</text>
        <text x="${W - padR}" y="${H - 2}" font-size="9" text-anchor="end" fill="var(--agy-muted)">T${turns[turns.length - 1].turn} · 峰值 ${fmtTok(Math.max(...turns.map((x) => x.promptTokens || 0)))}</text>
      </svg>
    `;
  }

  // ---- Breakdown Segment Aggregation for 100-dot /context Grid ----
  function classifyGroup(g) {
    const key = String(g.key || '').toLowerCase();
    const name = String(g.name || '').toLowerCase();
    if (key === 'mcp' || key === 'skills' || key === 'rules' || key === 'subagents' || key === 'workflows' || key === 'plugins' ||
        /mcp|技能|规则|子代理|插件|workflow/i.test(name)) {
      return 'custom';
    }
    if (key === 'user' || /用户输入|user/i.test(name)) return 'user';
    if (key === 'agent' || key === 'thinking' || /模型回复|思考/i.test(name)) return 'agent';
    if (key === 'tool_output' || /工具结果|工具输出/i.test(name)) return 'tool';
    if (key === 'chat' || /对话/i.test(name)) return 'user';
    return 'system';
  }

  function renderContextTab(data, activeCtx, liveResp) {
    const used = Number(liveResp?.context?.estimatedTokensUsed ?? activeCtx?.used ?? data.contextUsed ?? 0);
    const maxTok = Number(liveResp?.context?.maxContextTokens ?? activeCtx?.max ?? data.contextMax ?? 0);
    const pct = maxTok > 0 ? Math.min(100, (used / maxTok) * 100) : Number(activeCtx?.pct ?? data.contextPct ?? 0);
    const title = liveResp?.context?.title || liveResp?.detail?.title || activeCtx?.title || data.activeTitle || '当前会话';
    const model = liveResp?.context?.model || liveResp?.detail?.modelName || activeCtx?.model || data.activeModel || '';
    const cpIdx = Number(liveResp?.context?.checkpointIndex ?? activeCtx?.checkpointIndex ?? data.checkpointIndex ?? -1);
    const compacted = Boolean((liveResp?.detail?.compactedCount || 0) > 0 || cpIdx >= 0 || activeCtx?.compacted || data.compacted);
    const rawGroups = liveResp?.context?.tokenBreakdown || activeCtx?.breakdown || data.breakdown || [];
    const freeTok = Math.max(0, maxTok - used);
    const tier = contextColorTier(pct, data.contextWarnPct, compacted);

    if (maxTok <= 0 && used <= 0) {
      return `<div class="empty-msg">暂无活跃上下文</div>`;
    }

    // Aggregate into 5 colored buckets for 100-dot matrix & stacked bar
    const segs = { system: 0, custom: 0, user: 0, agent: 0, tool: 0 };
    for (const g of rawGroups) {
      const cat = classifyGroup(g);
      segs[cat] += Number(g.numTokens ?? g.tokens ?? 0);
    }
    const segMeta = [
      { id: 'system', lbl: '系统与工具定义', color: 'var(--agy-seg-sys)', tok: segs.system },
      { id: 'custom', lbl: '规则/技能/MCP', color: 'var(--agy-seg-custom)', tok: segs.custom },
      { id: 'user', lbl: '用户与对话', color: 'var(--agy-seg-user)', tok: segs.user },
      { id: 'agent', lbl: '模型与思考', color: 'var(--agy-seg-agent)', tok: segs.agent },
      { id: 'tool', lbl: '工具调用输出', color: 'var(--agy-seg-tool)', tok: segs.tool },
    ];

    // Calculate 100 dots (proportional, non-empty gets >= 1 dot)
    const denom = Math.max(maxTok, used, 1);
    let counts = segMeta.map((s) => (s.tok > 0 ? Math.max(1, Math.floor((s.tok / denom) * 100)) : 0));
    const sumDots = counts.reduce((a, b) => a + b, 0);
    if (sumDots > 100) {
      counts = counts.map((c) => Math.floor((c * 100) / sumDots));
    }
    const freeDots = Math.max(0, 100 - counts.reduce((a, b) => a + b, 0));

    let dotsHTML = '';
    counts.forEach((n, i) => {
      for (let k = 0; k < n; k++) {
        dotsHTML += `<span class="dot-cell" style="background:${segMeta[i].color}" title="${esc(segMeta[i].lbl)}: ${fmtTok(segMeta[i].tok)}"></span>`;
      }
    });
    for (let k = 0; k < freeDots; k++) {
      dotsHTML += `<span class="dot-cell" title="空闲空间: ${fmtTok(freeTok)}"></span>`;
    }

    const barHTML = segMeta.map((s) => {
      const w = maxTok > 0 ? Math.max(0, (s.tok / maxTok) * 100) : 0;
      return w > 0 ? `<span style="width:${w.toFixed(2)}%;background:${s.color}"></span>` : '';
    }).join('');

    const groupsHTML = rawGroups.map((g) => {
      const tok = Number(g.numTokens ?? g.tokens ?? 0);
      const gPct = maxTok > 0 ? ((tok / maxTok) * 100).toFixed(1) : '0.0';
      const cat = classifyGroup(g);
      const col = segMeta.find((s) => s.id === cat)?.color || 'var(--agy-seg-sys)';
      const isRound = cat === 'user' || cat === 'agent';
      const children = Array.isArray(g.children) ? g.children : [];
      const childTip = children.slice(0, 6).map((ch) => `${ch.name}: ${fmtTok(ch.numTokens ?? ch.tokens)}`).join(' · ');
      return `
        <div class="bd-row"${childTip ? ` title="${esc(childTip)}"` : ''}>
          <span class="bd-swatch ${isRound ? 'rnd' : ''}" style="background:${col}"></span>
          <span class="bd-name" title="${esc(g.name)}">${esc(g.name)}</span>
          <span class="bd-tok">${fmtTok(tok)}</span>
          <span class="bd-pct">${gPct}%</span>
        </div>
      `;
    }).join('');

    const freePctStr = maxTok > 0 ? ((freeTok / maxTok) * 100).toFixed(1) : '0.0';
    const showWarnFoot = pct >= (data.contextWarnPct || 80);

    return `
      <div class="card">
        <div class="card-h">
          <span class="c-title" title="${esc(title)}">${esc(title)}</span>
          <span class="badge-mini ${compacted ? 'compacted' : tier.cls === 'danger' ? 'danger' : tier.cls === 'warn' || tier.cls === 'orange' ? 'warn' : ''}">
            ${compacted ? (cpIdx >= 0 ? `Checkpoint #${cpIdx + 1}` : '已压缩会话') : (model || '活跃上下文')}
          </span>
        </div>
        <div class="big-stat">
          <span class="big-num">${fmtTok(used)}</span>
          <span class="big-of">/ ${fmtTok(maxTok)} tokens</span>
          <span class="big-pct ${tier.cls}">${pct.toFixed(1)}%</span>
        </div>
        <div class="seg-bar">${barHTML}</div>
        ${showWarnFoot ? `<div class="q-foot"><span>${esc(model || '当前模型')}</span><span style="color:var(--agy-warn)">建议 /compact</span></div>` : ''}
      </div>

      <div class="card">
        <div class="card-h">
          <span class="c-title">/context 窗口点阵</span>
        </div>
        <div class="dot-grid">${dotsHTML}</div>
      </div>

      <div class="card">
        <div class="card-h"><span class="c-title">分类构成明细</span></div>
        <div class="bd-list">
          ${groupsHTML || `<div class="empty-msg">暂无细分项</div>`}
          <div class="bd-row">
            <span class="bd-swatch free"></span>
            <span class="bd-name" style="color:var(--agy-muted)">可用剩余空间</span>
            <span class="bd-tok" style="color:var(--agy-muted)">${fmtTok(freeTok)}</span>
            <span class="bd-pct">${freePctStr}%</span>
          </div>
        </div>
      </div>
    `;
  }

  function renderTurnsTab(data, activeCtx, liveResp) {
    const det = liveResp?.detail;
    const turns = (det && Array.isArray(det.turns) && det.turns.length > 0)
      ? det.turns
      : (Array.isArray(data.recentTurns) ? data.recentTurns : []);
    const lastTPS = Number(det?.lastTps || activeCtx?.lastTps || data.lastTps || 0);
    const lastTTFT = Number(det?.lastTtftMs || activeCtx?.lastTtftMs || data.lastTtftMs || 0);
    const prefill = Number(det?.lastPrefillTps || activeCtx?.lastPrefillTps || data.lastPrefillTps || 0);
    const cachePct = Number(det?.cacheHitPct ?? activeCtx?.cacheHitRate ?? data.cacheHitRate ?? 0);
    const maxTok = Number(liveResp?.context?.maxContextTokens ?? activeCtx?.max ?? data.contextMax ?? 0);

    const kpis = `
      <div class="kpi4">
        <div class="kpi-tile"><span class="k">输出速度</span><span class="v">${lastTPS > 0 ? lastTPS.toFixed(1) + ' t/s' : '--'}</span></div>
        <div class="kpi-tile"><span class="k">首字延迟</span><span class="v">${fmtMs(lastTTFT)}</span></div>
        <div class="kpi-tile"><span class="k">Prefill 吞吐</span><span class="v">${prefill > 0 ? fmtTok(prefill) + '/s' : '--'}</span></div>
        <div class="kpi-tile"><span class="k">缓存命中</span><span class="v">${Math.round(cachePct)}%</span></div>
      </div>
    `;

    if (turns.length === 0) {
      return kpis + `<div class="empty-msg">当前会话暂无多轮调用记录</div>`;
    }

    const curve = buildGrowthCurveSVG(turns, maxTok);
    const listTurns = turns.slice().reverse().slice(0, 14);
    const accordions = listTurns.map((t) => {
      const deltaStr = t.contextDelta > 0 ? `+${fmtTok(t.contextDelta)}` : (t.contextDelta < 0 ? fmtTok(t.contextDelta) : '0');
      const tools = Array.isArray(t.tools) ? t.tools : [];
      const toolBrief = tools[0] ? `${tools[0].name}${tools[0].summary ? ' · ' + tools[0].summary : ''}` : '';
      const snip = t.userSnippet || toolBrief || t.agentSnippet || t.modelName || t.model || `第 ${t.turn} 轮`;
      return `
        <details class="turn-item" data-turn="${t.turn}">
          <summary class="turn-sum">
            <span class="turn-idx">#${t.turn}${t.compacted ? ' ⚡' : ''}</span>
            <span class="turn-snip" title="${esc(snip)}">${esc(snip)}</span>
            <span class="turn-delta">${esc(deltaStr)}</span>
            <span class="turn-tps">${t.tps > 0 ? t.tps.toFixed(1) + ' t/s' : '--'}</span>
            <span class="turn-delta">${fmtMs(t.ttftMs)}</span>
          </summary>
          <div class="turn-detail">
            <div class="td-grid">
              <div class="td-kv"><span class="k">提示 / 缓存</span><span class="v">${fmtTok(t.inputTokens ?? 0)} + ${fmtTok(t.cacheTokens ?? 0)}</span></div>
              <div class="td-kv"><span class="k">输出 (思/答)</span><span class="v">${fmtTok(t.thinkingTokens || 0)} / ${fmtTok(t.responseTokens || t.outputTokens || 0)}</span></div>
              <div class="td-kv"><span class="k">TTFT / 流式</span><span class="v">${fmtMs(t.ttftMs)} / ${fmtMs(t.streamMs)}</span></div>
              <div class="td-kv"><span class="k">Prefill / 命中</span><span class="v">${t.prefillTps > 0 ? fmtTok(t.prefillTps) + '/s' : '--'} · ${Math.round(t.cacheHitPct || 0)}%</span></div>
            </div>
            ${tools.length > 0 ? tools.slice(0, 5).map((tl) => `
              <div class="tool-row">
                <span class="mono" style="color:var(--agy-fg-strong)">${esc(tl.name)}</span>
                <span style="flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--agy-muted)">${esc(tl.summary || '')}</span>
                <span class="mono">${tl.tokens > 0 ? fmtTok(tl.tokens) : ''}</span>
              </div>
            `).join('') : ''}
          </div>
        </details>
      `;
    }).join('');

    return `
      ${kpis}
      ${curve ? `
        <div class="card">
          <div class="card-h">
            <span class="c-title">上下文增长曲线（轮次 × Prompt）</span>
          </div>
          ${curve}
        </div>
      ` : ''}
      <div class="card">
        <div class="card-h">
          <span class="c-title">每轮调用审计（${turns.length}）</span>
        </div>
        <div class="turn-list">${accordions}</div>
      </div>
    `;
  }

  function renderQuotaTab(data) {
    const buckets = Array.isArray(data.buckets) ? data.buckets : [];
    const models = Array.isArray(data.models) ? data.models : [];
    const accts = Array.isArray(data.accounts) ? data.accounts : [];

    let out = '';
    if (data.burnHint) {
      out += `<div class="alert-strip"><span>⚡</span><span>${esc(data.burnHint)}</span></div>`;
    }
    if (buckets.length > 0) {
      out += buckets.map((b) => {
        const pct = Math.max(0, Math.min(100, Number(b.pct || 0)));
        const tier = quotaColorTier(pct);
        const pctStr = pct <= 0 ? '已耗尽 0%' : `${pct >= 99.95 ? '100' : pct.toFixed(1).replace(/\.0$/, '')}%`;
        const resetTxt = zhResetLeft(b.resetLeft);
        const sparkColor = tier.cls === 'danger' ? 'var(--agy-danger)' : tier.cls === 'warn' ? 'var(--agy-warn)' : 'var(--agy-ok)';
        const spark = buildSparklineSVG(b.trend, sparkColor);
        return `
          <div class="q-card">
            <div class="q-top">
              <span class="q-name">${esc(b.name)}</span>
              ${spark}
              <span class="q-pct ${tier.cls}">${esc(pctStr)}</span>
            </div>
            <div class="bar-track"><i class="bar-fill ${tier.cls}" style="width:${pct}%"></i></div>
            <div class="q-foot">
              <span>${esc(resetTxt || (data.cachedQuota ? '离线快照' : '--'))}</span>
              ${(b.burnHint || b.burnPctPerHour > 0) ? `<span style="${b.exhaustsBeforeReset ? 'color:var(--agy-warn)' : ''}">${esc(b.burnHint || `速率 ${b.burnPctPerHour.toFixed(1)}%/h`)}</span>` : ''}
            </div>
          </div>
        `;
      }).join('');
    } else {
      out += `<div class="empty-msg">暂无配额池数据</div>`;
    }

    if (accts.length > 1) {
      out += `
        <div class="card">
          <div class="card-h"><span class="c-title">多账号余量总览</span></div>
          <div class="acct-chips">
            ${accts.map((a) => {
              const name = (a.email || '').split('@')[0] || a.email;
              const p = a.tightestPct >= 0 ? `${Math.round(a.tightestPct)}%` : '--';
              return `<span class="acct-chip ${a.active ? 'on' : ''}" title="${esc(a.email)} (${esc(a.planName || '')})">${esc(name)} · <b>${p}</b></span>`;
            }).join('')}
          </div>
        </div>
      `;
    }

    if (models.length > 0) {
      out += `
        <details class="turn-item">
          <summary class="turn-sum" style="grid-template-columns:1fr auto">
            <span style="font-weight:600;color:var(--agy-muted)">按模型配额明细（${models.length}）</span>
            <span style="font-size:10.5px;color:var(--agy-muted)">▾</span>
          </summary>
          <div class="turn-detail">
            ${models.map((m) => `
              <div class="td-kv">
                <span class="k" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(m.label)}</span>
                <span class="v">${Math.round(Number(m.pct || 0))}%${m.resetLeft ? ` · ${esc(zhResetLeft(m.resetLeft))}` : ''}</span>
              </div>
            `).join('')}
          </div>
        </details>
      `;
    }

    if (data.colabLabel) {
      out += `
        <div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;${data.colabBurning ? 'border-color:rgba(245,158,11,0.35)' : ''}">
          <span class="c-title">Colab 云算力</span>
          <span class="mono" style="font-weight:600;color:${data.colabBurning ? 'var(--agy-warn)' : 'var(--agy-fg-strong)'}">${esc(data.colabLabel)}</span>
        </div>
      `;
    }
    return out;
  }

  function renderHeatmapTab(data) {
    const costLbl = String(data.todayCostLabel || '$0.00');
    const savedLbl = String(data.cacheSavedLabel || '$0.00');
    const hm = Array.isArray(data.heatmap) ? data.heatmap : [];

    let colsHTML = '';
    for (let c = 0; c < hm.length; c += 7) {
      const slice = hm.slice(c, c + 7);
      const cells = slice.map((d) => {
        const tip = `${d.date} · ${d.calls} 次调用 · ${fmtTok(d.tokens)} tok · ${(data.currencySymbol || '$')}${Number(d.usd || 0).toFixed(2)}`;
        return `<span class="hm-cell" data-l="${d.level || 0}" data-tip="${esc(tip)}" title="${esc(tip)}"></span>`;
      }).join('');
      colsHTML += `<div class="hm-col">${cells}</div>`;
    }

    return `
      <div class="kpi4">
        <div class="kpi-tile"><span class="k">今日账单</span><span class="v">${esc(costLbl)}</span></div>
        <div class="kpi-tile"><span class="k">今日 Token</span><span class="v">${fmtTok(data.todayTokens)}</span></div>
        <div class="kpi-tile"><span class="k">缓存命中</span><span class="v">${Math.round(Number(data.cacheHitRate || 0))}%</span></div>
        <div class="kpi-tile"><span class="k">缓存省下</span><span class="v">${esc(savedLbl)}</span></div>
      </div>
      <div class="card">
        <div class="card-h">
          <span class="c-title">近 12 周活跃热力图</span>
          <span id="hm-hover-lbl" style="font-size:10.5px;color:var(--agy-muted)"></span>
        </div>
        ${colsHTML ? `<div class="hm-wrap">${colsHTML}</div>` : `<div class="empty-msg">暂无历史活跃记录</div>`}
      </div>
    `;
  }

  function renderPopover() {
    if (!latestData) return;
    const ov = ensureOverlay();
    const s = ov.shadowRoot;
    const data = latestData;

    s.getElementById('pop-badge').textContent = zhBadge(data.badge);
    const stTag = s.getElementById('pop-state');
    stTag.className = 'state-tag ' + zhStateClass(data.state);
    s.getElementById('pop-state-txt').textContent = zhStateText(data.state, data.stateLabel);
    const plan = zhPlan(data.planName);
    s.getElementById('pop-acct').textContent = [data.account || '未登录', plan].filter(Boolean).join(' · ');
    s.getElementById('pop-console').href = String(data.hubUrl || 'http://127.0.0.1:3778');

    s.querySelectorAll('.tab[data-tab]').forEach((btn) => {
      btn.classList.toggle('on', btn.getAttribute('data-tab') === activeTab);
    });

    const cid = popoverCascadeId || data.activeCascadeId || '';
    const activeCtx = (cid && data.cascadeContexts?.[cid]) || null;
    const liveResp = cid ? convCache.get(cid) : null;

    let bodyHTML = '';
    if (activeTab === 'context') bodyHTML = renderContextTab(data, activeCtx, liveResp);
    else if (activeTab === 'turns') bodyHTML = renderTurnsTab(data, activeCtx, liveResp);
    else if (activeTab === 'quota') bodyHTML = renderQuotaTab(data);
    else bodyHTML = renderHeatmapTab(data);

    const bodyEl = s.getElementById('pop-body');
    const key = activeTab + '|' + cid + '|' + bodyHTML;
    if (key !== lastPopBodyKey) {
      const openTurns = new Set();
      bodyEl.querySelectorAll('details[data-turn][open]').forEach((d) => openTurns.add(d.getAttribute('data-turn')));
      lastPopBodyKey = key;
      bodyEl.innerHTML = bodyHTML;
      if (openTurns.size > 0) {
        bodyEl.querySelectorAll('details[data-turn]').forEach((d) => {
          if (openTurns.has(d.getAttribute('data-turn'))) d.open = true;
        });
      }
    }
    if (popoverOpen) {
      const ov = document.getElementById(OVERLAY_ID);
      if (ov && ov.shadowRoot) localizeShadowTree(ov.shadowRoot, resolveHudLang(latestData));
      positionPopoverOverlay();
    }
  }

  // ---- Top Header Native Dock (antigravity-context-viewer style) ----
  function findTitlebarAndToolbar() {
    if (typeof document.querySelector !== 'function') return null;
    const titlebar = document.querySelector('[class*="@container/titlebar"]');
    if (!titlebar) return null;
    const toolbar = titlebar.querySelector?.('[data-testid="pane-titlebar-toolbar"]')
      || titlebar.querySelector?.(':scope > div.shrink-0')
      || titlebar.lastElementChild;
    if (!toolbar || toolbar === titlebar.firstElementChild) return { titlebar, container: titlebar };
    return { titlebar, container: toolbar };
  }

  function ensureTopbarHost(posName) {
    let host = document.getElementById(HOST_ID);
    if (!host || !host.shadowRoot || host.dataset.agymateHudV !== '3') {
      if (host) host.remove();
      const created = createShadowHost('div', HOST_ID, TOPBAR_CSS);
      host = created.host;
      host.dataset.agymateHudV = '3';
      host.setAttribute('translate', 'no');
      host.classList.add('notranslate');

      const tb = document.createElement('div');
      tb.className = 'tb';
      tb.innerHTML = `
        <button type="button" class="unit" id="u-inst" title="实例状态 · 点击查看账号与配额">
          <span class="dot" id="tb-dot"></span>
          <span class="inst-lbl" id="tb-badge">主窗口</span>
        </button>
        <span class="div" id="d-1"></span>
        <button type="button" class="unit" id="u-quota" title="模型配额与消耗趋势">
          <svg class="ring-svg" viewBox="0 0 18 18" aria-hidden="true">
            <circle class="ring-track" cx="9" cy="9" r="${RING_R}" fill="none" stroke-width="2"></circle>
            <circle class="ring-fill" id="tb-q-ring" cx="9" cy="9" r="${RING_R}" fill="none" stroke-width="2" stroke-linecap="round" transform="rotate(-90 9 9)"></circle>
          </svg>
          <span class="lbl" id="tb-q-lbl">5小时</span>
          <span class="val" id="tb-q-val">--</span>
          <span class="alert-pill" id="tb-q-alert"></span>
        </button>
        <button type="button" class="unit" id="u-ctx" title="对话上下文水位与 /context 明细">
          <svg class="ring-svg" viewBox="0 0 18 18" aria-hidden="true">
            <circle class="ring-track" cx="9" cy="9" r="${RING_R}" fill="none" stroke-width="2"></circle>
            <circle class="ring-fill" id="tb-c-ring" cx="9" cy="9" r="${RING_R}" fill="none" stroke-width="2" stroke-linecap="round" transform="rotate(-90 9 9)"></circle>
          </svg>
          <span class="lbl">上下文</span>
          <span class="val" id="tb-c-val">--</span>
        </button>
        <span class="div" id="d-2"></span>
        <button type="button" class="unit" id="u-speed" title="生成速度与每轮遥测">
          <span class="bolt" aria-hidden="true">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><path d="M13 2 4 14h7l-1 8 10-12h-7l0-8z"/></svg>
          </span>
          <span class="val" id="tb-tps">-- tok/s</span>
        </button>
        <button type="button" class="unit" id="u-cost" title="今日等效账单与活跃热力图">
          <span class="val" id="tb-cost">$0.00</span>
        </button>
        <button type="button" class="unit icon-btn" id="u-console" title="打开 AgyMate 控制台">
          <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
            <path d="M6.5 3.5H3.5v9h9V9.5"></path>
            <path d="M9.5 2.5h4v4"></path>
            <path d="M7.5 8.5l6-6"></path>
          </svg>
        </button>
      `;
      created.shadow.appendChild(tb);

      const bindUnit = (id, tab, tipBuilder) => {
        const el = created.shadow.getElementById(id);
        el.addEventListener('click', (e) => {
          e.stopPropagation();
          if (id === 'u-console') {
            window.open(latestData?.hubUrl || 'http://127.0.0.1:3778', '_blank', 'noopener,noreferrer');
            return;
          }
          openPopover(el, tab, 'down', getUrlCascadeId());
        });
        el.addEventListener('mouseenter', () => {
          if (popoverOpen || !latestData) return;
          if (tipTimer) clearTimeout(tipTimer);
          const info = tipBuilder();
          if (info) showTooltip(el, info.title, info.tag, info.hero, info.cls, info.sub);
        });
        el.addEventListener('mouseleave', () => {
          if (tipTimer) clearTimeout(tipTimer);
          tipTimer = setTimeout(() => hideTooltip(false), 90);
        });
      };

      bindUnit('u-inst', 'quota', () => ({
        title: `${zhBadge(latestData.badge)} · ${latestData.account || '未登录'}`,
        tag: zhPlan(latestData.planName) || zhStateText(latestData.state, latestData.stateLabel),
        hero: `状态：${zhStateText(latestData.state, latestData.stateLabel)}`,
        cls: '',
        sub: latestData.colabLabel ? `Colab：${latestData.colabLabel}` : '',
      }));
      bindUnit('u-quota', 'quota', () => {
        const q = pickCapsuleQuota(latestData);
        const lines = (latestData.buckets || []).slice(0, 3).map((b) => `${b.group || ''} ${zhShortWindow(b.name, b.window)} ${Math.round(b.pct)}%`).join(' · ');
        return {
          title: '模型配额余量',
          tag: latestData.cachedQuota ? '缓存' : '实时',
          hero: q ? `${q.label} 剩余 ${Math.round(q.pct)}%` : '暂无配额数据',
          cls: q && q.pct <= 10 ? 'danger' : (q && q.pct <= 25 ? 'warn' : ''),
          sub: latestData.burnHint || lines || '',
        };
      });
      bindUnit('u-ctx', 'context', () => {
        const cid = getUrlCascadeId() || latestData.activeCascadeId;
        const c = (cid && latestData.cascadeContexts?.[cid]) || null;
        const used = c ? c.used : latestData.contextUsed;
        const max = c ? c.max : latestData.contextMax;
        const pct = c ? c.pct : latestData.contextPct;
        const model = c?.model || latestData.activeModel || '当前模型';
        const compacted = Boolean(c?.compacted ?? latestData.compacted);
        const tier = contextColorTier(pct, latestData.contextWarnPct, compacted);
        return {
          title: `上下文窗口 · ${model}`,
          tag: compacted ? '已压缩' : '',
          hero: max > 0 ? `${Math.round(pct)}% · ${fmtTok(used)} / ${fmtTok(max)}` : '当前会话空闲',
          cls: tier.cls === 'orange' ? 'warn' : tier.cls,
          sub: max > 0 ? `缓存命中 ${Math.round(c?.cacheHitRate ?? latestData.cacheHitRate ?? 0)}%` : '',
        };
      });
      bindUnit('u-speed', 'turns', () => {
        const cid = getUrlCascadeId() || latestData.activeCascadeId;
        const c = (cid && latestData.cascadeContexts?.[cid]) || null;
        const tps = Number(c?.lastTps || latestData.lastTps || 0);
        const ttft = Number(c?.lastTtftMs || latestData.lastTtftMs || 0);
        const pref = Number(c?.lastPrefillTps || latestData.lastPrefillTps || 0);
        return {
          title: '最近生成遥测',
          tag: latestData.activeModel || '',
          hero: tps > 0 ? `⚡ ${tps.toFixed(1)} tok/s · 首字 ${fmtMs(ttft)}` : '暂无生成速度记录',
          cls: '',
          sub: pref > 0 ? `Prefill 吞吐 ${fmtTok(pref)}/s` : '',
        };
      });
      bindUnit('u-cost', 'heatmap', () => ({
        title: '今日 API 等效用量',
        tag: `${fmtTok(latestData.todayTokens)} tok`,
        hero: `${latestData.todayCostLabel || '$0.00'}${latestData.cacheSavedLabel ? ` · 缓存省 ${latestData.cacheSavedLabel}` : ''}`,
        cls: '',
        sub: `缓存命中率 ${Math.round(latestData.cacheHitRate || 0)}%`,
      }));
      bindUnit('u-console', '', () => ({
        title: 'AgyMate 控制台',
        tag: latestData.hubUrl || '127.0.0.1:3778',
        hero: '打开完整 Web 控制台',
        cls: '',
        sub: '',
      }));
    }

    dockTopbarHost(host, posName);
    return host;
  }

  function dockTopbarHost(host, posName) {
    const loc = findTitlebarAndToolbar();
    if (loc && loc.container) {
      try {
        if (host.parentElement !== loc.container) {
          if (loc.container !== loc.titlebar && loc.container.firstChild) {
            loc.container.insertBefore(host, loc.container.firstChild);
          } else {
            loc.container.appendChild(host);
          }
        }
        host.setAttribute('data-dock', 'flow');
        host.style.left = '';
        host.style.top = '';
        host.style.right = '';
        host.style.bottom = '';
        updateTopbarResponsiveLayout(host, loc.titlebar, loc.container);
        if (resizeObserver) {
          try {
            resizeObserver.disconnect();
            resizeObserver.observe(loc.titlebar);
          } catch {}
        }
        return;
      } catch {}
    }

    // Graceful fallback when header titlebar is absent (home/minimal view)
    const fallbackParent = document.body || document.documentElement;
    if (host.parentElement !== fallbackParent) fallbackParent.appendChild(host);
    host.setAttribute('data-dock', 'fixed');
    host.setAttribute('data-layout', 'compact');
    const norm = normalizePos(posName);
    host.style.top = norm.startsWith('bottom') ? 'auto' : '8px';
    host.style.bottom = norm.startsWith('bottom') ? '12px' : 'auto';
    host.style.left = norm.endsWith('left') ? '14px' : 'auto';
    host.style.right = norm.endsWith('left') ? 'auto' : '16px';
  }

  function updateTopbarResponsiveLayout(host, titlebar, toolbar) {
    if (!host || !titlebar) return;
    try {
      const tbRect = titlebar.getBoundingClientRect();
      const firstChild = titlebar.firstElementChild;
      const leftWidth = (firstChild && firstChild !== toolbar) ? Math.min(260, firstChild.getBoundingClientRect().width) : 160;
      const nativeRightBtns = toolbar ? Array.from(toolbar.children).filter((ch) => ch !== host)
        .reduce((acc, el) => acc + (el.getBoundingClientRect?.().width || 0), 0) : 160;
      const avail = Math.max(0, tbRect.width - leftWidth - nativeRightBtns - 24);
      let mode = 'wide';
      if (avail < 135) mode = 'ring-only';
      else if (avail < 230) mode = 'minimal';
      else if (avail < 330) mode = 'compact';
      if (host.getAttribute('data-layout') !== mode) {
        host.setAttribute('data-layout', mode);
      }
    } catch {}
  }

  // ---- Composer Inline Ring & Pulse Capsule (brien9 + antigravity-pulse style) ----
  function syncComposerCapsules(data) {
    if (typeof document.querySelectorAll !== 'function') return;
    const footers = Array.from(document.querySelectorAll('.agy-composer-footer, [class*="agy-composer-footer"]'));
    const activeSet = new Set();

    for (const footer of footers) {
      const leftSlot = footer.querySelector(':scope > div.flex.min-w-0.flex-1') || footer.firstElementChild;
      if (!leftSlot) continue;

      let cid = resolveComposerPaneCascadeId(footer);
      const urlCid = getUrlCascadeId();
      const hasRenderedTurns = typeof document.querySelector === 'function'
        && Boolean(document.querySelector('[data-testid="user-input-step"], [data-testid="turn-cards-container"] [data-testid*="step"], [data-testid="turn-cards-container"] [data-testid*="turn"]'));
      const onHomeEmptyScreen = !urlCid && !hasRenderedTurns;
      let ctx = null;
      if (cid && cid !== '__draft__') {
        ctx = data.cascadeContexts?.[cid] || (cid === data.activeCascadeId ? {
          cascadeId: cid,
          used: data.contextUsed,
          max: data.contextMax,
          pct: data.contextPct,
          model: data.activeModel,
          compacted: data.compacted,
          lastTps: data.lastTps,
          lastTtftMs: data.lastTtftMs,
          cacheHitRate: data.cacheHitRate,
        } : null);
      } else if (!cid && !onHomeEmptyScreen && data.contextMax > 0) {
        // Fallback when React Fiber cascadeId is unavailable inside an active conversation pane
        if (footers.length === 1) {
          cid = data.activeCascadeId || '';
          ctx = {
            cascadeId: cid,
            used: data.contextUsed,
            max: data.contextMax,
            pct: data.contextPct,
            model: data.activeModel,
            compacted: data.compacted,
            lastTps: data.lastTps,
            lastTtftMs: data.lastTtftMs,
            cacheHitRate: data.cacheHitRate,
          };
        }
      }

      let ch = Array.from(leftSlot.children).find((el) => el.hasAttribute?.(COMPOSER_ATTR));
      if (!ctx || Number(ctx.max || 0) <= 0 || Number(ctx.used || 0) <= 0 || cid === '__draft__') {
        if (ch) ch.style.display = 'none';
        continue;
      }

      if (!ch) {
        const created = createShadowHost('div', '', COMPOSER_CSS);
        ch = created.host;
        ch.setAttribute(COMPOSER_ATTR, '1');
        ch.setAttribute('translate', 'no');
        ch.classList.add('notranslate');
        ch.innerHTML = '';
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'cap';
        btn.id = 'cap';
        btn.innerHTML = `
          <svg class="cap-ring" viewBox="0 0 18 18" aria-hidden="true">
            <circle class="ring-track" cx="9" cy="9" r="${RING_R}" fill="none" stroke-width="2"></circle>
            <circle class="ring-fill" id="c-ring" cx="9" cy="9" r="${RING_R}" fill="none" stroke-width="2" stroke-linecap="round" transform="rotate(-90 9 9)"></circle>
          </svg>
          <span class="cap-pct" id="c-pct">0%</span>
          <span class="cap-sep" id="c-sep" style="display:none"></span>
          <span class="cap-tps" id="c-tps" style="display:none"></span>
        `;
        created.shadow.appendChild(btn);

        btn.addEventListener('click', (e) => {
          e.stopPropagation();
          const curCid = ch.getAttribute('data-cid') || '';
          openPopover(ch, 'context', 'up', curCid);
        });
        btn.addEventListener('mouseenter', () => {
          if (popoverOpen || !latestData) return;
          if (tipTimer) clearTimeout(tipTimer);
          const curCid = ch.getAttribute('data-cid') || '';
          const cur = (curCid && latestData.cascadeContexts?.[curCid]) || {
            used: latestData.contextUsed,
            max: latestData.contextMax,
            pct: latestData.contextPct,
            model: latestData.activeModel,
            compacted: latestData.compacted,
            lastTps: latestData.lastTps,
            lastTtftMs: latestData.lastTtftMs,
            cacheHitRate: latestData.cacheHitRate,
          };
          const pctVal = Number(cur.pct || 0);
          const tier = contextColorTier(pctVal, latestData.contextWarnPct, cur.compacted);
          const tps = Number(cur.lastTps || 0);
          showTooltip(
            ch,
            `上下文 · ${cur.model || latestData.activeModel || '当前模型'}`,
            cur.compacted ? '已压缩' : '',
            `${pctVal.toFixed(1)}% · ${fmtTok(cur.used)} / ${fmtTok(cur.max)}`,
            tier.cls === 'orange' ? 'warn' : tier.cls,
            `${tps > 0 ? `⚡ ${tps.toFixed(1)} tok/s · ` : ''}${cur.lastTtftMs ? `TTFT ${fmtMs(cur.lastTtftMs)} · ` : ''}缓存 ${Math.round(cur.cacheHitRate ?? latestData.cacheHitRate ?? 0)}%`
          );
        });
        btn.addEventListener('mouseleave', () => {
          if (tipTimer) clearTimeout(tipTimer);
          tipTimer = setTimeout(() => hideTooltip(false), 90);
        });

        try { leftSlot.appendChild(ch); } catch { continue; }
      } else if (ch.nextElementSibling) {
        try { leftSlot.appendChild(ch); } catch {}
      }

      activeSet.add(ch);
      ch.style.display = 'inline-flex';
      ch.setAttribute('data-cid', cid || '');

      const sr = ch.shadowRoot;
      const pctVal = Math.max(0, Math.min(100, Number(ctx.pct || 0)));
      const tier = contextColorTier(pctVal, data.contextWarnPct, ctx.compacted);
      setRingStroke(sr.getElementById('c-ring'), pctVal, tier.stroke);
      const pctEl = sr.getElementById('c-pct');
      pctEl.textContent = `${Math.round(pctVal)}%`;
      pctEl.className = 'cap-pct' + (tier.cls ? ' ' + tier.cls : '');

      const tps = Number(ctx.lastTps || data.lastTps || 0);
      const sepEl = sr.getElementById('c-sep');
      const tpsEl = sr.getElementById('c-tps');
      if (tps > 0) {
        sepEl.style.display = 'inline-block';
        tpsEl.style.display = 'inline-block';
        tpsEl.textContent = `${tps.toFixed(1)} t/s`;
        tpsEl.className = 'cap-tps' + (data.state === 'running' ? ' live' : '');
      } else {
        sepEl.style.display = 'none';
        tpsEl.style.display = 'none';
      }
    }

    // Remove stale orphaned composer hosts no longer inside a valid footer
    document.querySelectorAll(`[${COMPOSER_ATTR}]`).forEach((el) => {
      if (!activeSet.has(el) && !el.closest?.('.agy-composer-footer')) el.remove();
    });
  }

  // ---- Update Topbar Widgets ----
  function renderTopbar(host, data) {
    const s = host.shadowRoot;
    if (!s) return;

    const stCls = zhStateClass(data.state);
    const dot = s.getElementById('tb-dot');
    dot.className = 'dot' + (stCls ? ' ' + stCls : '');
    s.getElementById('tb-badge').textContent = zhBadge(data.badge);

    // Quota ring
    const q = pickCapsuleQuota(data);
    const qValEl = s.getElementById('tb-q-val');
    const qLblEl = s.getElementById('tb-q-lbl');
    const qAlert = s.getElementById('tb-q-alert');
    if (q) {
      const qTier = quotaColorTier(q.pct);
      setRingStroke(s.getElementById('tb-q-ring'), q.pct, qTier.stroke);
      qLblEl.textContent = q.label;
      qValEl.textContent = `${Math.round(Math.max(0, Math.min(100, q.pct)))}%`;
      qValEl.className = 'val' + (qTier.cls === 'warn' || qTier.cls === 'danger' ? ' ' + qTier.cls : '');
      qAlert.classList.toggle('on', q.exhausting);
    } else {
      setRingStroke(s.getElementById('tb-q-ring'), 0, 'var(--agy-muted)');
      qLblEl.textContent = '配额';
      qValEl.textContent = '--';
      qValEl.className = 'val';
      qAlert.classList.remove('on');
    }

    // Context ring (isolated per URL conversation)
    const cid = getUrlCascadeId() || data.activeCascadeId;
    const c = (cid && data.cascadeContexts?.[cid]) || null;
    const cMax = Number(c?.max ?? data.contextMax ?? 0);
    const cPct = cMax > 0 ? Math.max(0, Math.min(100, Number(c?.pct ?? data.contextPct ?? 0))) : 0;
    const cComp = Boolean(c?.compacted ?? data.compacted);
    const cValEl = s.getElementById('tb-c-val');
    if (cMax > 0) {
      const cTier = contextColorTier(cPct, data.contextWarnPct, cComp);
      setRingStroke(s.getElementById('tb-c-ring'), cPct, cTier.stroke);
      cValEl.textContent = `${Math.round(cPct)}%`;
      cValEl.className = 'val' + (cTier.cls === 'orange' ? ' warn' : cTier.cls ? ' ' + cTier.cls : '');
    } else {
      setRingStroke(s.getElementById('tb-c-ring'), 0, 'var(--agy-muted)');
      cValEl.textContent = '空闲';
      cValEl.className = 'val';
    }

    // Generation Speed & Cost
    const tps = Number(c?.lastTps || data.lastTps || 0);
    s.getElementById('tb-tps').textContent = tps > 0 ? `${tps.toFixed(1)} tok/s` : '-- tok/s';
    s.getElementById('tb-cost').textContent = String(data.todayCostLabel || '$0.00');
  }

  // ---- Global Observers (DOM self-healing, ResizeObserver, Theme sync, Outside Click) ----
  function rebindDetachedAnchor() {
    if (!popoverOpen || !popoverAnchorEl || popoverAnchorEl.isConnected) return;
    if (popoverAnchorEl.hasAttribute?.(COMPOSER_ATTR)) {
      const cid = popoverAnchorEl.getAttribute('data-cid') || popoverCascadeId || '';
      const replacement = Array.from(document.querySelectorAll(`[${COMPOSER_ATTR}]`))
        .find((el) => el.isConnected && el.style.display !== 'none' && (!cid || el.getAttribute('data-cid') === cid));
      if (replacement) {
        popoverAnchorEl = replacement;
        return;
      }
      const fallback = document.getElementById(HOST_ID)?.shadowRoot?.getElementById('u-ctx');
      if (fallback && fallback.isConnected) popoverAnchorEl = fallback;
      return;
    }
    if (popoverAnchorEl.id) {
      const replacement = document.getElementById(HOST_ID)?.shadowRoot?.getElementById(popoverAnchorEl.id);
      if (replacement && replacement.isConnected) popoverAnchorEl = replacement;
    }
  }

  function scheduleRelayout() {
    if (relayoutRAF) return;
    relayoutRAF = requestAnimationFrame(() => {
      relayoutRAF = 0;
      applyThemeToAllHosts();
      if (!latestData) return;
      const host = document.getElementById(HOST_ID);
      if (host) {
        dockTopbarHost(host, latestData.position);
        renderTopbar(host, latestData);
      }
      syncComposerCapsules(latestData);
      if (popoverOpen) {
        rebindDetachedAnchor();
        positionPopoverOverlay();
      }
    });
  }

  function ensureGlobalListeners() {
    if (abortCtrl) return;
    abortCtrl = new AbortController();
    const sig = abortCtrl.signal;

    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => scheduleRelayout());
    }
    if (typeof MutationObserver !== 'undefined') {
      themeObserver = new MutationObserver(() => applyThemeToAllHosts());
      themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['style', 'class', 'data-theme'] });

      let lastDomRelayoutAt = 0;
      domObserver = new MutationObserver(() => {
        const host = document.getElementById(HOST_ID);
        const loc = findTitlebarAndToolbar();
        const needUrgentDock = !host || !host.isConnected || (loc && loc.container && host.parentElement !== loc.container);
        if (needUrgentDock) {
          if (domRelayoutTimer) {
            clearTimeout(domRelayoutTimer);
            domRelayoutTimer = null;
          }
          lastDomRelayoutAt = Date.now();
          scheduleRelayout();
          return;
        }
        const now = Date.now();
        if (now - lastDomRelayoutAt >= 280) {
          if (domRelayoutTimer) {
            clearTimeout(domRelayoutTimer);
            domRelayoutTimer = null;
          }
          lastDomRelayoutAt = now;
          scheduleRelayout();
        } else if (!domRelayoutTimer) {
          // Ensure React remount bursts heal cleanly after throttling
          domRelayoutTimer = setTimeout(() => {
            domRelayoutTimer = null;
            lastDomRelayoutAt = Date.now();
            scheduleRelayout();
          }, 160);
        }
      });
      if (document.body) {
        domObserver.observe(document.body, { childList: true, subtree: true });
      }
    }

    window.addEventListener('resize', () => scheduleRelayout(), { signal: sig });
    document.addEventListener('pointerdown', (e) => {
      if (!popoverOpen) return;
      const path = typeof e.composedPath === 'function' ? e.composedPath() : [];
      const ov = document.getElementById(OVERLAY_ID);
      const top = document.getElementById(HOST_ID);
      if ((ov && path.includes(ov)) || (top && path.includes(top))) return;
      for (const ch of document.querySelectorAll(`[${COMPOSER_ATTR}]`)) {
        if (path.includes(ch)) return;
      }
      popoverOpen = false;
      syncOverlayVisibility();
    }, { capture: true, signal: sig });

    window.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && popoverOpen) {
        popoverOpen = false;
        syncOverlayVisibility();
      }
    }, { signal: sig });
  }

  // ---- Lightweight HUD i18n (zh / en driven by data.lang) ----
  const HUD_EN_EXACT = {
    '上下文': 'Context',
    '轮次': 'Turns',
    '配额': 'Quota',
    '热力图': 'Heatmap',
    '主窗口': 'Main',
    '运行中': 'Running',
    '待确认': 'Waiting',
    '空闲': 'Idle',
    '离线': 'Offline',
    '今日': 'Today',
    '缓存命中': 'Cache Hit',
    '缓存节省': 'Cache Saved',
    '打开控制台': 'Open Dashboard',
    '归位': 'Reset Pos',
    '已压缩': 'Compacted',
    '即将重置': 'Resetting soon',
    '暂不适用': 'N/A',
    '当前窗口暂不适用': 'N/A in current window',
    '受上级窗口限制，暂不适用': 'Limited by parent window',
    '系统与工具': 'System & Tools',
    '规则/技能/MCP': 'Rules / Skills / MCP',
    '对话消息': 'Messages',
    '历史上下文': 'History',
    '工作区上下文': 'Workspace',
    '工具定义': 'Tool Defs',
    '工具输出': 'Tool Output',
    '模型与思考': 'Model & Thinking',
    '用户与对话': 'User & Dialog',
    '未分类项': 'Other',
    '其他内容': 'Other',
    '其他开销': 'Overhead',
    '暂无配额数据': 'No quota data',
    '暂无轮次记录': 'No turn history',
    '暂无热力图数据': 'No activity data',
  };

  function resolveHudLang(data) {
    const pref = String(data?.lang || 'auto').toLowerCase().trim();
    if (pref === 'en' || pref.startsWith('en-')) return 'en';
    if (pref === 'zh' || pref.startsWith('zh-')) return 'zh';
    const nav = (typeof navigator !== 'undefined' && navigator && navigator.language ? String(navigator.language) : 'zh').toLowerCase();
    return nav.startsWith('zh') ? 'zh' : 'en';
  }

  function translateHudText(raw) {
    if (!raw || !/[\u4e00-\u9fff]/.test(raw)) return raw;
    const trimmed = raw.trim();
    let out = HUD_EN_EXACT[trimmed];
    if (!out) {
      out = trimmed
        .replace(/5小时/g, '5h')
        .replace(/周配额/g, 'Weekly')
        .replace(/日配额/g, 'Daily')
        .replace(/月配额/g, 'Monthly')
        .replace(/Ultra 套餐/g, 'Ultra')
        .replace(/Pro 套餐/g, 'Pro')
        .replace(/团队套餐/g, 'Team')
        .replace(/免费版/g, 'Free')
        .replace(/台实例/g, ' VMs')
        .replace(/空闲/g, 'Idle')
        .replace(/运行中/g, 'Running')
        .replace(/待确认/g, 'Waiting')
        .replace(/离线/g, 'Offline')
        .replace(/小时/g, 'h')
        .replace(/分钟/g, 'm')
        .replace(/分/g, 'm')
        .replace(/天/g, 'd')
        .replace(/后重置/g, ' reset')
        .replace(/已耗尽/g, ' exhausted')
        .replace(/预计/g, 'est. ')
        .replace(/耗尽/g, ' left')
        .replace(/今日 /g, 'Today ')
        .replace(/约 /g, '~');
    }
    const lead = raw.match(/^\s*/)[0];
    const trail = raw.match(/\s*$/)[0];
    return lead + out + trail;
  }

  function localizeShadowTree(root, lang) {
    if (!root || lang !== 'en' || typeof document.createTreeWalker !== 'function' || typeof NodeFilter === 'undefined') return;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
    let n;
    while ((n = walker.nextNode())) {
      if (n.nodeType === 3) {
        const p = n.parentElement;
        if (p && (p.tagName === 'STYLE' || p.tagName === 'SCRIPT')) continue;
        const next = translateHudText(n.nodeValue || '');
        if (next !== n.nodeValue) n.nodeValue = next;
      } else if (n.nodeType === 1 && n.hasAttribute('title')) {
        const cur = n.getAttribute('title') || '';
        const next = translateHudText(cur);
        if (next !== cur) n.setAttribute('title', next);
      }
    }
  }

  // ---- Public CDP Entrypoints ----
  window.__agymateHudUpdate = function(data) {
    if (!data) return;
    latestData = data;
    const hudLang = resolveHudLang(data);
    ensureGlobalListeners();
    applyThemeToAllHosts();
    const topHost = ensureTopbarHost(data.position);
    renderTopbar(topHost, data);
    if (topHost && topHost.shadowRoot) localizeShadowTree(topHost.shadowRoot, hudLang);
    syncComposerCapsules(data);
    if (hudLang === 'en') {
      for (const ch of document.querySelectorAll(`[${COMPOSER_ATTR}]`)) {
        if (ch.shadowRoot) localizeShadowTree(ch.shadowRoot, hudLang);
      }
    }
    applyThemeToAllHosts();
    if (popoverOpen) {
      rebindDetachedAnchor();
      renderPopover();
      const ov = document.getElementById(OVERLAY_ID);
      if (ov && ov.shadowRoot) localizeShadowTree(ov.shadowRoot, hudLang);
    }
  };

  window.__agymateHudRemove = function() {
    if (tipTimer) {
      clearTimeout(tipTimer);
      tipTimer = null;
    }
    if (domRelayoutTimer) {
      clearTimeout(domRelayoutTimer);
      domRelayoutTimer = null;
    }
    if (relayoutRAF) {
      cancelAnimationFrame(relayoutRAF);
      relayoutRAF = 0;
    }
    popoverOpen = false;
    latestData = null;
    lastPopBodyKey = '';
    try { domObserver?.disconnect(); } catch {}
    try { themeObserver?.disconnect(); } catch {}
    try { resizeObserver?.disconnect(); } catch {}
    domObserver = themeObserver = resizeObserver = null;
    try { abortCtrl?.abort(); } catch {}
    abortCtrl = null;
    try {
      document.getElementById(HOST_ID)?.remove();
      document.getElementById(OVERLAY_ID)?.remove();
      document.querySelectorAll?.(`[${COMPOSER_ATTR}]`).forEach((el) => el.remove());
    } catch {}
    try { delete window.__agymateHudFp; } catch { window.__agymateHudFp = ''; }
  };
})();
