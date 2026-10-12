const test = require("node:test");
const assert = require("node:assert/strict");
const sync = require("./sync");

test("safeRelative rejects escapes", () => {
  assert.equal(sync.safeRelative("../x"), null);
  assert.equal(sync.safeRelative(".git/config"), null);
  assert.equal(sync.safeRelative("src/main.js"), "src/main.js");
});

test("binary file is base64 and planApply restores it", () => {
  const body = Buffer.from([0xff, 0xfe, 0x00]);
  const file = sync.fileFromBuffer("bin.dat", body);
  assert.equal(file.encoding, "base64");
  const plan = sync.planApply([file, { path: "old.txt", deleted: true }]);
  assert.deepEqual(plan.deletes, ["old.txt"]);
  assert.equal(plan.writes[0].path, "bin.dat");
  assert.deepEqual(plan.writes[0].body, body);
});

test("planApply rejects a parent path", () => {
  assert.throws(() => sync.planApply([{ path: "../x", content: "a" }]), /запрещ/);
});
