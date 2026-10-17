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

const modelNames = { grok: "Grok", claude: "Claude", gpt: "GPT" };
const effortNames = { low: "Low", medium: "Medium", high: "High", xhigh: "Extra high" };

function boot() {
  const vscode = acquireVsCodeApi();
  const log = document.getElementById("log");
  const text = document.getElementById("text");
  const send = document.getElementById("send");
  const modelBtn = document.getElementById("model");
  const effortBtn = document.getElementById("effort");
  const autoBtn = document.getElementById("auto");
  const usage = document.getElementById("usage");
  const jump = document.getElementById("jump");
  const menus = {
    plus: document.getElementById("plus-menu"),
    model: document.getElementById("model-menu"),
    effort: document.getElementById("effort-menu"),
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

  function scrollIfPinned() {
    const gap = log.scrollHeight - log.scrollTop - log.clientHeight;
    if (gap < 80) {
      log.scrollTop = log.scrollHeight;
    }
    jump.hidden = log.scrollHeight - log.scrollTop - log.clientHeight < 48;
  }

  function closeMenus() {
    for (const menu of Object.values(menus)) {
      menu.hidden = true;
    }
  }

  function toggleMenu(name) {
    const open = menus[name].hidden;
    closeMenus();
    menus[name].hidden = !open;
  }

  function chip(button, label) {
    const icon = button.querySelector("svg");
    button.textContent = label + " ";
    if (icon) {
      button.appendChild(icon);
    }
  }

  function resize() {
    text.style.height = "auto";
    text.style.height = Math.min(text.scrollHeight, 160) + "px";
    send.disabled = !text.value.trim();
  }

  function resetTurn() {
    thoughtEl = null;
    agentEl = null;
    agentText = "";
    toolBox = null;
    toolItems = [];
    tools.clear();
  }

  function add(kind, value) {
    const line = document.createElement("div");
    line.className = kind === "you" || kind === "agent" ? "msg " + kind : kind;
    if (kind === "you") {
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
    const ring = document.getElementById("donut");
    const length = 2 * Math.PI * 5.5;
    const frac = size > 0 ? Math.max(0, Math.min(1, used / size)) : 0;
    if (ring) {
      ring.setAttribute("stroke-dasharray", (frac * length).toFixed(2) + " " + length.toFixed(2));
    }
    usage.textContent = size > 0 ? formatTokens(used) + "/" + formatTokens(size) : formatTokens(used) + "/—";
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
      setUsage(Number(msg.used) || 0, Number(msg.size) || 0);
      return;
    }
    if (msg.phase === "done") {
      const wait = document.getElementById("wait");
      if (wait) {
        wait.remove();
      }
      resetTurn();
      send.disabled = !text.value.trim();
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
  effortBtn.addEventListener("click", () => toggleMenu("effort"));
  menus.plus.addEventListener("click", (event) => {
    const act = event.target && event.target.dataset && event.target.dataset.act;
    if (!act) {
      return;
    }
    closeMenus();
    if (act === "new") {
      vscode.postMessage({ type: "new" });
    } else if (act === "file") {
      vscode.postMessage({ type: "attach" });
    }
  });
  menus.model.addEventListener("click", (event) => {
    const next = event.target && event.target.dataset && event.target.dataset.model;
    if (!next || modelLocked) {
      return;
    }
    model = next;
    chip(modelBtn, modelNames[next] || next);
    for (const button of menus.model.querySelectorAll("button")) {
      button.setAttribute("aria-checked", button.dataset.model === next ? "true" : "false");
    }
    closeMenus();
    vscode.postMessage({ type: "model", model: next });
  });
  menus.effort.addEventListener("click", (event) => {
    const next = event.target && event.target.dataset && event.target.dataset.effort;
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
    const value = text.value.trim();
    if (!value || send.disabled) {
      return;
    }
    text.value = "";
    resize();
    send.disabled = true;
    closeMenus();
    vscode.postMessage({ type: "prompt", text: value, effort, autoAccept });
  });
  text.addEventListener("input", resize);
  text.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      document.getElementById("composer").requestSubmit();
    }
  });
  jump.addEventListener("click", () => {
    log.scrollTop = log.scrollHeight;
    jump.hidden = true;
  });
  log.addEventListener("scroll", () => {
    jump.hidden = log.scrollHeight - log.scrollTop - log.clientHeight < 48;
  });
  document.addEventListener("click", (event) => {
    if (!event.target.closest(".menu") && !event.target.closest("#plus") && !event.target.closest("#model") && !event.target.closest("#effort")) {
      closeMenus();
    }
  });
  window.addEventListener("message", (event) => {
    const msg = event.data || {};
    if (msg.type === "clear") {
      log.textContent = "";
      resetTurn();
      setUsage(0, 0);
      return;
    }
    if (msg.type === "busy") {
      send.disabled = !!msg.text || !text.value.trim();
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
    if (msg.type === "profile") {
      model = msg.model || "grok";
      modelLocked = !!(msg.forced || msg.locked);
      chip(modelBtn, modelNames[model] || model);
      modelBtn.disabled = modelLocked;
      for (const button of menus.model.querySelectorAll("button")) {
        button.hidden = modelLocked && button.dataset.model !== model;
        button.setAttribute("aria-checked", button.dataset.model === model ? "true" : "false");
      }
      return;
    }
    const kinds = ["you", "agent", "error", "file", "status"];
    add(kinds.indexOf(msg.type) >= 0 ? msg.type : "status", msg.text || "");
  });
  resize();
}

if (typeof document !== "undefined") {
  boot();
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = { formatTokens, renderMarkdown, summarizeTools, toolBucket };
}
