const test = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./chat-ui");
const { chatPage } = require("./chat-page");

test("token counter matches the composer scale", () => {
  assert.equal(ui.formatTokens(0), "0");
  assert.equal(ui.formatTokens(150000), "150K");
  assert.equal(ui.formatTokens(500000), "500K");
  assert.equal(ui.formatTokens(1500), "1.5K");
  assert.equal(ui.formatUsage(0, 500000), "0K/500K");
  assert.equal(ui.formatUsage(1500, 500000), "1.5K/500K");
  assert.equal(ui.formatUsage(0, 200000), "0K/200K");
  assert.equal(ui.defaultContextWindow("grok"), 500000);
  assert.equal(ui.defaultContextWindow("claude"), 200000);
  assert.equal(ui.defaultContextWindow("codex"), 200000);
});

test("markdown keeps text and escapes markup", () => {
  const html = ui.renderMarkdown("Смотри **LICENSE** и `file`\n\n- один\n- <script>");
  assert.match(html, /<strong>LICENSE<\/strong>/);
  assert.match(html, /<code>file<\/code>/);
  assert.match(html, /<li>один<\/li>/);
  assert.equal(html.includes("<script>"), false);
  assert.match(html, /&lt;script&gt;/);
});

test("tool summary reads like the activity row", () => {
  const text = ui.summarizeTools([
    { kind: "read", text: "open a" },
    { kind: "search", text: "find b" },
    { kind: "edit", text: "write c" },
    { kind: "execute", text: "npm test" },
  ]);
  assert.equal(text, "Explored 2 items, generated 1 item, ran 1 command");
});

test("chatPage renders Remote LLM welcome screen, header bar, and composer", () => {
  const html = chatPage();
  assert.match(html, /<h1 class="welcome-title">Remote LLM<\/h1>/);
  assert.equal(html.includes("previously Grok Build"), false);
  assert.equal(html.includes("AFK Pilot"), false);
  assert.match(html, /id="session-title">New session<\/div>/);
  assert.match(html, /placeholder="Ask\.\.\."/);
  assert.equal(html.includes("Ask Grok"), false);
  assert.match(html, /id="usage">0K\/500K<\/span>/);
  assert.match(html, /id="active-file"/);
  assert.match(html, /flex-wrap: nowrap/);
  assert.match(html, /class="file-count">1</);
  assert.match(html, /id="history-btn"/);
  assert.equal(html.includes("Connect Codex or Claude Code"), false);
  assert.equal(html.includes("tip-banner"), false);
});

