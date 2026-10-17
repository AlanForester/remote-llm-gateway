const path = require("path");
const vscode = require("vscode");
const { chatPage } = require("./chat-page");
const sync = require("./sync");
const turn = require("./turn");
const ws = require("./ws");

const tokenKey = "remoteLlm.gatewayToken";
let secrets;

function activate(context) {
  secrets = context.secrets;
  context.subscriptions.push(
    vscode.commands.registerCommand("remoteLlm.open", () =>
      vscode.commands.executeCommand("remoteLlm.chat.focus")
    ),
    vscode.commands.registerCommand("remoteLlm.showModel", () => showModel()),
    vscode.commands.registerCommand("remoteLlm.setToken", () => setToken())
  );
  migrateLegacyToken().catch(() => {});
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider("remoteLlm.chat", new ChatView())
  );
}

async function setToken() {
  const value = await vscode.window.showInputBox({
    title: "Токен Remote LLM",
    prompt: "Токен пользователя из админки",
    password: true,
    ignoreFocusOut: true,
  });
  if (!value || !value.trim()) {
    return;
  }
  await secrets.store(tokenKey, value.trim());
  vscode.window.showInformationMessage("Токен сохранён в Secret Storage.");
}

async function migrateLegacyToken() {
  const existing = await secrets.get(tokenKey);
  if (existing) {
    return;
  }
  const cfg = vscode.workspace.getConfiguration("remoteLlm");
  const legacy = cfg.get("gatewayToken");
  if (!legacy) {
    return;
  }
  await secrets.store(tokenKey, String(legacy));
  const inspected = cfg.inspect("gatewayToken");
  if (inspected && inspected.workspaceValue !== undefined) {
    await cfg.update("gatewayToken", undefined, vscode.ConfigurationTarget.Workspace);
  }
  if (inspected && inspected.globalValue !== undefined) {
    await cfg.update("gatewayToken", undefined, vscode.ConfigurationTarget.Global);
  }
}

async function gatewayToken() {
  const stored = await secrets.get(tokenKey);
  if (!stored) {
    throw new Error("Задайте токен командой «Remote LLM: задать токен».");
  }
  return stored;
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
    this.model = "grok";
    this.forced = false;
    this.effort = "high";
    this.autoAccept = true;
    this.permissionWait = null;
  }

  resolveWebviewView(view) {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.html = chatPage();
    view.webview.onDidReceiveMessage((msg) => {
      if (!msg) {
        return;
      }
      if (msg.type === "prompt") {
        if (msg.effort) {
          this.effort = String(msg.effort);
        }
        if (typeof msg.autoAccept === "boolean") {
          this.autoAccept = msg.autoAccept;
        }
        this.onPrompt(String(msg.text || "")).catch((err) => {
          this.post("error", String(err.message || err));
        });
      } else if (msg.type === "model" && !this.forced && !this.sessionId) {
        this.model = normalizeModel(msg.model);
        this.publishProfile();
      } else if (msg.type === "new") {
        this.newSession();
      } else if (msg.type === "attach") {
        this.attachFile().catch((err) => {
          this.post("error", String(err.message || err));
        });
      } else if (msg.type === "permission" && this.permissionWait) {
        const resolve = this.permissionWait;
        this.permissionWait = null;
        resolve(String(msg.optionId || ""));
      }
    });
    view.onDidDispose(() => {
      if (this.socket) {
        this.socket.close();
      }
    });
    this.refreshProfile().catch((err) => {
      this.post("error", String(err.message || err));
    });
  }

  newSession() {
    if (this.busy) {
      return;
    }
    if (this.socket) {
      this.socket.close();
      this.socket = null;
    }
    this.sessionId = "";
    this.post("clear", "");
    this.publishProfile();
  }

  async refreshProfile() {
    const cfg = vscode.workspace.getConfiguration("remoteLlm");
    if (!cfg.get("useRemoteGateway")) {
      return;
    }
    const body = await getJSON(cfg, "/v1/me");
    this.forced = !!body.forced;
    if (this.forced && body.model) {
      this.model = body.model;
    }
    this.publishProfile();
  }

  publishProfile() {
    this.view.webview.postMessage({
      type: "profile",
      model: this.model,
      forced: this.forced,
      locked: !!this.sessionId,
    });
  }

  async onPrompt(text) {
    const trimmed = text.trim();
    if (!trimmed || this.busy) {
      return;
    }
    this.busy = true;
    this.post("busy", "1");
    this.post("you", trimmed);
    this.postActivity("wait", "Отправляю проект…");
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
        this.socket = await ws.connect(ws.gatewaySocketURL(cfg.get("gatewayUrl"), this.sessionId), {
          Authorization: "Bearer " + (await gatewayToken()),
        });
      }
      this.postActivity("wait", "Thinking…");
      const id = this.nextId++;
      this.socket.send(
        JSON.stringify({
          jsonrpc: "2.0",
          id,
          method: "session/prompt",
          params: {
            text: trimmed,
            effort: this.effort || "high",
            autoAccept: this.autoAccept !== false,
          },
        })
      );
      const outcome = await turn.readTurn(
        this.socket,
        id,
        (activity) => {
          this.postActivity(activity.phase, activity.text, activity.id, activity);
        },
        (request) => this.askPermission(request)
      );
      const reply = outcome.msg;
      if (reply.error) {
        throw new Error(reply.error.message || "Шлюз отказал.");
      }
      const shown = turn.replyText(reply);
      if (shown && !(shown.kind === "agent" && outcome.streamed)) {
        this.post(shown.kind, shown.text);
      } else if (!shown && !outcome.streamed && !outcome.thought) {
        this.post("status", "Пустой ответ");
      }
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
      this.postActivity("done", "");
      this.busy = false;
      this.post("busy", "");
    }
  }

  async openSession(cfg, folder) {
    const created = await postJSON(cfg, "/v1/sessions", {
      workspace: folder.uri.fsPath,
      model: this.forced ? "" : this.model,
    });
    this.sessionId = created.sessionId;
    this.forced = !!created.forced;
    if (created.model) {
      this.model = created.model;
    }
    this.publishProfile();
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

  postActivity(phase, text, id, extra) {
    if (!this.view) {
      return;
    }
    const more = extra || {};
    this.view.webview.postMessage({
      type: "activity",
      phase,
      text: text || "",
      id: id || "",
      kind: more.kind || "",
      used: more.used,
      size: more.size,
    });
  }

  askPermission(request) {
    const params = (request && request.params) || {};
    const tool = params.toolCall || {};
    const options = params.options || [];
    return new Promise((resolve) => {
      this.permissionWait = resolve;
      this.view.webview.postMessage({
        type: "permission",
        title: tool.title || "Allow this action?",
        options,
      });
    }).then((optionId) => turn.permissionResponse(request, optionId));
  }

  async attachFile() {
    const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
    if (!folder) {
      throw new Error("Откройте папку проекта.");
    }
    const picked = await vscode.window.showOpenDialog({
      canSelectMany: false,
      defaultUri: folder.uri,
      openLabel: "Упомянуть файл",
    });
    if (!picked || !picked[0]) {
      return;
    }
    let rel = path.relative(folder.uri.fsPath, picked[0].fsPath).replace(/\\/g, "/");
    if (!rel || rel.startsWith("..")) {
      rel = picked[0].fsPath;
    }
    this.view.webview.postMessage({ type: "insert", text: "@" + rel + " " });
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

async function gatewayConfig(cfg) {
  return { base: httpBase(cfg.get("gatewayUrl")), token: await gatewayToken() };
}

async function getJSON(cfg, route) {
  const { base, token } = await gatewayConfig(cfg);
  const res = await fetch(base + route, {
    headers: { Authorization: "Bearer " + token },
  });
  return readBody(res);
}

async function postJSON(cfg, route, payload) {
  const { base, token } = await gatewayConfig(cfg);
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
  const { base, token } = await gatewayConfig(cfg);
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

function normalizeModel(value) {
  const name = String(value || "").toLowerCase();
  if (name.indexOf("claude") >= 0) {
    return "claude";
  }
  if (name.indexOf("gpt") >= 0 || name.indexOf("openai") >= 0 || name.indexOf("codex") >= 0) {
    return "gpt";
  }
  return "grok";
}

function httpBase(url) {
  const raw = String(url || "").replace(/\/$/, "");
  return raw.replace(/^ws:/, "http:").replace(/^wss:/, "https:");
}

function deactivate() {}

module.exports = { activate, deactivate, httpBase };
