package cdp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

func TestFormatResetCountdown(t *testing.T) {
	if got := FormatResetCountdown(0); got != "" {
		t.Fatalf("FormatResetCountdown(0) = %q, want empty", got)
	}
	if got := FormatResetCountdown(-10); got != "" {
		t.Fatalf("FormatResetCountdown(-10) = %q, want empty", got)
	}
	if got := FormatResetCountdown(time.Now().Unix() - 5); got != "即将重置" {
		t.Fatalf("expired reset = %q, want 即将重置", got)
	}

	cases := []struct {
		diff int64
		want string
	}{
		{diff: 0, want: "即将重置"},
		{diff: -60, want: "即将重置"},
		{diff: 35, want: "不足1分钟"},
		{diff: 15 * 60, want: "15分钟"},
		{diff: 3 * 3600, want: "3小时"},
		{diff: 3600 + 17*60, want: "1小时17分"},
		{diff: 2 * 86400, want: "2天"},
		{diff: 6*86400 + 20*3600 + 12*60, want: "6天20小时"},
	}
	for _, tc := range cases {
		if got := formatResetDiff(tc.diff); got != tc.want {
			t.Errorf("formatResetDiff(%d) = %q, want %q", tc.diff, got, tc.want)
		}
	}
}

func TestEmbeddedHUDScriptContract(t *testing.T) {
	mustContain := []string{
		"agymate-hud-host",
		"agymate-overlay-host",
		"data-agymate-composer-host",
		"agymate_hud_pos_v1",
		"data-dock",
		"data-theme",
		"data-layout",
		"上下文 /context",
		"每轮遥测",
		"额度与趋势",
		"用量与热力",
		"dot-grid",
		"curve-svg",
		"spark-svg",
		"hm-wrap",
		"turn-item",
		"控制台",
	}
	for _, token := range mustContain {
		if !strings.Contains(hudScript, token) {
			t.Errorf("embedded hud.js missing expected token %q", token)
		}
	}

	mustNotContain := []string{
		"Ctx --",
		"`Ctx ",
		"data.position || 'bottom-right'",
		">×<",
	}
	for _, bad := range mustNotContain {
		if strings.Contains(hudScript, bad) {
			t.Errorf("embedded hud.js still contains legacy token %q", bad)
		}
	}
}

func TestHUDScriptDOMBehaviorWithNode(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping JS DOM harness")
	}

	harness := `
const fs = require('fs');
const vm = require('vm');
const assert = require('assert');

const code = fs.readFileSync('hud.js', 'utf8');

const storage = new Map([
  ['agymate_hud_pos_v1', JSON.stringify({ left: 920, top: 760 })],
]);
const localStorage = {
  getItem: (k) => (storage.has(k) ? storage.get(k) : null),
  setItem: (k, v) => storage.set(k, String(v)),
  removeItem: (k) => storage.delete(k),
};

function makeEl(tag, ownerShadow) {
  const listeners = {};
  const classSet = new Set();
  const attrs = new Map();
  let _rect = { left: 1000, top: 6, right: 1200, bottom: 34, width: 200, height: 28 };
  const el = {
    tagName: String(tag || 'DIV').toUpperCase(),
    id: '',
    title: '',
    href: '',
    dataset: {},
    style: { cssText: '' },
    children: [],
    parentElement: null,
    parentNode: null,
    isConnected: true,
    shadowRoot: null,
    _html: '',
    _text: '',
    offsetWidth: 240,
    offsetHeight: 70,
    get firstElementChild() { return this.children[0] || null; },
    get lastElementChild() { return this.children[this.children.length - 1] || null; },
    get firstChild() { return this.children[0] || null; },
    get nextElementSibling() {
      if (!this.parentElement) return null;
      const arr = this.parentElement.children;
      const i = arr.indexOf(this);
      return i >= 0 && i + 1 < arr.length ? arr[i + 1] : null;
    },
    get className() { return Array.from(classSet).join(' '); },
    set className(v) {
      classSet.clear();
      String(v || '').split(/\s+/).filter(Boolean).forEach((c) => classSet.add(c));
    },
    classList: {
      add: (...cs) => cs.forEach((c) => c && classSet.add(c)),
      remove: (...cs) => cs.forEach((c) => classSet.delete(c)),
      toggle: (c, force) => {
        if (force === undefined) {
          if (classSet.has(c)) { classSet.delete(c); return false; }
          classSet.add(c); return true;
        }
        if (force) classSet.add(c); else classSet.delete(c);
        return Boolean(force);
      },
      contains: (c) => classSet.has(c),
    },
    setAttribute(k, v) {
      attrs.set(k, String(v));
      if (ownerShadow && ownerShadow._byAttr) {
        if (!ownerShadow._byAttr.has(k)) ownerShadow._byAttr.set(k, new Set());
        ownerShadow._byAttr.get(k).add(this);
      }
      if (!ownerShadow) {
        if (!document._byAttr.has(k)) document._byAttr.set(k, new Set());
        document._byAttr.get(k).add(this);
      }
    },
    getAttribute(k) { return attrs.has(k) ? attrs.get(k) : null; },
    hasAttribute(k) { return attrs.has(k); },
    get innerHTML() { return this._html; },
    set innerHTML(v) {
      this._html = String(v || '');
      if (ownerShadow) {
        const re = /<([a-zA-Z0-9-]+)([^>]*)>/g;
        let m;
        while ((m = re.exec(this._html)) !== null) {
          const rawAttrs = m[2];
          const idM = rawAttrs.match(/\bid="([^"]+)"/);
          const tabM = rawAttrs.match(/\bdata-tab="([^"]+)"/);
          const clsM = rawAttrs.match(/\bclass="([^"]+)"/);
          if (!idM && !tabM) continue;
          const id = idM ? idM[1] : '';
          let child = id && ownerShadow._byId.get(id);
          if (!child) {
            child = makeEl(m[1], ownerShadow);
            if (id) {
              child.id = id;
              ownerShadow._byId.set(id, child);
            }
            if (clsM) child.className = clsM[1];
            if (tabM) child.setAttribute('data-tab', tabM[1]);
            if (clsM && clsM[1].includes('tab')) ownerShadow._tabs.push(child);
            if (clsM && clsM[1].includes('unit')) ownerShadow._units.push(child);
          }
        }
      }
    },
    get textContent() { return this._text; },
    set textContent(v) { this._text = String(v ?? ''); },
    appendChild(ch) {
      if (ch.parentElement && ch.parentElement.children) {
        const idx = ch.parentElement.children.indexOf(ch);
        if (idx >= 0) ch.parentElement.children.splice(idx, 1);
      }
      ch.parentElement = this;
      ch.parentNode = this;
      ch.isConnected = true;
      this.children.push(ch);
      if (ch.id && !ownerShadow) document._byId.set(ch.id, ch);
      return ch;
    },
    insertBefore(ch, ref) {
      if (ch.parentElement && ch.parentElement.children) {
        const idx = ch.parentElement.children.indexOf(ch);
        if (idx >= 0) ch.parentElement.children.splice(idx, 1);
      }
      const ridx = this.children.indexOf(ref);
      if (ridx >= 0) this.children.splice(ridx, 0, ch);
      else this.children.push(ch);
      ch.parentElement = this;
      ch.parentNode = this;
      ch.isConnected = true;
      if (ch.id && !ownerShadow) document._byId.set(ch.id, ch);
      return ch;
    },
    contains(target) {
      let cur = target;
      while (cur) {
        if (cur === this) return true;
        cur = cur.parentElement;
      }
      return false;
    },
    remove() {
      this.isConnected = false;
      if (this.parentElement && this.parentElement.children) {
        const i = this.parentElement.children.indexOf(this);
        if (i >= 0) this.parentElement.children.splice(i, 1);
      }
      this.parentElement = null;
      if (document._byId.get(this.id) === this) document._byId.delete(this.id);
    },
    attachShadow() {
      const sr = {
        _byId: new Map(),
        _byAttr: new Map(),
        _tabs: [],
        _units: [],
        adoptedStyleSheets: null,
        appendChild: (ch) => { sr.root = ch; },
        getElementById: (id) => sr._byId.get(id) || null,
        querySelectorAll: (sel) => {
          if (sel === '.tab[data-tab]') return sr._tabs;
          if (sel === '.unit') return sr._units;
          return [];
        },
      };
      this.shadowRoot = sr;
      return sr;
    },
    setRect(r) { _rect = Object.assign({}, _rect, r); },
    getBoundingClientRect() { return _rect; },
    querySelector(sel) {
      if (sel === '[data-testid="pane-titlebar-toolbar"]') return this.children.find((c) => c.getAttribute('data-testid') === 'pane-titlebar-toolbar') || null;
      if (sel === ':scope > div.flex.min-w-0.flex-1') return this.children[0] || null;
      return null;
    },
    querySelectorAll() { return []; },
    closest(sel) {
      if (sel === '.tab[data-tab]' && this.getAttribute('data-tab')) return this;
      if (sel === '#pop-refresh' && this.id === 'pop-refresh') return this;
      if (sel === '#pop-sidecar' && this.id === 'pop-sidecar') return this;
      return null;
    },
    addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
    dispatch(type, extra = {}) {
      const ev = Object.assign({
        type, button: 0, target: this, stopPropagation() {}, composedPath: () => [this],
      }, extra);
      for (const fn of (listeners[type] || [])) fn(ev);
    },
  };
  return el;
}

const winListeners = {};
const docListeners = {};
let currentShadow = null;
let fakeTitlebar = null;
let fakeToolbar = null;
let fakeFooter = null;
let mockBg = 'rgb(255, 255, 255)';
let mockColorScheme = 'light';

const document = {
  _byId: new Map(),
  _byAttr: new Map(),
  documentElement: {
    getAttribute: () => '',
    appendChild(el) {
      el.isConnected = true;
      if (el.id) document._byId.set(el.id, el);
      return el;
    },
  },
  body: null,
  getElementById: (id) => document._byId.get(id) || null,
  querySelector(sel) {
    if (sel === '[class*="@container/titlebar"]') return fakeTitlebar;
    return null;
  },
  querySelectorAll(sel) {
    const walk = (node, out = []) => {
      if (!node || !Array.isArray(node.children)) return out;
      for (const ch of node.children) {
        out.push(ch);
        walk(ch, out);
      }
      return out;
    };
    const roots = [document.documentElement, document.body, fakeTitlebar, fakeFooter].filter(Boolean);
    const all = [];
    for (const r of roots) walk(r, all);
    if (sel === '[data-agymate-composer-host]') {
      return all.filter((n) => n.hasAttribute && n.hasAttribute('data-agymate-composer-host'));
    }
    if (sel.includes('agy-composer-footer')) return fakeFooter ? [fakeFooter] : [];
    return [];
  },
  createElement(tag) {
    const el = makeEl(tag, currentShadow);
    const origAttach = el.attachShadow.bind(el);
    el.attachShadow = (opts) => {
      const sr = origAttach(opts);
      currentShadow = sr;
      return sr;
    };
    return el;
  },
  addEventListener(type, fn, opts) {
    if (opts && opts.signal) {
      opts.signal.addEventListener('abort', () => {
        docListeners[type] = (docListeners[type] || []).filter((f) => f !== fn);
      });
    }
    (docListeners[type] = docListeners[type] || []).push(fn);
  },
  dispatch(type, ev = {}) { for (const fn of (docListeners[type] || [])) fn(ev); },
};
document.body = makeEl('body', null);

// Build realistic Antigravity titlebar + composer footer
fakeTitlebar = makeEl('div', null);
fakeTitlebar.setRect({ left: 260, top: 0, right: 1352, bottom: 40, width: 1092, height: 40 });
const crumb = makeEl('div', null);
crumb.setRect({ left: 265, top: 6, right: 480, bottom: 34, width: 215, height: 28 });
fakeToolbar = makeEl('div', null);
fakeToolbar.setAttribute('data-testid', 'pane-titlebar-toolbar');
const nativeBtnCluster = makeEl('div', null);
nativeBtnCluster.setRect({ left: 1176, top: 6, right: 1352, bottom: 34, width: 176, height: 28 });
fakeToolbar.appendChild(nativeBtnCluster);
fakeTitlebar.appendChild(crumb);
fakeTitlebar.appendChild(fakeToolbar);

fakeFooter = makeEl('div', null);
fakeFooter.className = 'agy-composer-footer';
const footerLeft = makeEl('div', null);
footerLeft.className = 'flex min-w-0 flex-1 items-center';
const modelBtn = makeEl('button', null);
footerLeft.appendChild(modelBtn);
fakeFooter.appendChild(footerLeft);

const window = {
  innerWidth: 1352,
  innerHeight: 798,
  localStorage,
  document,
  AbortController,
  setTimeout,
  clearTimeout,
  requestAnimationFrame: (fn) => setTimeout(fn, 0),
  cancelAnimationFrame: clearTimeout,
  getComputedStyle: (el) => {
    if (el === document.documentElement) return { colorScheme: mockColorScheme, backgroundColor: mockBg };
    return { backgroundColor: mockBg };
  },
  location: { pathname: '/c/11111111-2222-3333-4444-555555555555' },
  addEventListener(type, fn, opts) {
    if (opts && opts.signal) {
      opts.signal.addEventListener('abort', () => {
        winListeners[type] = (winListeners[type] || []).filter((f) => f !== fn);
      });
    }
    (winListeners[type] = winListeners[type] || []).push(fn);
  },
  dispatch(type, ev = {}) { for (const fn of (winListeners[type] || [])) fn(ev); },
};

const ctx = vm.createContext({
  window, document, localStorage, AbortController,
  getComputedStyle: window.getComputedStyle,
  location: window.location,
  setTimeout, clearTimeout,
  requestAnimationFrame: window.requestAnimationFrame,
  cancelAnimationFrame: window.cancelAnimationFrame,
  Math, Date, Number, String, Boolean, Array, JSON, RegExp, Set, Map,
});

vm.runInContext(code, ctx);

// 1. Legacy v1 bottom-right cache purged
assert.strictEqual(storage.has('agymate_hud_pos_v1'), false, 'v1 pos cache should be purged');

// 2. Push rich HUDData -> mounts in titlebar (data-dock="flow"), light theme, wide layout, composer inline capsule
const sampleData = {
  instanceId: 'p4',
  badge: 'P4',
  account: 'user@example.com',
  planName: 'Google AI Ultra',
  state: 'running',
  stateLabel: '运行中',
  position: 'top-right',
  hubUrl: 'http://127.0.0.1:3778',
  activeCascadeId: '11111111-2222-3333-4444-555555555555',
  activeTitle: 'UI 集大成改造',
  activeModel: 'Gemini 4 Carbon (High)',
  burnHint: 'Claude 5小时限额预计 2小时耗尽',
  contextUsed: 180000,
  contextMax: 512000,
  contextPct: 35.15,
  contextWarnPct: 80,
  compacted: false,
  checkpointIndex: -1,
  quotaLabel: '5小时',
  quotaPct: 74.0,
  todayCostLabel: '¥12.80',
  todayTokens: 425000,
  cacheHitRate: 86.4,
  cacheSavedLabel: '¥36.20',
  lastTps: 64.5,
  lastTtftMs: 1200,
  lastPrefillTps: 28000,
  colabLabel: '2台实例 · 1.96/小时 · 1800 CCU',
  colabBurning: true,
  buckets: [
    { id: 'gemini-5h', group: 'Gemini', name: 'Gemini · 5小时限额', window: '5h', pct: 74.0, resetLeft: '1小时17分', trend: [100, 92, 85, 74] },
    { id: 'gemini-weekly', group: 'Gemini', name: 'Gemini · 周配额', window: 'weekly', pct: 93.4, resetLeft: '6天20小时', trend: [98, 95, 93.4] },
  ],
  breakdown: [
    { key: 'system', name: '系统提示词', tokens: 17500 },
    { key: 'tools', name: '工具定义', tokens: 7600 },
    { key: 'user', name: '用户输入', tokens: 50000 },
    { key: 'agent', name: '模型回复', tokens: 80000 },
    { key: 'tool_output', name: '工具结果', tokens: 24900 },
  ],
  cascadeContexts: {
    '11111111-2222-3333-4444-555555555555': {
      cascadeId: '11111111-2222-3333-4444-555555555555',
      title: 'UI 集大成改造',
      model: 'Gemini 4 Carbon (High)',
      used: 180000,
      max: 512000,
      pct: 35.15,
      lastTps: 64.5,
      lastTtftMs: 1200,
      cacheHitRate: 86.4,
    },
  },
  recentTurns: [
    { turn: 1, promptTokens: 90000, contextDelta: 90000, outputTokens: 200, tps: 50.0, ttftMs: 1100, userSnippet: '分析架构' },
    { turn: 2, promptTokens: 180000, contextDelta: 90000, outputTokens: 320, tps: 64.5, ttftMs: 1200, userSnippet: '升级 HUD' },
  ],
  heatmap: Array.from({ length: 84 }, (_, idx) => ({ date: '2026-11-01', tokens: idx * 1000, calls: 2, usd: 0.5, level: idx % 5 })),
};

window.__agymateHudUpdate(sampleData);

const host = document.getElementById('agymate-hud-host');
assert(host, 'topbar host mounted');
assert.strictEqual(host.getAttribute('data-dock'), 'flow', 'docks inside native titlebar toolbar');
assert.strictEqual(host.getAttribute('data-theme'), 'light', 'matches Antigravity light mode');
assert.strictEqual(host.getAttribute('data-layout'), 'wide', 'wide layout on desktop viewport');
assert.strictEqual(host.parentElement, fakeToolbar, 'inserted into pane-titlebar-toolbar');

const sr = host.shadowRoot;
assert.strictEqual(sr.getElementById('tb-badge').textContent, 'P4');
assert.strictEqual(sr.getElementById('tb-c-val').textContent, '35%');
assert.strictEqual(sr.getElementById('tb-q-val').textContent, '74%');
assert.strictEqual(sr.getElementById('tb-tps').textContent, '64.5 tok/s');

// Composer ring mounted inside footerLeft beside Turbo
const composerHosts = document.querySelectorAll('[data-agymate-composer-host]');
assert.strictEqual(composerHosts.length, 1, 'composer inline ring mounted');
assert.strictEqual(composerHosts[0].getAttribute('data-theme'), 'light');
assert.strictEqual(composerHosts[0].shadowRoot.getElementById('c-pct').textContent, '35%');
assert.strictEqual(composerHosts[0].shadowRoot.getElementById('c-tps').textContent, '64.5 t/s');

// 3. Click Topbar Context unit -> opens overlay portal Popover directly on /context tab with 100-dot grid
sr.getElementById('u-ctx').dispatch('click');
const overlay = document.getElementById('agymate-overlay-host');
assert(overlay, 'overlay portal created');
const ovSr = overlay.shadowRoot;
const pop = ovSr.getElementById('pop');
assert(pop.classList.contains('open'), 'popover opens on click');
assert(ovSr.getElementById('pop-body').innerHTML.includes('dot-grid'), 'renders /context 100-dot grid');

// Switch to 每轮遥测 tab -> renders SVG growth curve & turn items
const turnsTab = ovSr.querySelectorAll('.tab[data-tab]').find((t) => t.getAttribute('data-tab') === 'turns');
pop.dispatch('click', { target: turnsTab });
assert(ovSr.getElementById('pop-body').innerHTML.includes('curve-svg'), 'renders growth curve SVG');
assert(ovSr.getElementById('pop-body').innerHTML.includes('turn-item'), 'renders per-turn accordion');

// Switch to 额度与趋势 tab -> renders quota sparklines & Colab
const quotaTab = ovSr.querySelectorAll('.tab[data-tab]').find((t) => t.getAttribute('data-tab') === 'quota');
pop.dispatch('click', { target: quotaTab });
assert(ovSr.getElementById('pop-body').innerHTML.includes('spark-svg'), 'renders quota trend sparkline');
assert(ovSr.getElementById('pop-body').innerHTML.includes('Colab 云算力'), 'renders colab status');

// Switch to 用量与热力 tab -> renders 12-week heatmap
const hmTab = ovSr.querySelectorAll('.tab[data-tab]').find((t) => t.getAttribute('data-tab') === 'heatmap');
pop.dispatch('click', { target: hmTab });
assert(ovSr.getElementById('pop-body').innerHTML.includes('hm-wrap'), 'renders 12-week heatmap grid');

// Escape closes popover
window.dispatch('keydown', { key: 'Escape' });
assert(!pop.classList.contains('open'), 'Escape closes popover');

// 4. Narrow titlebar collapses layout to ring-only
fakeTitlebar.setRect({ left: 0, top: 0, right: 420, bottom: 40, width: 420, height: 40 });
window.dispatch('resize', {});

// 5. Fallback fixed mode when titlebar absent, dark theme adaptation, and clean removal
fakeTitlebar = null;
mockColorScheme = 'dark';
mockBg = 'rgb(24, 24, 27)';
window.__agymateHudUpdate(sampleData);
assert.strictEqual(host.getAttribute('data-dock'), 'fixed', 'falls back to fixed dock when no titlebar');
assert.strictEqual(host.getAttribute('data-theme'), 'dark', 'adapts to dark theme dynamically');

window.__agymateHudRemove();
assert.strictEqual(document.getElementById('agymate-hud-host'), null, 'topbar host removed');
assert.strictEqual(document.getElementById('agymate-overlay-host'), null, 'overlay host removed');
assert.strictEqual(document.querySelectorAll('[data-agymate-composer-host]').length, 0, 'composer hosts removed');
`

	cmd := exec.Command(nodeBin, "-e", harness)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node HUD harness failed: %v\n%s", err, string(out))
	}
}

func TestWatcherScanSyncAndProbe(t *testing.T) {
	var mu sync.Mutex
	evalLog := make([]string, 0, 8)
	zhActive := false
	hudActive := false

	mux := http.NewServeMux()
	var srvPort int

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		wsURL := "ws://127.0.0.1:" + strconv.Itoa(srvPort) + "/devtools/page/tgt-1"
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":                   "tgt-1",
				"type":                 "page",
				"title":                "Antigravity",
				"url":                  "https://127.0.0.1:59000/",
				"webSocketDebuggerUrl": wsURL,
			},
			{
				"id":                   "tgt-worker",
				"type":                 "service_worker",
				"title":                "sw",
				"url":                  "https://127.0.0.1:59000/sw.js",
				"webSocketDebuggerUrl": wsURL,
			},
		})
	})

	mux.HandleFunc("/devtools/page/tgt-1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(16 * 1024 * 1024)

		_, msg, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var req struct {
			ID     int `json:"id"`
			Params struct {
				Expression string `json:"expression"`
			} `json:"params"`
		}
		_ = json.Unmarshal(msg, &req)

		mu.Lock()
		expr := req.Params.Expression
		evalLog = append(evalLog, expr)
		var val any = true
		switch {
		case strings.Contains(expr, "__agyZhFp ="):
			zhActive = true
			val = "ok"
		case strings.Contains(expr, "return 'install';"):
			if hudActive {
				val = "ok"
			} else {
				val = "install"
			}
		case strings.Contains(expr, "__agymateHudFp ="):
			hudActive = true
			val = true
		case strings.Contains(expr, "__agymateHudRemove()"):
			hudActive = false
			val = nil
		case strings.Contains(expr, "Boolean(window.__agyZhFp)"):
			val = map[string]any{"zh": zhActive, "hud": hudActive}
		}
		mu.Unlock()

		resp, _ := json.Marshal(map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"result": map[string]any{
					"value": val,
				},
			},
		})
		_ = conn.Write(r.Context(), websocket.MessageText, resp)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	srvPort = ln.Addr().(*net.TCPAddr).Port
	srv := httptest.NewUnstartedServer(mux)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	tmpDir := t.TempDir()
	zhScriptFile := filepath.Join(tmpDir, "inject.js")
	if err := os.WriteFile(zhScriptFile, []byte("window.__zhLoaded = 1;"), 0o644); err != nil {
		t.Fatalf("write zh script: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.CDP.Enabled = true
	cfg.CDP.ZhInject = true
	cfg.CDP.ZhScriptPath = zhScriptFile
	cfg.CDP.WindowHUD = true
	cfg.CDP.HUDPosition = "top-right"

	instList := []instances.Instance{
		{ID: "main", Label: "Antigravity", CDPPort: srvPort, Running: true},
	}

	w := NewWatcher()
	ctx := context.Background()

	targets := w.ScanTargets(ctx, instList)
	if len(targets) != 1 || targets[0].TargetID != "tgt-1" {
		t.Fatalf("ScanTargets = %+v, want single page target tgt-1", targets)
	}

	synced := w.SyncAll(ctx, instList, cfg, func(instID string) *HUDData {
		return &HUDData{
			Badge:       "主窗口",
			Account:     "dev@example.com",
			PlanName:    "Google AI Ultra",
			State:       "running",
			StateLabel:  "运行中",
			Position:    "top-right",
			ActiveTitle: "测试任务",
			BurnHint:    "速率平稳",
			Buckets: []HUDBucket{
				{Name: "5小时限额", Group: "Gemini", Window: "5h", Pct: 82.5, ResetLeft: "2小时10分"},
			},
		}
	}, false)

	if len(synced) != 1 || !synced[0].ZhInjected || !synced[0].HUDActive {
		t.Fatalf("SyncAll result = %+v, want ZhInjected=true and HUDActive=true", synced)
	}
	if len(w.LastStatus()) != 1 {
		t.Fatalf("LastStatus len = %d, want 1", len(w.LastStatus()))
	}

	probed := w.ProbeTargetStatus(ctx, instList)
	if len(probed) != 1 || !probed[0].ZhInjected || !probed[0].HUDActive {
		t.Fatalf("ProbeTargetStatus = %+v, want both active", probed)
	}

	// Second SyncAll should take lightweight HUD fast-path (no full ~60KB hud.js resend).
	mu.Lock()
	beforeFast := len(evalLog)
	mu.Unlock()
	syncedFast := w.SyncAll(ctx, instList, cfg, func(instID string) *HUDData {
		return &HUDData{Badge: "主窗口", Account: "dev@example.com", Position: "top-right"}
	}, false)
	if len(syncedFast) != 1 || !syncedFast[0].HUDActive {
		t.Fatalf("fast-path SyncAll result = %+v, want HUDActive=true", syncedFast)
	}
	mu.Lock()
	fastExprs := append([]string(nil), evalLog[beforeFast:]...)
	mu.Unlock()
	if len(fastExprs) != 1 || len(fastExprs[0]) > 4096 {
		t.Fatalf("expected single lightweight fast-path eval (<4KB), got count=%d firstLen=%d", len(fastExprs), func() int {
			if len(fastExprs) == 0 {
				return 0
			}
			return len(fastExprs[0])
		}())
	}

	// Excluding an instance via HUDInstances while WindowHUD remains true should still remove its HUD.
	cfg.CDP.HUDInstances = []string{"other-instance"}
	w.SyncAll(ctx, instList, cfg, func(instID string) *HUDData {
		return &HUDData{Badge: "主窗口", Account: "dev@example.com", Position: "top-right"}
	}, false)
	probedExcluded := w.ProbeTargetStatus(ctx, instList)
	if len(probedExcluded) != 1 || probedExcluded[0].HUDActive {
		t.Fatalf("after excluding via HUDInstances, ProbeTargetStatus = %+v, want HUDActive=false", probedExcluded)
	}

	// Re-enabling and then turning WindowHUD off should invoke __agymateHudRemove.
	cfg.CDP.HUDInstances = nil
	w.SyncAll(ctx, instList, cfg, func(instID string) *HUDData {
		return &HUDData{Badge: "主窗口", Account: "dev@example.com", Position: "top-right"}
	}, false)
	cfg.CDP.WindowHUD = false
	w.SyncAll(ctx, instList, cfg, nil, false)
	probedOff := w.ProbeTargetStatus(ctx, instList)
	if len(probedOff) != 1 || probedOff[0].HUDActive {
		t.Fatalf("after WindowHUD=false, ProbeTargetStatus = %+v, want HUDActive=false", probedOff)
	}

	// Syncing with no active instances should prune stale lastSeen entries.
	w.SyncAll(ctx, nil, cfg, nil, false)
	w.mu.Lock()
	staleCount := len(w.lastSeen)
	w.mu.Unlock()
	if staleCount != 0 {
		t.Fatalf("expected stale lastSeen entries pruned, got %d remaining", staleCount)
	}
}

func TestBuiltinZhScriptFallbackAndExternalOverride(t *testing.T) {
	if !HasBuiltinZhScript() {
		t.Fatal("expected built-in zh_inject.js to be embedded")
	}

	w := NewWatcher()
	// Empty or missing path falls back cleanly to built-in engine
	src, fp, err := w.loadZhScript("")
	if err != nil || src == "" || !strings.HasPrefix(fp, "agy-zh-builtin-") {
		t.Fatalf("expected built-in zh script fallback, got err=%v len=%d fp=%q", err, len(src), fp)
	}
	label, external := ResolveZhSource("/nonexistent/path/inject.js")
	if external || label != "builtin" {
		t.Fatalf("ResolveZhSource(missing) = (%q, %v), want (\"builtin\", false)", label, external)
	}

	// Existing custom override takes precedence
	customFile := filepath.Join(t.TempDir(), "custom-zh.js")
	if err := os.WriteFile(customFile, []byte("window.__customZh = true;"), 0o600); err != nil {
		t.Fatal(err)
	}
	cSrc, cFP, err := w.loadZhScript(customFile)
	if err != nil || cSrc != "window.__customZh = true;" || !strings.HasPrefix(cFP, "agy-zh-") || strings.HasPrefix(cFP, "agy-zh-builtin-") {
		t.Fatalf("expected external override script, got err=%v src=%q fp=%q", err, cSrc, cFP)
	}
	cLabel, cExt := ResolveZhSource(customFile)
	if !cExt || cLabel != customFile {
		t.Fatalf("ResolveZhSource(custom) = (%q, %v), want (%q, true)", cLabel, cExt, customFile)
	}
}
