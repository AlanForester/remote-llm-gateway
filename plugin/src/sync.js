// Helpers for the project snapshot the plugin sends to the gateway.
// Limits match internal/filesync.

const MAX_FILE_BYTES = 1024 * 1024;
const MAX_FILES = 4000;
const MAX_TOTAL_BYTES = 24 * 1024 * 1024;

function safeRelative(rel) {
  const cleaned = String(rel || "")
    .replace(/\\/g, "/")
    .replace(/^\.\/+/, "")
    .replace(/^\/+/, "");
  if (!cleaned || cleaned === "." || cleaned === "..") {
    return null;
  }
  const parts = cleaned.split("/");
  if (parts.some((part) => part === "" || part === "." || part === ".." || part === ".git")) {
    return null;
  }
  return parts.join("/");
}

function fileFromBuffer(rel, body) {
  const path = safeRelative(rel);
  if (!path) {
    throw new Error("путь запрещён");
  }
  const buf = Buffer.from(body);
  if (buf.length > MAX_FILE_BYTES) {
    return null;
  }
  if (Buffer.compare(Buffer.from(buf.toString("utf8"), "utf8"), buf) === 0) {
    return { path, content: buf.toString("utf8") };
  }
  return { path, content: buf.toString("base64"), encoding: "base64" };
}

function bytesOf(file) {
  if (file.encoding === "base64") {
    return Buffer.from(file.content || "", "base64");
  }
  if (file.encoding) {
    throw new Error("путь запрещён");
  }
  return Buffer.from(file.content || "", "utf8");
}

function planApply(files) {
  const writes = [];
  const deletes = [];
  for (const file of files || []) {
    const rel = safeRelative(file.path);
    if (!rel) {
      throw new Error("путь запрещён");
    }
    if (file.deleted) {
      deletes.push(rel);
      continue;
    }
    writes.push({ path: rel, body: bytesOf(file) });
  }
  return { writes, deletes };
}

module.exports = {
  MAX_FILE_BYTES,
  MAX_FILES,
  MAX_TOTAL_BYTES,
  safeRelative,
  fileFromBuffer,
  planApply,
};
