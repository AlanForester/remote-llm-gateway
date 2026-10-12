const crypto = require("crypto");
const path = require("path");
const vscode = require("vscode");
const sync = require("./sync");
const ws = require("./ws");

function activate(context) {
  context.subscriptions.push(
    vscode.commands.registerCommand("remoteLlm.showModel", () => showModel())
  );
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider("remoteLlm.chat", new ChatView())
  );
}

async function showModel() {
  const cfg = vscode.workspace.getConfiguration("remoteLlm");
  if (!cfg.get("useRemoteGateway")) {
    vscode.window.showInformationMessage("Включён локальный CLI, шлюз не используется.");
    return;
  }
  try {
    const body = await getJSON(cfg, "/v1/me");
    const who = body.forced
      ? "Назначена в админке: " + body.model
      : "Модель: " + body.model;
    const limit =
      body.tokenLimit > 0
        ? ". Токены " + body.tokensUsed + " из " + body.tokenLimit
        : ". Лимит не задан";
    vscode.window.showInformationMessage(who + limit);
  } catch (err) {
    vscode.window.showErrorMessage(String(err.message || err));
  }
}

class ChatView {
  constructor() {
    this.sessionId = "";
    this.socket = null;
    this.nextId = 1;
    this.busy = false;
  }

  resolveWebviewView(view) {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.html = chatHtml();
    view.webview.onDidReceiveMessage((msg) => {
      if (msg && msg.type === "prompt") {
        this.onPrompt(String(msg.text || "")).catch((err) => {
          this.post("error", String(err.message || err));
        });
      }
    });
    view.onDidDispose(() => {
      if (this.socket) {
        this.socket.close();
      }
    });
  }

  async onPrompt(text) {
    const trimmed = text.trim();
    if (!trimmed || this.busy) {
      return;
    }
    this.busy = true;
    try {
      const cfg = vscode.workspace.getConfiguration("remoteLlm");
      if (!cfg.get("useRemoteGateway")) {
        throw new Error("Включён локальный CLI, шлюз не используется.");
      }
      const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
      if (!folder) {
        throw new Error("Откройте папку проекта.");
      }
      if (!this.sessionId) {
        await this.openSession(cfg, folder);
      }
      const skipped = await uploadProject(cfg, folder, this.sessionId);
      for (const name of skipped) {
        this.post("status", "пропущен " + name);
      }
      if (!this.socket) {
        this.socket = await ws.connect(
          socketURL(cfg.get("gatewayUrl"), this.sessionId, cfg.get("gatewayToken"))
        );
      }
      this.post("you", trimmed);
      const id = this.nextId++;
      this.socket.send(
        JSON.stringify({
          jsonrpc: "2.0",
          id,
          method: "session/prompt",
          params: { text: trimmed },
        })
      );
      const reply = await readResult(this.socket, id);
      if (reply.error) {
        throw new Error(reply.error.message || "Шлюз отказал.");
      }
      const answer = (reply.result && reply.result.text) || JSON.stringify(reply.result || {});
      this.post("agent", answer);
      const changed = await getJSON(
        cfg,
        "/v1/sessions/" + this.sessionId + "/changes"
      );
      await applyChanges(folder, changed.files || []);
      for (const file of changed.files || []) {
        this.post("file", file.deleted ? "удалён " + file.path : "записан " + file.path);
      }
    } catch (err) {
      if (this.socket) {
        this.socket.close();
        this.socket = null;
      }
      throw err;
    } finally {
      this.busy = false;
    }
  }

  async openSession(cfg, folder) {
    const created = await postJSON(cfg, "/v1/sessions", {
      workspace: folder.uri.fsPath,
      model: "",
    });
    this.sessionId = created.sessionId;
    const who = created.forced
      ? "Назначена в админке: " + created.model
      : "Модель: " + created.model;
    this.post("status", who);
  }

  post(kind, text) {
    if (!this.view) {
      return;
    }
    this.view.webview.postMessage({ type: kind, text });
  }
}

async function uploadProject(cfg, folder, sessionId) {
  const uris = await vscode.workspace.findFiles(
    "**/*",
    "**/{.git,node_modules,vendor}/**",
    sync.MAX_FILES
  );
  if (uris.length >= sync.MAX_FILES) {
    throw new Error("слишком большой проект");
  }
  const files = [];
  const skipped = [];
  let total = 0;
  for (const uri of uris) {
    const rel = sync.safeRelative(path.relative(folder.uri.fsPath, uri.fsPath));
    if (!rel) {
      continue;
    }
    const stat = await vscode.workspace.fs.stat(uri);
    if (stat.size > sync.MAX_FILE_BYTES) {
      skipped.push(rel);
      continue;
    }
    const raw = Buffer.from(await vscode.workspace.fs.readFile(uri));
    total += raw.length;
    if (total > sync.MAX_TOTAL_BYTES) {
      throw new Error("слишком большой проект");
    }
    const file = sync.fileFromBuffer(rel, raw);
    if (!file) {
      skipped.push(rel);
      continue;
    }
    files.push(file);
  }
  await putJSON(cfg, "/v1/sessions/" + sessionId + "/files", { files });
  return skipped;
}

async function applyChanges(folder, files) {
  const plan = sync.planApply(files);
  for (const file of plan.writes) {
    const parts = file.path.split("/");
    if (parts.length > 1) {
      await vscode.workspace.fs.createDirectory(
        vscode.Uri.joinPath(folder.uri, ...parts.slice(0, -1))
      );
    }
    const uri = vscode.Uri.joinPath(folder.uri, ...parts);
    await vscode.workspace.fs.writeFile(uri, file.body);
  }
  for (const rel of plan.deletes) {
    const uri = vscode.Uri.joinPath(folder.uri, ...rel.split("/"));
    try {
      await vscode.workspace.fs.delete(uri, { useTrash: false });
    } catch (err) {
      const code = err && (err.code || err.name);
      if (code !== "FileNotFound" && code !== "EntryNotFound") {
        throw err;
      }
    }
  }
}

async function readResult(socket, id) {
  for (;;) {
    const text = await socket.next();
    const msg = JSON.parse(text);
    if (msg.id === id) {
      return msg;
    }
  }
}

function gatewayConfig(cfg) {
  const token = cfg.get("gatewayToken");
  if (!token) {
    throw new Error("Укажите remoteLlm.gatewayToken.");
  }
  return { base: httpBase(cfg.get("gatewayUrl")), token };
}

async function getJSON(cfg, route) {
  const { base, token } = gatewayConfig(cfg);
  const res = await fetch(base + route, {
    headers: { Authorization: "Bearer " + token },
  });
  return readBody(res);
}

async function postJSON(cfg, route, payload) {
  const { base, token } = gatewayConfig(cfg);
  const res = await fetch(base + route, {
    method: "POST",
    headers: {
      Authorization: "Bearer " + token,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
  return readBody(res);
}

async function putJSON(cfg, route, payload) {
  const { base, token } = gatewayConfig(cfg);
  const res = await fetch(base + route, {
    method: "PUT",
    headers: {
      Authorization: "Bearer " + token,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
  return readBody(res);
}

async function readBody(res) {
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(body.message || body.error || "Шлюз отказал.");
  }
  return body;
}

function httpBase(url) {
  const raw = String(url || "").replace(/\/$/, "");
  return raw.replace(/^ws:/, "http:").replace(/^wss:/, "https:");
}

function socketURL(gateway, sessionId, token) {
  const base = httpBase(gateway).replace(/^https:/, "wss:").replace(/^http:/, "ws:");
  return base + "/ws/" + sessionId + "?token=" + encodeURIComponent(token);
}

function chatHtml() {
  const nonce = crypto.randomBytes(16).toString("hex");
  return `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}'">
<style>
  body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); margin: 0; padding: 8px; }
  #log { white-space: pre-wrap; min-height: 8em; }
  .you { margin: 6px 0; }
  .agent { margin: 6px 0; }
  .file, .status { opacity: 0.8; }
  .error { color: var(--vscode-errorForeground); }
  form { display: flex; gap: 6px; margin-top: 8px; }
  textarea { flex: 1; min-height: 4em; }
</style>
</head>
<body>
<div id="log"></div>
<form>
  <textarea id="text" placeholder="Запрос агенту"></textarea>
  <button type="submit">Отправить</button>
</form>
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  const log = document.getElementById("log");
  document.querySelector("form").addEventListener("submit", (event) => {
    event.preventDefault();
    const text = document.getElementById("text").value.trim();
    if (!text) return;
    document.getElementById("text").value = "";
    vscode.postMessage({ type: "prompt", text });
  });
  window.addEventListener("message", (event) => {
    const msg = event.data || {};
    const line = document.createElement("div");
    line.className = msg.type || "status";
    const prefix = { you: "Вы: ", agent: "Агент: ", error: "", file: "", status: "" };
    line.textContent = (prefix[msg.type] || "") + (msg.text || "");
    log.appendChild(line);
  });
</script>
</body>
</html>`;
}

function deactivate() {}

module.exports = { activate, deactivate, httpBase, socketURL };
