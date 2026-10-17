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
    font-family: var(--vscode-font-family);
    font-size: 13px;
    line-height: 1.45;
    color: var(--vscode-foreground);
    background: var(--vscode-sideBar-background);
  }
  button, textarea { font: inherit; color: inherit; }
  #log { flex: 1; overflow: auto; padding: 14px 12px 8px; }
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
  .error { color: var(--vscode-errorForeground); }
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
  .dock { position: relative; padding: 0 8px 8px; }
  .composer {
    position: relative;
    border: 1px solid var(--vscode-focusBorder, #0078d4);
    border-radius: 12px;
    background: var(--vscode-input-background);
    padding: 6px 8px 4px;
  }
  textarea {
    display: block;
    width: 100%;
    box-sizing: border-box;
    min-height: 44px;
    max-height: 160px;
    resize: none;
    border: 0;
    outline: none;
    padding: 4px 4px 2px;
    background: transparent;
  }
  textarea::placeholder { opacity: .45; }
  .toolbar { display: flex; align-items: center; gap: 1px; min-height: 30px; }
  .toolbar .spacer { flex: 1; }
  .iconbtn, .chip, .auto {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border-radius: 7px;
    padding: 4px 6px;
    font-size: 12px;
    opacity: .9;
  }
  .iconbtn:hover, .chip:hover, .auto:hover { background: rgba(127,127,127,.16); }
  .iconbtn:disabled, .chip:disabled, .send:disabled { opacity: .4; cursor: default; }
  .iconbtn.on { color: var(--vscode-button-background, #0078d4); }
  .context {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 0 6px;
    font-size: 11px;
    color: #3dce86;
  }
  .auto.on { opacity: 1; }
  .send {
    width: 28px;
    height: 28px;
    margin-left: 4px;
    border: 0;
    border-radius: 8px;
    display: grid;
    place-items: center;
    cursor: pointer;
    color: var(--vscode-button-foreground, #fff);
    background: var(--vscode-button-background, #0e639c);
  }
  .menu {
    position: absolute;
    bottom: 40px;
    z-index: 4;
    min-width: 148px;
    padding: 4px;
    border-radius: 10px;
    border: 1px solid var(--vscode-menu-border, var(--vscode-widget-border, rgba(128,128,128,.4)));
    background: var(--vscode-menu-background, var(--vscode-editorWidget-background, #252526));
    box-shadow: 0 8px 24px rgba(0,0,0,.35);
  }
  .menu[hidden] { display: none; }
  .menu button {
    display: block;
    width: 100%;
    text-align: left;
    background: transparent;
    border: 0;
    color: inherit;
    border-radius: 6px;
    padding: 6px 8px;
    cursor: pointer;
  }
  .menu button:hover, .menu button[aria-checked="true"] {
    background: var(--vscode-list-hoverBackground, rgba(128,128,128,.18));
  }
  #model-menu { left: 28px; }
  #effort-menu { left: 108px; }
</style>
</head>
<body>
<div id="log"></div>
<button id="jump" type="button" hidden aria-label="Вниз">${svg("down")}</button>
<div class="dock">
  <form class="composer" id="composer">
    <textarea id="text" rows="2" placeholder="Message"></textarea>
    <div class="toolbar">
      <button class="iconbtn" id="plus" type="button" aria-label="Ещё">${svg("plus")}</button>
      <button class="iconbtn" id="mic" type="button" aria-label="Голос">${svg("mic")}</button>
      <button class="chip" id="model" type="button">Grok ${svg("chev")}</button>
      <button class="chip" id="effort" type="button">High ${svg("chev")}</button>
      <span class="context" id="context" title="Контекст">${svg("donut")}<span id="usage">0/—</span></span>
      <span class="spacer"></span>
      <button class="auto on" id="auto" type="button">${svg("bolt")} Auto accept</button>
      <button class="send" id="send" type="submit" aria-label="Отправить" disabled>${svg("send")}</button>
    </div>
    <div class="menu" id="plus-menu" hidden>
      <button type="button" data-act="new">New chat</button>
      <button type="button" data-act="file">Add file</button>
    </div>
    <div class="menu" id="model-menu" hidden>
      <button type="button" data-model="grok" aria-checked="true">Grok</button>
      <button type="button" data-model="claude">Claude</button>
      <button type="button" data-model="gpt">GPT</button>
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
    return `<svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true"><path d="M9 1.5 3.5 9H8l-1 5.5L12.5 7H8L9 1.5Z" fill="currentColor"/></svg>`;
  }
  if (name === "send") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 12.5V3.5M4.5 7 8 3.5 11.5 7" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
  }
  if (name === "down") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 6.5 8 10.5 12 6.5" stroke="currentColor" stroke-width="1.4" fill="none" stroke-linecap="round"/></svg>`;
  }
  if (name === "donut") {
    return `<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.5" stroke="currentColor" stroke-opacity=".28" fill="none" stroke-width="2"/><circle id="donut" cx="8" cy="8" r="5.5" stroke="currentColor" fill="none" stroke-width="2" stroke-linecap="round" transform="rotate(-90 8 8)" stroke-dasharray="0 34.56"/></svg>`;
  }
  return "";
}

module.exports = { chatPage };
