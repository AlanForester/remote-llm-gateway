// Turns ACP session/update notifications into chat rows.

function contentText(content) {
  if (!content) {
    return "";
  }
  if (typeof content === "string") {
    return content;
  }
  if (Array.isArray(content)) {
    return content.map(contentText).join("");
  }
  if (content.type === "text" || typeof content.text === "string") {
    return content.text || "";
  }
  if (content.content) {
    return contentText(content.content);
  }
  return "";
}

function statusLabel(status) {
  switch (status) {
    case "pending":
      return "ждёт";
    case "in_progress":
      return "выполняется";
    case "completed":
      return "готово";
    case "failed":
      return "ошибка";
    case "cancelled":
      return "отменено";
    default:
      return "";
  }
}

function toolLabel(update) {
  const title = update.title || update.name || "инструмент";
  const status = statusLabel(update.status);
  return status ? title + " · " + status : title;
}

function activityFrom(msg) {
  if (!msg || msg.method !== "session/update" || !msg.params || !msg.params.update) {
    return null;
  }
  const update = msg.params.update;
  switch (update.sessionUpdate) {
    case "agent_thought_chunk": {
      const text = contentText(update.content);
      return text ? { phase: "thought", text } : null;
    }
    case "agent_message_chunk": {
      const text = contentText(update.content);
      return text ? { phase: "agent", text } : null;
    }
    case "tool_call":
    case "tool_call_update":
      return { phase: "tool", id: update.toolCallId || "", kind: update.kind || "", text: toolLabel(update) };
    case "usage_update":
      return { phase: "usage", used: Number(update.used) || 0, size: Number(update.size) || 0 };
    case "plan": {
      const lines = (update.entries || []).map((entry) => entry && entry.content).filter(Boolean);
      return lines.length ? { phase: "status", text: lines.join("\n") } : null;
    }
    case "notice": {
      const text = [update.title, update.description].filter(Boolean).join(" — ");
      return text ? { phase: "status", text } : null;
    }
    default:
      return null;
  }
}

function sameID(a, b) {
  return a === b || String(a) === String(b);
}

function permissionResponse(request, optionId) {
  if (!optionId) {
    return { jsonrpc: "2.0", id: request && request.id, result: { outcome: { outcome: "cancelled" } } };
  }
  return {
    jsonrpc: "2.0",
    id: request && request.id,
    result: { outcome: { outcome: "selected", optionId: optionId } },
  };
}

async function readTurn(socket, id, onActivity, onPermission) {
  const seen = { streamed: false, thought: false };
  for (;;) {
    const text = await socket.next();
    const msg = JSON.parse(text);
    if (msg.method === "session/request_permission" && onPermission) {
      const answer = await onPermission(msg);
      if (answer) {
        socket.send(JSON.stringify(answer));
      }
      continue;
    }
    const activity = activityFrom(msg);
    if (activity) {
      if (activity.phase === "agent") {
        seen.streamed = true;
      }
      if (activity.phase === "thought") {
        seen.thought = true;
      }
      if (onActivity) {
        onActivity(activity);
      }
      continue;
    }
    if (msg.error && (msg.id == null || sameID(msg.id, id))) {
      return { msg, streamed: seen.streamed, thought: seen.thought };
    }
    if (sameID(msg.id, id)) {
      return { msg, streamed: seen.streamed, thought: seen.thought };
    }
  }
}

function replyText(msg) {
  if (!msg) {
    return null;
  }
  if (msg.error && msg.error.message) {
    return { kind: "error", text: msg.error.message };
  }
  const result = msg.result;
  if (!result || typeof result !== "object") {
    return null;
  }
  if (typeof result.text === "string" && result.text) {
    return { kind: "agent", text: result.text };
  }
  if (result.stopReason && result.stopReason !== "end_turn") {
    return { kind: "status", text: "Остановлено: " + result.stopReason };
  }
  if (result.stopReason) {
    return null;
  }
  const raw = JSON.stringify(result);
  if (raw === "{}") {
    return null;
  }
  return { kind: "agent", text: raw };
}

module.exports = { activityFrom, readTurn, replyText, contentText, permissionResponse };
