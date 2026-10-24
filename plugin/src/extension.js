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
    this.sessionTitle = "New session";
    this.socket = null;
    this.nextId = 1;
    this.busy = false;
    this.model = "grok";
    this.forced = false;
    this.contextWindow = 500000;
    this.effort = "high";
    this.autoAccept = true;
    this.permissionWait = null;
    this.uploadedHashes = null;
    this.editorSub = null;
  }

  resolveWebviewView(view) {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.html = chatPage();
    if (vscode.window && typeof vscode.window.onDidChangeActiveTextEditor === "function") {
      this.editorSub = vscode.window.onDidChangeActiveTextEditor(() => {
        this.publishActiveFile();
      });
    }
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
        this.onPrompt(String(msg.text || ""), String(msg.activeFile || "")).catch((err) => {
          this.post("error", String(err.message || err));
        });
      } else if (msg.type === "cancel") {
        this.cancelTurn();
      } else if (msg.type === "model" && !this.forced && !this.sessionId) {
        this.model = normalizeModel(msg.model);
        this.contextWindow = defaultWindowFor(this.model);
        this.publishProfile();
      } else if (msg.type === "new") {
        this.newSession();
      } else if (msg.type === "history") {
        this.loadHistory().catch((err) => {
          this.post("error", String(err.message || err));
        });
      } else if (msg.type === "loadSession" && msg.sessionId) {
        this.restoreSession(String(msg.sessionId)).catch((err) => {
          this.post("error", String(err.message || err));
        });
      } else if (msg.type === "deleteSession" && msg.sessionId) {
        this.removeSession(String(msg.sessionId)).catch((err) => {
          this.post("error", String(err.message || err));
        });
      } else if (msg.type === "setToken") {
        setToken().then(() => this.refreshProfile()).catch(() => {});
      } else if (msg.type === "showModel") {
        showModel();
      } else if (msg.type === "openSettings") {
        vscode.commands.executeCommand("workbench.action.openSettings", "remoteLlm");
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
      if (this.editorSub) {
        this.editorSub.dispose();
        this.editorSub = null;
      }
    });
    this.publishActiveFile();
    this.refreshProfile().catch((err) => {
      this.post("error", String(err.message || err));
    });
  }

  publishActiveFile() {
    if (!this.view) {
      return;
    }
    const editor = vscode.window && vscode.window.activeTextEditor;
    if (!editor || !editor.document || editor.document.isUntitled) {
      this.view.webview.postMessage({ type: "activeFile", path: "", name: "" });
      return;
    }
    const fsPath = editor.document.uri && editor.document.uri.fsPath;
    if (!fsPath) {
      this.view.webview.postMessage({ type: "activeFile", path: "", name: "" });
      return;
    }
    const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
    let rel = folder ? path.relative(folder.uri.fsPath, fsPath).replace(/\\/g, "/") : path.basename(fsPath);
    if (!rel || rel.startsWith("..")) {
      rel = path.basename(fsPath);
    }
    this.view.webview.postMessage({
      type: "activeFile",
      path: rel,
      name: path.basename(rel),
    });
  }

  cancelTurn() {
    if (!this.busy || !this.socket) {
      return;
    }
    try {
      this.socket.send(
        JSON.stringify({
          jsonrpc: "2.0",
          method: "session/cancel",
          params: { sessionId: this.sessionId },
        })
      );
    } catch (_) {
      // Socket may already be closing.
    }
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
    this.sessionTitle = "New session";
    this.uploadedHashes = null;
    this.post("clear", "");
    this.publishProfile();
    this.publishActiveFile();
  }

  async loadHistory() {
    const cfg = vscode.workspace.getConfiguration("remoteLlm");
    if (!cfg.get("useRemoteGateway")) {
      this.view.webview.postMessage({ type: "sessions", sessions: [] });
      return;
    }
    const body = await getJSON(cfg, "/v1/sessions");
    this.view.webview.postMessage({
      type: "sessions",
      sessions: body.sessions || [],
    });
  }

  async restoreSession(sessionId) {
    if (this.busy) {
      return;
    }
    const cfg = vscode.workspace.getConfiguration("remoteLlm");
    const rec = await getJSON(cfg, "/v1/sessions/" + encodeURIComponent(sessionId));
    if (this.socket) {
      this.socket.close();
      this.socket = null;
    }
    this.sessionId = rec.id || sessionId;
    this.sessionTitle = rec.title || "New session";
    this.uploadedHashes = null;
    if (rec.model) {
      this.model = normalizeModel(rec.model);
    }
    if (rec.contextWindow) {
      this.contextWindow = rec.contextWindow;
    }
    this.view.webview.postMessage({ type: "restore", session: rec });
    this.publishProfile();
  }

  async removeSession(sessionId) {
    const cfg = vscode.workspace.getConfiguration("remoteLlm");
    await deleteJSON(cfg, "/v1/sessions/" + encodeURIComponent(sessionId));
    if (this.sessionId === sessionId) {
      this.newSession();
    }
    await this.loadHistory();
  }

  async refreshProfile() {
    const cfg = vscode.workspace.getConfiguration("remoteLlm");
    if (!cfg.get("useRemoteGateway")) {
      this.publishProfile();
      return;
    }
    const body = await getJSON(cfg, "/v1/me?model=" + encodeURIComponent(this.model));
    this.forced = !!body.forced;
    if (this.forced && body.model) {
      this.model = normalizeModel(body.model);
    }
    if (body.contextWindow) {
      this.contextWindow = body.contextWindow;
    } else {
      this.contextWindow = defaultWindowFor(this.model);
    }
    this.publishProfile();
  }

  publishProfile() {
    if (!this.view) {
      return;
    }
    const cfg = vscode.workspace.getConfiguration("remoteLlm");
    this.view.webview.postMessage({
      type: "profile",
      model: this.model,
      forced: this.forced,
      locked: !!this.sessionId,
      contextWindow: this.contextWindow || defaultWindowFor(this.model),
      title: this.sessionTitle || "New session",
      remote: !!cfg.get("useRemoteGateway"),
    });
  }

  async onPrompt(text, activeFile) {
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
      const syncResult = await uploadProject(cfg, folder, this.sessionId, this.uploadedHashes);
      this.uploadedHashes = syncResult.hashes;
      for (const name of syncResult.skipped) {
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
            activeFile: activeFile || "",
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
      await applyChanges(folder, changed.files || [], this.uploadedHashes);
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
    this.sessionTitle = created.title || "New session";
    this.forced = !!created.forced;
    this.uploadedHashes = null;
    if (created.model) {
      this.model = normalizeModel(created.model);
    }
    if (created.contextWindow) {
      this.contextWindow = created.contextWindow;
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

async function uploadProject(cfg, folder, sessionId, prevHashes) {
  const uris = await vscode.workspace.findFiles(
    "**/*",
    "**/{.git,node_modules,vendor,.next,dist,build,target,.venv,venv,__pycache__}/**",
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
  if (!prevHashes) {
    await putJSON(cfg, "/v1/sessions/" + sessionId + "/files", { files });
    return { skipped, hashes: sync.hashSnapshot(files) };
  }
  const { patch, nextMap } = sync.diffSnapshot(prevHashes, files);
  if (patch.length > 0) {
    await patchJSON(cfg, "/v1/sessions/" + sessionId + "/files", { files: patch });
  }
  return { skipped, hashes: nextMap };
}

async function applyChanges(folder, files, knownHashes) {
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
    if (knownHashes) {
      const entry = sync.fileFromBuffer(file.path, file.body);
      if (entry) {
        const sub = sync.hashSnapshot([entry]);
        knownHashes.set(file.path, sub.get(file.path));
      }
    }
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
    if (knownHashes) {
      knownHashes.delete(rel);
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

async function patchJSON(cfg, route, payload) {
  const { base, token } = await gatewayConfig(cfg);
  const res = await fetch(base + route, {
    method: "PATCH",
    headers: {
      Authorization: "Bearer " + token,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });
  return readBody(res);
}

async function deleteJSON(cfg, route) {
  const { base, token } = await gatewayConfig(cfg);
  const res = await fetch(base + route, {
    method: "DELETE",
    headers: { Authorization: "Bearer " + token },
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

function defaultWindowFor(model) {
  return normalizeModel(model) === "grok" ? 500000 : 200000;
}

function httpBase(url) {
  const raw = String(url || "").replace(/\/$/, "");
  return raw.replace(/^ws:/, "http:").replace(/^wss:/, "https:");
}

function deactivate() {}

module.exports = { activate, deactivate, httpBase };

