function escapeHtml(value) {
  return String(value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function inlineMarkdown(value) {
  let text = escapeHtml(value);
  text = text.replace(/`([^`]+)`/g, "<code>$1</code>");
  text = text.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  text = text.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2">$1</a>');
  return text;
}

function renderMarkdown(src) {
  const fences = [];
  let text = String(src || "").replace(/```([\s\S]*?)```/g, (_, code) => {
    const body = escapeHtml(String(code).replace(/^\n/, "").replace(/\n$/, ""));
    fences.push("<pre><code>" + body + "</code></pre>");
    return "\u0000" + (fences.length - 1) + "\u0000";
  });
  const open = text.indexOf("```");
  if (open >= 0) {
    fences.push("<pre><code>" + escapeHtml(text.slice(open + 3)) + "</code></pre>");
    text = text.slice(0, open) + "\u0000" + (fences.length - 1) + "\u0000";
  }
  return text
    .split(/\n{2,}/)
    .map((block) => {
      const token = block.trim();
      const fence = token.match(/^\u0000(\d+)\u0000$/);
      if (fence) {
        return fences[Number(fence[1])] || "";
      }
      const lines = block.split("\n");
      if (lines.length && lines.every((line) => /^\s*[-*]\s+/.test(line))) {
        return (
          "<ul>" +
          lines.map((line) => "<li>" + inlineMarkdown(line.replace(/^\s*[-*]\s+/, "")) + "</li>").join("") +
          "</ul>"
        );
      }
      return "<p>" + lines.map(inlineMarkdown).join("<br>") + "</p>";
    })
    .join("");
}

function formatTokens(value) {
  const n = Number(value);
  if (!isFinite(n) || n <= 0) {
    return "0";
  }
  if (n < 1000) {
    return String(Math.round(n));
  }
  if (n < 10000) {
    const tenths = Math.round(n / 100) / 10;
    return (tenths % 1 === 0 ? String(tenths.toFixed(0)) : String(tenths)) + "K";
  }
  if (n < 1000000) {
    return Math.round(n / 1000) + "K";
  }
  const millions = Math.round(n / 100000) / 10;
  return (millions % 1 === 0 ? String(millions.toFixed(0)) : String(millions)) + "M";
}

function defaultContextWindow(model) {
  const name = String(model || "").toLowerCase();
  if (name.includes("claude") || name.includes("gpt") || name.includes("codex") || name.includes("openai")) {
    return 200000;
  }
  return 500000;
}

function formatUsage(used, size) {
  const u = Number(used) || 0;
  const s = Number(size) || 0;
  if (s > 0) {
    const left = u > 0 ? formatTokens(u) : "0K";
    return left + "/" + formatTokens(s);
  }
  return formatTokens(u) + "/—";
}

function toolBucket(kind, title) {
  const name = String(kind || "").toLowerCase();
  const label = String(title || "").toLowerCase();
  if (name === "execute" || /command|shell|terminal|команд/.test(label)) {
    return "ran";
  }
  if (name === "edit" || name === "delete" || name === "move" || /write|edit|generat|запис/.test(label)) {
    return "edited";
  }
  if (name === "read" || name === "search" || name === "fetch" || /read|list|search|explor/.test(label)) {
    return "explored";
  }
  return "other";
}

function summarizeTools(items) {
  const count = { explored: 0, edited: 0, ran: 0, other: 0 };
  for (const item of items || []) {
    count[toolBucket(item.kind, item.text)] += 1;
  }
  const parts = [];
  if (count.explored) {
    parts.push(count.explored === 1 ? "Explored 1 item" : "Explored " + count.explored + " items");
  }
  if (count.edited) {
    parts.push(count.edited === 1 ? "generated 1 item" : "generated " + count.edited + " items");
  }
  if (count.ran) {
    parts.push(count.ran === 1 ? "ran 1 command" : "ran " + count.ran + " commands");
  }
  if (count.other) {
    parts.push(count.other === 1 ? "1 more" : count.other + " more");
  }
  return parts.join(", ") || "Tools";
}

function clockLabel() {
  return new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function shortTitle(prompt) {
  const first = String(prompt || "")
    .trim()
    .split(/\r?\n/)[0]
    .trim();
  if (!first) {
    return "New session";
  }
  return first.length > 48 ? first.slice(0, 48) + "…" : first;
}

const modelNames = { grok: "Grok 4.6", claude: "Claude Code", gpt: "Codex" };
const modelPlaceholders = { grok: "Ask...", claude: "Ask Claude...", gpt: "Ask Codex..." };
const effortNames = { low: "Low", medium: "Medium", high: "High", xhigh: "Extra high" };

function boot() {
  const vscode = acquireVsCodeApi();
  const log = document.getElementById("log");
  const welcome = document.getElementById("welcome");
  const sessionTitle = document.getElementById("session-title");
  const text = document.getElementById("text");
  const send = document.getElementById("send");
  const sendIcon = document.getElementById("send-icon");
  const stopIcon = document.getElementById("stop-icon");
  const modelBtn = document.getElementById("model");
  const effortBtn = document.getElementById("effort");
  const autoBtn = document.getElementById("auto");
  const usage = document.getElementById("usage");
  const jump = document.getElementById("jump");
  const activeFileBtn = document.getElementById("active-file");
  const activeFileNameEl = document.getElementById("active-file-name");
  const historyList = document.getElementById("history-list");
  const remoteBtn = document.getElementById("remote-btn");
  const menus = {
    plus: document.getElementById("plus-menu"),
    model: document.getElementById("model-menu"),
    effort: document.getElementById("effort-menu"),
    history: document.getElementById("history-menu"),
    more: document.getElementById("more-menu"),
  };
  const tools = new Map();
  let thoughtEl = null;
  let agentEl = null;
  let agentText = "";
  let toolBox = null;
  let toolItems = [];
  let effort = "high";
  let model = "grok";
  let modelLocked = false;
  let autoAccept = true;
  let recognition = null;
  let isBusy = false;
  let usedTokens = 0;
  let contextSize = defaultContextWindow("grok");
  let activeFilePath = "";
  let includeActiveFile = true;

  function syncStage() {
    const hasMessages = log.children.length > 0;
    if (welcome) {
      welcome.hidden = hasMessages;
    }
    log.hidden = !hasMessages;
  }

  function scrollIfPinned() {
    syncStage();
    const gap = log.scrollHeight - log.scrollTop - log.clientHeight;
    if (gap < 80) {
      log.scrollTop = log.scrollHeight;
    }
    jump.hidden = log.hidden || log.scrollHeight - log.scrollTop - log.clientHeight < 48;
  }

  function closeMenus() {
    for (const menu of Object.values(menus)) {
      if (menu) {
        menu.hidden = true;
      }
    }
  }

  function toggleMenu(name) {
    const target = menus[name];
    if (!target) {
      return;
    }
    const open = target.hidden;
    closeMenus();
    target.hidden = !open;
  }

  function chip(button, label) {
    if (!button) {
      return;
    }
    const icon = button.querySelector("svg");
    if (icon) {
      button.textContent = label + " ";
      button.appendChild(icon);
    } else {
      button.textContent = label;
    }
  }

  function syncSendState() {
    if (isBusy) {
      send.disabled = false;
      send.classList.add("stopping");
      if (sendIcon) sendIcon.hidden = true;
      if (stopIcon) stopIcon.hidden = false;
      send.setAttribute("aria-label", "Остановить");
    } else {
      send.classList.remove("stopping");
      if (sendIcon) sendIcon.hidden = false;
      if (stopIcon) stopIcon.hidden = true;
      send.disabled = !text.value.trim();
      send.setAttribute("aria-label", "Отправить");
    }
  }

  function resize() {
    text.style.height = "auto";
    text.style.height = Math.min(text.scrollHeight, 160) + "px";
    syncSendState();
  }

  function resetTurn() {
    thoughtEl = null;
    agentEl = null;
    agentText = "";
    toolBox = null;
    toolItems = [];
    tools.clear();
  }

  function applyModelUI(nextModel, nextWindow) {
    model = nextModel || "grok";
    chip(modelBtn, modelNames[model] || model);
    text.placeholder = modelPlaceholders[model] || "Ask " + (modelNames[model] || model) + "...";
    contextSize = Number(nextWindow) > 0 ? Number(nextWindow) : defaultContextWindow(model);
    setUsage(usedTokens, contextSize);
    if (menus.model) {
      for (const button of menus.model.querySelectorAll("button")) {
        button.hidden = modelLocked && button.dataset.model !== model;
        button.setAttribute("aria-checked", button.dataset.model === model ? "true" : "false");
      }
    }
  }

  function setActiveFile(relPath, baseName) {
    activeFilePath = String(relPath || "");
    const label = String(baseName || relPath || "");
    if (!activeFileBtn || !activeFileNameEl) {
      return;
    }
    if (!label) {
      activeFileBtn.hidden = true;
      activeFileNameEl.textContent = "";
      return;
    }
    activeFileNameEl.textContent = label;
    activeFileBtn.hidden = false;
    activeFileBtn.classList.toggle("off", !includeActiveFile);
    activeFileBtn.title = includeActiveFile
      ? "Active file included: " + activeFilePath + " (click to exclude)"
      : "Active file excluded: " + activeFilePath + " (click to include)";
  }

  function add(kind, value) {
    const line = document.createElement("div");
    line.className = kind === "you" || kind === "agent" ? "msg " + kind : kind;
    if (kind === "you") {
      if (sessionTitle && sessionTitle.textContent === "New session" && value) {
        sessionTitle.textContent = shortTitle(value);
      }
      const bubble = document.createElement("div");
      bubble.className = "bubble";
      bubble.textContent = value || "";
      const meta = document.createElement("div");
      meta.className = "meta";
      const copy = document.createElement("button");
      copy.type = "button";
      copy.textContent = "Copy";
      copy.addEventListener("click", () => {
        if (navigator.clipboard) {
          navigator.clipboard.writeText(value || "").catch(() => {});
        }
      });
      const edit = document.createElement("button");
      edit.type = "button";
      edit.textContent = "Edit";
      edit.addEventListener("click", () => {
        text.value = value || "";
        resize();
        text.focus();
      });
      meta.append(copy, edit, document.createTextNode(clockLabel()));
      line.append(bubble, meta);
    } else if (kind === "agent") {
      const body = document.createElement("div");
      body.className = "agent-body";
      body.innerHTML = renderMarkdown(value || "");
      const meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = clockLabel();
      line.append(body, meta);
    } else {
      line.textContent = value || "";
    }
    log.appendChild(line);
    scrollIfPinned();
    return line;
  }

  function ensureAgent() {
    if (agentEl) {
      return;
    }
    const wrap = document.createElement("div");
    wrap.className = "msg";
    agentEl = document.createElement("div");
    agentEl.className = "agent-body";
    const meta = document.createElement("div");
    meta.className = "meta";
    meta.textContent = clockLabel();
    wrap.append(agentEl, meta);
    log.appendChild(wrap);
    syncStage();
  }

  function noteTool(msg) {
    if (!toolBox) {
      toolBox = document.createElement("details");
      toolBox.className = "tools";
      const summary = document.createElement("summary");
      const list = document.createElement("div");
      list.className = "tool-list";
      toolBox.append(summary, list);
      log.appendChild(toolBox);
    }
    const list = toolBox.querySelector(".tool-list");
    let row = msg.id ? tools.get(msg.id) : null;
    if (!row) {
      row = document.createElement("div");
      row.className = "tool-row";
      list.appendChild(row);
      if (msg.id) {
        tools.set(msg.id, row);
      }
      toolItems.push({ kind: msg.kind || "", text: msg.text || "", row });
    } else {
      const found = toolItems.find((item) => item.row === row);
      if (found) {
        found.kind = msg.kind || found.kind;
        found.text = msg.text || found.text;
      }
    }
    row.textContent = msg.text || "";
    toolBox.querySelector("summary").textContent = summarizeTools(toolItems);
    scrollIfPinned();
  }

  function setUsage(used, size) {
    usedTokens = Number(used) || 0;
    const total = Number(size) > 0 ? Number(size) : contextSize;
    const ring = document.getElementById("donut");
    const length = 2 * Math.PI * 5.5;
    const frac = total > 0 ? Math.max(0, Math.min(1, usedTokens / total)) : 0;
    if (ring) {
      ring.setAttribute("stroke-dasharray", (frac * length).toFixed(2) + " " + length.toFixed(2));
    }
    usage.textContent = formatUsage(usedTokens, total);
  }

  function renderHistoryList(items) {
    if (!historyList) {
      return;
    }
    historyList.textContent = "";
    const list = Array.isArray(items) ? items : [];
    if (!list.length) {
      const empty = document.createElement("div");
      empty.className = "menu-empty";
      empty.textContent = "No saved sessions";
      historyList.appendChild(empty);
      return;
    }
    for (const item of list) {
      const row = document.createElement("div");
      row.className = "history-row";
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "history-item";
      btn.dataset.sessionId = item.id || "";
      btn.textContent = item.title || "New session";
      const del = document.createElement("button");
      del.type = "button";
      del.className = "history-del";
      del.dataset.deleteId = item.id || "";
      del.title = "Delete session";
      del.textContent = "×";
      row.append(btn, del);
      historyList.appendChild(row);
    }
  }

  function showPermission(msg) {
    const card = document.createElement("div");
    card.className = "card";
    const title = document.createElement("div");
    title.textContent = msg.title || "Allow this action?";
    const actions = document.createElement("div");
    actions.className = "actions";
    const options = msg.options && msg.options.length ? msg.options : [{ optionId: "", name: "Dismiss" }];
    for (const opt of options) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = opt.name || opt.kind || "Allow";
      button.addEventListener("click", () => {
        vscode.postMessage({ type: "permission", optionId: opt.optionId || "" });
        card.remove();
      });
      actions.appendChild(button);
    }
    card.append(title, actions);
    log.appendChild(card);
    scrollIfPinned();
  }

  function renderActivity(msg) {
    if (msg.phase === "usage") {
      setUsage(Number(msg.used) || 0, Number(msg.size) || contextSize);
      return;
    }
    if (msg.phase === "done") {
      const wait = document.getElementById("wait");
      if (wait) {
        wait.remove();
      }
      resetTurn();
      isBusy = false;
      syncSendState();
      return;
    }
    if (msg.phase === "wait") {
      let wait = document.getElementById("wait");
      if (!wait) {
        resetTurn();
        wait = document.createElement("div");
        wait.id = "wait";
        wait.className = "status wait";
        log.appendChild(wait);
      }
      wait.textContent = msg.text || "Thinking…";
      log.appendChild(wait);
      scrollIfPinned();
      return;
    }
    const wait = document.getElementById("wait");
    if (wait) {
      wait.remove();
    }
    if (msg.phase === "thought") {
      if (!thoughtEl) {
        thoughtEl = document.createElement("details");
        thoughtEl.className = "thought";
        thoughtEl.open = true;
        const summary = document.createElement("summary");
        summary.textContent = "Thinking";
        const body = document.createElement("div");
        body.className = "thought-body";
        thoughtEl.append(summary, body);
        log.appendChild(thoughtEl);
      }
      thoughtEl.querySelector(".thought-body").textContent += msg.text || "";
    } else if (msg.phase === "agent") {
      ensureAgent();
      agentText += msg.text || "";
      agentEl.innerHTML = renderMarkdown(agentText);
    } else if (msg.phase === "tool") {
      noteTool(msg);
    } else {
      add("status", msg.text || "");
    }
    scrollIfPinned();
  }

  document.getElementById("plus").addEventListener("click", () => toggleMenu("plus"));
  modelBtn.addEventListener("click", () => {
    if (!modelLocked) {
      toggleMenu("model");
    }
  });
  if (effortBtn) {
    effortBtn.addEventListener("click", () => toggleMenu("effort"));
  }
  if (remoteBtn) {
    remoteBtn.addEventListener("click", () => {
      closeMenus();
      vscode.postMessage({ type: "showModel" });
    });
  }
  const historyBtn = document.getElementById("history-btn");
  if (historyBtn) {
    historyBtn.addEventListener("click", () => {
      const opening = menus.history && menus.history.hidden;
      toggleMenu("history");
      if (opening) {
        vscode.postMessage({ type: "history" });
      }
    });
  }
  const newBtn = document.getElementById("new-btn");
  if (newBtn) {
    newBtn.addEventListener("click", () => {
      closeMenus();
      vscode.postMessage({ type: "new" });
    });
  }
  const moreBtn = document.getElementById("more-btn");
  if (moreBtn) {
    moreBtn.addEventListener("click", () => toggleMenu("more"));
  }
  if (activeFileBtn) {
    activeFileBtn.addEventListener("click", () => {
      includeActiveFile = !includeActiveFile;
      setActiveFile(activeFilePath, activeFileNameEl ? activeFileNameEl.textContent : "");
    });
  }
  if (menus.history) {
    menus.history.addEventListener("click", (event) => {
      const delBtn = event.target && event.target.closest && event.target.closest("[data-delete-id]");
      if (delBtn && delBtn.dataset.deleteId) {
        event.stopPropagation();
        vscode.postMessage({ type: "deleteSession", sessionId: delBtn.dataset.deleteId });
        return;
      }
      const itemBtn = event.target && event.target.closest && event.target.closest("[data-session-id]");
      if (itemBtn && itemBtn.dataset.sessionId) {
        closeMenus();
        vscode.postMessage({ type: "loadSession", sessionId: itemBtn.dataset.sessionId });
      }
    });
  }
  if (menus.more) {
    menus.more.addEventListener("click", (event) => {
      const btn = event.target && event.target.closest && event.target.closest("[data-more]");
      const act = btn && btn.dataset && btn.dataset.more;
      if (!act) {
        return;
      }
      closeMenus();
      if (act === "token") {
        vscode.postMessage({ type: "setToken" });
      } else if (act === "quota") {
        vscode.postMessage({ type: "showModel" });
      } else if (act === "settings") {
        vscode.postMessage({ type: "openSettings" });
      }
    });
  }
  menus.plus.addEventListener("click", (event) => {
    const btn = event.target && event.target.closest && event.target.closest("[data-act]");
    const act = btn && btn.dataset && btn.dataset.act;
    if (!act) {
      return;
    }
    closeMenus();
    if (act === "new") {
      vscode.postMessage({ type: "new" });
    } else if (act === "file") {
      vscode.postMessage({ type: "attach" });
    } else if (act === "active" && activeFileBtn) {
      includeActiveFile = !includeActiveFile;
      setActiveFile(activeFilePath, activeFileNameEl ? activeFileNameEl.textContent : "");
    }
  });
  menus.model.addEventListener("click", (event) => {
    const btn = event.target && event.target.closest && event.target.closest("[data-model]");
    const next = btn && btn.dataset && btn.dataset.model;
    if (!next || modelLocked) {
      return;
    }
    applyModelUI(next, defaultContextWindow(next));
    closeMenus();
    vscode.postMessage({ type: "model", model: next });
  });
  menus.effort.addEventListener("click", (event) => {
    const btn = event.target && event.target.closest && event.target.closest("[data-effort]");
    const next = btn && btn.dataset && btn.dataset.effort;
    if (!next) {
      return;
    }
    effort = next;
    chip(effortBtn, effortNames[next] || next);
    for (const button of menus.effort.querySelectorAll("button")) {
      button.setAttribute("aria-checked", button.dataset.effort === next ? "true" : "false");
    }
    closeMenus();
  });
  autoBtn.addEventListener("click", () => {
    autoAccept = !autoAccept;
    autoBtn.classList.toggle("on", autoAccept);
  });
  document.getElementById("mic").addEventListener("click", () => {
    const Ctor = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!Ctor) {
      add("status", "Голос в этой панели недоступен");
      return;
    }
    if (recognition) {
      recognition.stop();
      return;
    }
    recognition = new Ctor();
    recognition.lang = "ru-RU";
    recognition.onresult = (event) => {
      const last = event.results[event.results.length - 1];
      const said = last && last[0] ? last[0].transcript : "";
      if (!said) {
        return;
      }
      text.value = (text.value ? text.value.replace(/\s*$/, " ") : "") + said;
      resize();
    };
    recognition.onend = () => {
      recognition = null;
      document.getElementById("mic").classList.remove("on");
    };
    document.getElementById("mic").classList.add("on");
    recognition.start();
  });
  document.getElementById("composer").addEventListener("submit", (event) => {
    event.preventDefault();
    if (isBusy) {
      vscode.postMessage({ type: "cancel" });
      return;
    }
    const value = text.value.trim();
    if (!value || send.disabled) {
      return;
    }
    text.value = "";
    isBusy = true;
    resize();
    closeMenus();
    vscode.postMessage({
      type: "prompt",
      text: value,
      effort,
      autoAccept,
      activeFile: includeActiveFile ? activeFilePath : "",
    });
  });
  text.addEventListener("input", resize);
  text.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      if (!isBusy) {
        document.getElementById("composer").requestSubmit();
      }
    }
  });
  jump.addEventListener("click", () => {
    log.scrollTop = log.scrollHeight;
    jump.hidden = true;
  });
  log.addEventListener("scroll", () => {
    jump.hidden = log.hidden || log.scrollHeight - log.scrollTop - log.clientHeight < 48;
  });
  document.addEventListener("click", (event) => {
    if (
      !event.target.closest(".menu") &&
      !event.target.closest(".top-menu") &&
      !event.target.closest("#plus") &&
      !event.target.closest("#model") &&
      !event.target.closest("#effort") &&
      !event.target.closest("#history-btn") &&
      !event.target.closest("#more-btn")
    ) {
      closeMenus();
    }
  });
  window.addEventListener("message", (event) => {
    const msg = event.data || {};
    if (msg.type === "clear") {
      log.textContent = "";
      resetTurn();
      if (sessionTitle) {
        sessionTitle.textContent = "New session";
      }
      setUsage(0, contextSize);
      syncStage();
      return;
    }
    if (msg.type === "busy") {
      isBusy = !!msg.text;
      syncSendState();
      return;
    }
    if (msg.type === "activity") {
      renderActivity(msg);
      return;
    }
    if (msg.type === "permission") {
      showPermission(msg);
      return;
    }
    if (msg.type === "insert") {
      text.value += msg.text || "";
      resize();
      text.focus();
      return;
    }
    if (msg.type === "activeFile") {
      setActiveFile(msg.path || "", msg.name || "");
      return;
    }
    if (msg.type === "sessions") {
      renderHistoryList(msg.sessions || []);
      return;
    }
    if (msg.type === "restore") {
      const s = msg.session || {};
      log.textContent = "";
      resetTurn();
      if (sessionTitle) {
        sessionTitle.textContent = s.title || "New session";
      }
      if (s.model) {
        applyModelUI(s.model, s.contextWindow);
      }
      setUsage(s.tokensUsed || 0, s.contextWindow || contextSize);
      for (const item of s.messages || []) {
        add(item.role === "user" ? "you" : "agent", item.text || "");
      }
      syncStage();
      return;
    }
    if (msg.type === "profile") {
      modelLocked = !!(msg.forced || msg.locked);
      modelBtn.disabled = modelLocked;
      if (remoteBtn && typeof msg.remote === "boolean") {
        remoteBtn.classList.toggle("active", msg.remote);
      }
      if (msg.title && sessionTitle) {
        sessionTitle.textContent = msg.title;
      }
      applyModelUI(msg.model || "grok", msg.contextWindow);
      return;
    }
    const kinds = ["you", "agent", "error", "file", "status"];
    add(kinds.indexOf(msg.type) >= 0 ? msg.type : "status", msg.text || "");
  });
  applyModelUI(model, contextSize);
  syncStage();
  resize();
}

if (typeof document !== "undefined") {
  boot();
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    defaultContextWindow,
    formatTokens,
    formatUsage,
    renderMarkdown,
    shortTitle,
    summarizeTools,
    toolBucket,
  };
}

