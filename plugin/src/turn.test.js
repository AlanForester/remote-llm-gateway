const test = require("node:test");
const assert = require("node:assert/strict");
const turn = require("./turn");

test("thought and answer chunks become chat activity", () => {
  const thought = turn.activityFrom({
    method: "session/update",
    params: {
      update: {
        sessionUpdate: "agent_thought_chunk",
        content: { type: "text", text: "сначала проверю" },
      },
    },
  });
  assert.deepEqual(thought, { phase: "thought", text: "сначала проверю" });

  const answer = turn.activityFrom({
    method: "session/update",
    params: {
      update: {
        sessionUpdate: "agent_message_chunk",
        content: { type: "text", text: "готово" },
      },
    },
  });
  assert.equal(answer.phase, "agent");
  assert.equal(answer.text, "готово");
});

test("tool updates keep one id and a readable status", () => {
  const row = turn.activityFrom({
    method: "session/update",
    params: {
      update: {
        sessionUpdate: "tool_call_update",
        toolCallId: "call-1",
        title: "read file",
        status: "in_progress",
      },
    },
  });
  assert.equal(row.id, "call-1");
  assert.equal(row.text, "read file · выполняется");
});

test("readTurn shows updates and then the matching result", async () => {
  const frames = [
    JSON.stringify({
      method: "session/update",
      params: { update: { sessionUpdate: "agent_thought_chunk", content: { type: "text", text: "думаю" } } },
    }),
    JSON.stringify({
      method: "session/update",
      params: { update: { sessionUpdate: "agent_message_chunk", content: { type: "text", text: "ответ" } } },
    }),
    JSON.stringify({ jsonrpc: "2.0", id: 4, result: { stopReason: "end_turn", text: "ответ" } }),
  ];
  const seen = [];
  const outcome = await turn.readTurn(
    {
      next() {
        return Promise.resolve(frames.shift());
      },
    },
    4,
    (activity) => seen.push(activity.phase)
  );
  assert.deepEqual(seen, ["thought", "agent"]);
  assert.equal(outcome.streamed, true);
  assert.equal(outcome.thought, true);
  assert.equal(outcome.msg.result.text, "ответ");
});

test("a gateway error with no id is the turn result", async () => {
  const outcome = await turn.readTurn(
    {
      next() {
        return Promise.resolve(JSON.stringify({ jsonrpc: "2.0", id: null, error: { message: "агент не ответил" } }));
      },
    },
    1,
    () => {}
  );
  assert.deepEqual(turn.replyText(outcome.msg), { kind: "error", text: "агент не ответил" });
});

test("usage update and a permission reply keep their fields", () => {
  const usage = turn.activityFrom({
    method: "session/update",
    params: { update: { sessionUpdate: "usage_update", used: 150000, size: 500000 } },
  });
  assert.equal(usage.phase, "usage");
  assert.equal(usage.used, 150000);
  assert.equal(usage.size, 500000);
  const answer = turn.permissionResponse({ id: 9 }, "once");
  assert.equal(answer.result.outcome.optionId, "once");
  assert.equal(turn.permissionResponse({ id: 9 }, "").result.outcome.outcome, "cancelled");
});

test("end_turn without text is not dumped as JSON", () => {
  assert.equal(turn.replyText({ result: { stopReason: "end_turn" } }), null);
  assert.deepEqual(turn.replyText({ result: { stopReason: "cancelled" } }), {
    kind: "status",
    text: "Остановлено: cancelled",
  });
  assert.deepEqual(turn.replyText({ result: { text: "echo: hi" } }), { kind: "agent", text: "echo: hi" });
});
