const crypto = require("crypto");

// Helpers for the project snapshot the plugin sends to the gateway.
// Limits match internal/filesync.

const MAX_FILE_BYTES = 1024 * 1024;
const MAX_FILES = 4000;
const MAX_TOTAL_BYTES = 24 * 1024 * 1024;
const MAX_REQUEST_FILES = 500;
const MAX_REQUEST_BYTES = 8 * 1024 * 1024;
const MAX_PROJECT_FILES = 20000;
const MAX_PROJECT_BYTES = 256 * 1024 * 1024;

const SKIP_DIRS = new Set([
  ".git",
  "node_modules",
  "vendor",
  ".next",
  "dist",
  "build",
  "target",
  ".venv",
  "venv",
  "__pycache__",
]);

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

function fileHash(file) {
  return crypto
    .createHash("sha256")
    .update(String(file.encoding || "utf8") + ":" + String(file.content || ""))
    .digest("hex");
}

function hashSnapshot(files) {
  const map = new Map();
  for (const file of files || []) {
    if (file && file.path && !file.deleted) {
      map.set(file.path, fileHash(file));
    }
  }
  return map;
}

function inSkipDir(rel) {
  return String(rel || "")
    .split("/")
    .some((part) => SKIP_DIRS.has(part));
}

function ignoredFromCheck(output) {
  const ignored = new Set();
  for (const part of String(output || "").split("\0")) {
    if (part) {
      ignored.add(part.replace(/\\/g, "/"));
    }
  }
  return ignored;
}

function payloadSize(file) {
  return String((file && file.path) || "").length + String((file && file.content) || "").length + 64;
}

function chunkFiles(files, maxFiles, maxBytes) {
  const limitFiles = maxFiles > 0 ? maxFiles : 1;
  const limitBytes = maxBytes > 0 ? maxBytes : 1;
  const chunks = [];
  let cur = [];
  let size = 0;
  for (const file of files || []) {
    const n = payloadSize(file);
    if (cur.length > 0 && (cur.length >= limitFiles || size + n > limitBytes)) {
      chunks.push(cur);
      cur = [];
      size = 0;
    }
    cur.push(file);
    size += n;
  }
  if (cur.length > 0) {
    chunks.push(cur);
  }
  return chunks;
}

function diffSnapshot(prevMap, nextFiles) {
  const nextMap = new Map();
  const patch = [];
  for (const file of nextFiles || []) {
    if (!file || !file.path) {
      continue;
    }
    const h = fileHash(file);
    nextMap.set(file.path, h);
    if (!prevMap || prevMap.get(file.path) !== h) {
      patch.push(file);
    }
  }
  if (prevMap) {
    for (const oldPath of prevMap.keys()) {
      if (!nextMap.has(oldPath)) {
        patch.push({ path: oldPath, deleted: true });
      }
    }
  }
  return { patch, nextMap };
}

module.exports = {
  MAX_FILE_BYTES,
  MAX_FILES,
  MAX_TOTAL_BYTES,
  MAX_REQUEST_FILES,
  MAX_REQUEST_BYTES,
  MAX_PROJECT_FILES,
  MAX_PROJECT_BYTES,
  safeRelative,
  inSkipDir,
  ignoredFromCheck,
  fileFromBuffer,
  planApply,
  hashSnapshot,
  chunkFiles,
  diffSnapshot,
};
