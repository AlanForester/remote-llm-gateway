const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

function chatPage() {
  const nonce = crypto.randomBytes(16).toString("hex");
  const script = fs.readFileSync(path.join(__dirname, "chat-ui.js"), "utf8");
  return `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}'">
<style>
  :root { color-scheme: dark; }
  html, body { height: 100%; }
  body {
    margin: 0;
    display: flex;
    flex-direction: column;
    font-family: var(--vscode-font-family, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
    font-size: 13px;
    line-height: 1.45;
    color: var(--vscode-foreground, #e6edf3);
    background: var(--vscode-sideBar-background, #0d1117);
    user-select: none;
  }
  button, textarea { font: inherit; color: inherit; }

  /* Top Header Bar */
  .topbar {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    min-height: 36px;
    box-sizing: border-box;
    border-bottom: 1px solid var(--vscode-widget-border, rgba(255,255,255,.07));
  }
  .session-title {
    font-size: 13px;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 60%;
  }
  .topbar-actions {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .topbtn {
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--vscode-foreground);
    opacity: .78;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    padding: 0;
  }
  .topbtn:hover {
    opacity: 1;
    background: rgba(127,127,127,.18);
  }
  .topbtn.active {
    color: #388bfd;
    opacity: 1;
  }

  /* Top Dropdown Menus */
  .top-menu {
    position: absolute;
    top: 38px;
    right: 10px;
    z-index: 10;
    min-width: 220px;
    max-width: 300px;
    padding: 6px;
    border-radius: 10px;
    border: 1px solid var(--vscode-menu-border, var(--vscode-widget-border, rgba(128,128,128,.35)));
    background: var(--vscode-menu-background, var(--vscode-editorWidget-background, #161b22));
    box-shadow: 0 10px 28px rgba(0,0,0,.45);
  }
  .top-menu[hidden] { display: none; }
  .menu-header {
    font-size: 11px;
    font-weight: 600;
    opacity: .6;
    padding: 4px 8px 6px;
    text-transform: uppercase;
    letter-spacing: .04em;
  }
  .history-list {
    max-height: 240px;
    overflow-y: auto;
  }
  .history-row {
    display: flex;
    align-items: center;
    gap: 4px;
    border-radius: 6px;
  }
  .history-row:hover {
    background: var(--vscode-list-hoverBackground, rgba(128,128,128,.16));
  }
  .history-item {
    flex: 1;
    text-align: left;
    background: transparent;
    border: 0;
    padding: 6px 8px;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
  }
  .history-del {
    background: transparent;
    border: 0;
    opacity: .55;
    cursor: pointer;
    padding: 4px 6px;
    border-radius: 4px;
    font-size: 12px;
  }
  .history-del:hover { opacity: 1; color: var(--vscode-errorForeground, #f85149); }
  .menu-empty {
    padding: 8px;
    font-size: 12px;
    opacity: .6;
  }
  .top-menu > button {
    display: block;
    width: 100%;
    text-align: left;
    background: transparent;
    border: 0;
    color: inherit;
    border-radius: 6px;
    padding: 6px 8px;
    font-size: 12px;
    cursor: pointer;
  }
  .top-menu > button:hover {
    background: var(--vscode-list-hoverBackground, rgba(128,128,128,.18));
  }

  /* Welcome / Empty State */
  .welcome {
    flex: 1;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding: 16px 12px 10px;
    overflow: hidden;
  }
  .welcome[hidden] { display: none; }
  .welcome-center {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 12px;
  }
  .welcome-logo {
    margin-bottom: 14px;
    color: var(--vscode-foreground, #e6edf3);
    opacity: .95;
  }
  .welcome-title {
    margin: 0 0 4px;
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }
  .welcome-sub {
    font-size: 12px;
    opacity: .55;
  }
  .tip-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 11px;
    border-radius: 9px;
    border: 1px solid var(--vscode-widget-border, rgba(255,255,255,.1));
    background: rgba(255,255,255,.03);
    font-size: 12px;
    line-height: 1.4;
  }
  .tip-banner[hidden] { display: none; }
  .tip-icon {
    display: inline-flex;
    color: #e3b341;
    flex-shrink: 0;
  }
  .tip-text {
    flex: 1;
    color: var(--vscode-foreground);
    opacity: .9;
  }
  .tip-link {
    background: transparent;
    border: 0;
    padding: 0;
    color: #388bfd;
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
  }
  .tip-close {
    background: transparent;
    border: 0;
    padding: 2px;
    opacity: .55;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
  }
  .tip-close:hover { opacity: 1; }

  /* Chat Log */
  #log {
    flex: 1;
    overflow: auto;
    padding: 14px 12px 8px;
    user-select: text;
  }
  #log[hidden] { display: none; }
  .msg { margin: 0 0 14px; }
  .you { display: flex; flex-direction: column; align-items: flex-end; }
  .bubble {
    max-width: 92%;
    padding: 8px 12px;
    border-radius: 14px;
    background: rgba(255,255,255,.06);
    background: color-mix(in srgb, var(--vscode-foreground) 8%, var(--vscode-sideBar-background));
    white-space: pre-wrap;
    word-break: break-word;
  }
  .meta {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: 4px;
    opacity: .55;
    font-size: 11px;
  }
  .you .meta { justify-content: flex-end; }
  .meta button, .iconbtn, .chip, .auto {
    background: transparent;
    border: 0;
    color: inherit;
    cursor: pointer;
    padding: 2px;
  }
  .agent-body p { margin: 0 0 8px; }
  .agent-body ul { margin: 0 0 8px; padding-left: 18px; }
  .agent-body li { margin: 2px 0; }
  .agent-body code, .agent-body pre {
    font-family: var(--vscode-editor-font-family, ui-monospace, monospace);
    font-size: 12px;
  }
  .agent-body code {
    padding: 0 4px;
    border-radius: 4px;
    background: rgba(127,127,127,.18);
    background: color-mix(in srgb, var(--vscode-foreground) 12%, transparent);
  }
  .agent-body pre {
    margin: 0 0 8px;
    padding: 8px 10px;
    border-radius: 8px;
    overflow: auto;
    background: var(--vscode-textCodeBlock-background, rgba(0,0,0,.28));
  }
  .agent-body pre code { padding: 0; background: transparent; }
  .agent-body a { color: var(--vscode-textLink-foreground, #4da3ff); }
  .thought, .tools, .card, .status, .error, .file {
    margin: 0 0 10px;
    font-size: 12px;
  }
  .thought, .tools, .card {
    border: 1px solid var(--vscode-widget-border, rgba(128,128,128,.28));
    border-radius: 10px;
    background: transparent;
  }
  .thought summary, .tools summary {
    cursor: pointer;
    padding: 7px 10px;
    opacity: .8;
  }
  .thought-body, .tool-list { padding: 0 10px 8px; white-space: pre-wrap; word-break: break-word; }
  .tool-row { padding: 2px 0; opacity: .8; }
  .status, .file { opacity: .7; }
  .error { color: var(--vscode-errorForeground, #f85149); }
  .wait { animation: pulse 1.4s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: .35; } }
  @media (prefers-reduced-motion: reduce) { .wait { animation: none; } }
  .card { padding: 8px 10px; }
  .card .actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .card .actions button {
    border-radius: 7px;
    border: 1px solid var(--vscode-widget-border, rgba(128,128,128,.35));
    background: var(--vscode-input-background);
    padding: 4px 8px;
    cursor: pointer;
  }
  #jump {
    position: absolute;
    right: 18px;
    bottom: 108px;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 1px solid var(--vscode-widget-border, rgba(128,128,128,.4));
    background: var(--vscode-editorWidget-background, var(--vscode-sideBar-background));
    color: inherit;
    cursor: pointer;
  }
  #jump[hidden] { display: none; }

  /* Composer Dock */
  .dock { position: relative; padding: 0 10px 10px; }
  .composer {
    position: relative;
    border: 1px solid var(--vscode-focusBorder, #1f6feb);
    border-radius: 10px;
    background: var(--vscode-input-background, #0d1117);
    padding: 8px 8px 6px;
  }
  textarea {
    display: block;
    width: 100%;
    box-sizing: border-box;
    min-height: 42px;
    max-height: 160px;
    resize: none;
    border: 0;
    outline: none;
    padding: 2px 4px 6px;
    background: transparent;
  }
  textarea::placeholder { opacity: .45; }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 2px;
    min-height: 28px;
    flex-wrap: wrap;
  }
  .toolbar .spacer { flex: 1; }
  .iconbtn, .chip, .auto {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border-radius: 6px;
    padding: 3px 6px;
    font-size: 12px;
    opacity: .88;
  }
  .model-chip {
    font-weight: 600;
    opacity: .95;
  }
  .effort-chip {
    font-size: 11px;
    opacity: .75;
    padding: 3px 4px;
  }
  .iconbtn:hover, .chip:hover, .auto:hover { background: rgba(127,127,127,.16); opacity: 1; }
  .iconbtn:disabled, .chip:disabled, .send:disabled { opacity: .4; cursor: default; }
  .iconbtn.on { color: #388bfd; opacity: 1; }
  .context {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 0 5px;
    font-size: 11.5px;
    color: var(--vscode-descriptionForeground, #8b949e);
  }
  .file-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 7px;
    border-radius: 6px;
    border: 1px solid var(--vscode-widget-border, rgba(255,255,255,.15));
    background: rgba(255,255,255,.03);
    font-size: 11.5px;
    color: var(--vscode-foreground);
    cursor: pointer;
    max-width: 140px;
  }
  .file-pill[hidden] { display: none; }
  .file-pill.off {
    opacity: .42;
    text-decoration: line-through;
  }
  .file-pill span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .auto {
    font-size: 11.5px;
    opacity: .65;
  }
  .auto.on { opacity: .95; }
  .send {
    width: 26px;
    height: 26px;
    margin-left: 4px;
    border: 0;
    border-radius: 7px;
    display: grid;
    place-items: center;
    cursor: pointer;
    color: var(--vscode-button-foreground, #fff);
    background: var(--vscode-button-background, #1f6feb);
  }
  .send.stopping {
    background: var(--vscode-errorForeground, #da3633);
  }
  .send span[hidden] { display: none; }
  .menu {
    position: absolute;
    bottom: 42px;
    z-index: 10;
    min-width: 168px;
    padding: 5px;
    border-radius: 10px;
    border: 1px solid var(--vscode-menu-border, var(--vscode-widget-border, rgba(128,128,128,.4)));
    background: var(--vscode-menu-background, var(--vscode-editorWidget-background, #161b22));
    box-shadow: 0 8px 24px rgba(0,0,0,.4);
  }
  .menu[hidden] { display: none; }
  .menu button {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    text-align: left;
    background: transparent;
    border: 0;
    color: inherit;
    border-radius: 6px;
    padding: 6px 8px;
    font-size: 12px;
    cursor: pointer;
  }
  .menu button:hover, .menu button[aria-checked="true"] {
    background: var(--vscode-list-hoverBackground, rgba(128,128,128,.18));
  }
  .menu-sep {
    height: 1px;
    margin: 4px 2px;
    background: var(--vscode-widget-border, rgba(128,128,128,.25));
  }
  #model-menu { left: 48px; }
  #effort-menu { left: 118px; }
</style>
</head>
<body>
<header class="topbar">
  <div class="session-title" id="session-title">New session</div>
  <div class="topbar-actions">
    <button class="topbtn active" id="remote-btn" type="button" title="Remote Gateway" aria-label="Remote Gateway">${svg("phone")}</button>
    <button class="topbtn" id="history-btn" type="button" title="Session history" aria-label="Session history">${svg("clock")}</button>
    <button class="topbtn" id="new-btn" type="button" title="New session" aria-label="New session">${svg("compose")}</button>
    <button class="topbtn" id="more-btn" type="button" title="More options" aria-label="More options">${svg("dots")}</button>
  </div>
  <div class="top-menu" id="history-menu" hidden>
    <div class="menu-header">Recent sessions</div>
    <div class="history-list" id="history-list">
      <div class="menu-empty">No saved sessions</div>
    </div>
  </div>
  <div class="top-menu" id="more-menu" hidden>
    <button type="button" data-more="token">Set gateway token</button>
    <button type="button" data-more="quota">Model &amp; quota</button>
    <button type="button" data-more="settings">Gateway settings</button>
  </div>
</header>

<div class="welcome" id="welcome">
  <div class="welcome-center">
    <div class="welcome-logo">${svg("cube")}</div>
    <h1 class="welcome-title">Remote LLM</h1>
    <div class="welcome-sub">previously Grok Build (Community)</div>
  </div>
  <div class="tip-banner" id="tip-banner">
    <span class="tip-icon">${svg("bulb")}</span>
    <span class="tip-text">Grok isn't your only agent. <button type="button" class="tip-link" id="connect-agents">Connect Codex or Claude Code</button> and pick one per conversation.</span>
    <button type="button" class="tip-close" id="tip-close" aria-label="Dismiss">${svg("close")}</button>
  </div>
</div>

<div id="log" hidden></div>
<button id="jump" type="button" hidden aria-label="Вниз">${svg("down")}</button>
<div class="dock">
  <form class="composer" id="composer">
    <textarea id="text" rows="2" placeholder="Ask Grok..."></textarea>
    <div class="toolbar">
      <button class="iconbtn" id="plus" type="button" aria-label="Ещё">${svg("plus")}</button>
      <button class="iconbtn" id="mic" type="button" aria-label="Голос">${svg("mic")}</button>
      <button class="chip model-chip" id="model" type="button">Grok 4.6</button>
      <button class="chip effort-chip" id="effort" type="button" title="Reasoning effort">High ${svg("chev")}</button>
      <span class="context" id="context" title="Context window">${svg("donut")}<span id="usage">0K/500K</span></span>
      <button class="file-pill" id="active-file" type="button" hidden title="Active file in editor">${svg("file")}<span id="active-file-name"></span></button>
      <span class="spacer"></span>
      <button class="auto on" id="auto" type="button">${svg("bolt")} Auto accept</button>
      <button class="send" id="send" type="submit" aria-label="Отправить" disabled>
        <span id="send-icon">${svg("send")}</span>
        <span id="stop-icon" hidden>${svg("stop")}</span>
      </button>
    </div>
    <div class="menu" id="plus-menu" hidden>
      <button type="button" data-act="new">New session</button>
      <button type="button" data-act="file">Attach file…</button>
      <button type="button" data-act="active">Toggle active file</button>
    </div>
    <div class="menu" id="model-menu" hidden>
      <button type="button" data-model="grok" aria-checked="true"><span>Grok 4.6</span><small>500K</small></button>
      <button type="button" data-model="claude"><span>Claude Code</span><small>200K</small></button>
      <button type="button" data-model="gpt"><span>Codex (GPT)</span><small>200K</small></button>
    </div>
    <div class="menu" id="effort-menu" hidden>
      <button type="button" data-effort="low">Low</button>
      <button type="button" data-effort="medium">Medium</button>
      <button type="button" data-effort="high" aria-checked="true">High</button>
      <button type="button" data-effort="xhigh">Extra high</button>
    </div>
  </form>
</div>
<script nonce="${nonce}">
${script}
</script>
</body>
</html>`;
}

function svg(name) {
  const common = 'width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"';
  if (name === "plus") {
    return `<svg ${common}><path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.4" fill="none" stroke-linecap="round"/></svg>`;
  }
  if (name === "mic") {
    return `<svg ${common}><rect x="6" y="2" width="4" height="7" rx="2" stroke="currentColor" fill="none"/><path d="M4.5 8a3.5 3.5 0 0 0 7 0M8 11.5V14" stroke="currentColor" fill="none" stroke-linecap="round"/></svg>`;
  }
  if (name === "chev") {
    return `<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 3.5 5 6.5 8 3.5" stroke="currentColor" fill="none" stroke-width="1.2" stroke-linecap="round"/></svg>`;
  }
  if (name === "bolt") {
    return `<svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true"><path d="M9 1.5 3.5 9H8l-1 5.5L12.5 7H8L9 1.5Z" fill="currentColor"/></svg>`;
  }
  if (name === "send") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 12.5V3.5M4.5 7 8 3.5 11.5 7" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
  }
  if (name === "stop") {
    return `<svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true"><rect x="4" y="4" width="8" height="8" rx="1.5" fill="currentColor"/></svg>`;
  }
  if (name === "down") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 6.5 8 10.5 12 6.5" stroke="currentColor" stroke-width="1.4" fill="none" stroke-linecap="round"/></svg>`;
  }
  if (name === "donut") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.5" stroke="currentColor" stroke-opacity=".28" fill="none" stroke-width="1.8"/><circle id="donut" cx="8" cy="8" r="5.5" stroke="currentColor" fill="none" stroke-width="1.8" stroke-linecap="round" transform="rotate(-90 8 8)" stroke-dasharray="0 34.56"/></svg>`;
  }
  if (name === "file") {
    return `<svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 2.5h5.2L12.5 5.8v7.7a1 1 0 0 1-1 1h-7.5a1 1 0 0 1-1-1v-10a1 1 0 0 1 1-1Z" stroke="currentColor" fill="none" stroke-width="1.2"/><path d="M9 2.5V6h3.5" stroke="currentColor" fill="none" stroke-width="1.2"/></svg>`;
  }
  if (name === "phone") {
    return `<svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><rect x="4.5" y="1.8" width="7" height="12.4" rx="1.6" stroke="currentColor" stroke-width="1.3" fill="none"/><circle cx="8" cy="11.8" r=".8" fill="currentColor"/></svg>`;
  }
  if (name === "clock") {
    return `<svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.8" stroke="currentColor" stroke-width="1.3" fill="none"/><path d="M8 4.8v3.4l2.3 1.4" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round"/></svg>`;
  }
  if (name === "compose") {
    return `<svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><path d="M7.5 3H3.8A1.3 1.3 0 0 0 2.5 4.3v7.9a1.3 1.3 0 0 0 1.3 1.3h7.9a1.3 1.3 0 0 0 1.3-1.3V8.5" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round"/><path d="M11.9 2.3a1.2 1.2 0 0 1 1.7 1.7L8.3 9.3l-2.3.5.5-2.3 5.4-5.2Z" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linejoin="round"/></svg>`;
  }
  if (name === "dots") {
    return `<svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"><circle cx="3.5" cy="8" r="1.2" fill="currentColor"/><circle cx="8" cy="8" r="1.2" fill="currentColor"/><circle cx="12.5" cy="8" r="1.2" fill="currentColor"/></svg>`;
  }
  if (name === "bulb") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 12h4M6.5 14h3M8 2a4.5 4.5 0 0 0-2.6 8.2c.4.3.6.7.6 1.2v.6h4v-.6c0-.5.2-.9.6-1.2A4.5 4.5 0 0 0 8 2Z" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round"/></svg>`;
  }
  if (name === "close") {
    return `<svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>`;
  }
  if (name === "cube") {
    return `<svg width="54" height="54" viewBox="0 0 64 64" aria-hidden="true">
      <polygon points="32,4 58,19 58,45 32,60 6,45 6,19" stroke="currentColor" stroke-width="3.2" fill="none" stroke-linejoin="round"/>
      <polyline points="6,19 32,34 58,19" stroke="currentColor" stroke-width="3.2" fill="none" stroke-linejoin="round"/>
      <line x1="32" y1="34" x2="32" y2="60" stroke="currentColor" stroke-width="3.2" stroke-linecap="round"/>
      <polygon points="32,14 48,23 32,32 16,23" fill="currentColor" fill-opacity=".18"/>
      <polygon points="15,28 28,36 28,51 15,43" fill="currentColor" fill-opacity=".28"/>
      <polygon points="49,28 36,36 36,51 49,43" fill="currentColor" fill-opacity=".12"/>
    </svg>`;
  }
  return "";
}

module.exports = { chatPage };

