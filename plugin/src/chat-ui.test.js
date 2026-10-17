const test = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./chat-ui");

test("token counter matches the composer scale", () => {
  assert.equal(ui.formatTokens(0), "0");
  assert.equal(ui.formatTokens(150000), "150K");
  assert.equal(ui.formatTokens(500000), "500K");
  assert.equal(ui.formatTokens(1500), "1.5K");
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
