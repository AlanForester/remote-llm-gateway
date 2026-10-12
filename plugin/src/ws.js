// A small WebSocket client for the extension host. No npm dependency.
const crypto = require("crypto");
const http = require("http");
const https = require("https");

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

function acceptKey(key) {
  return crypto.createHash("sha1").update(key + GUID).digest("base64");
}

function encodeClientFrame(text, mask) {
  const payload = Buffer.from(String(text), "utf8");
  const maskBytes = mask || crypto.randomBytes(4);
  return encodeFrame(0x1, payload, maskBytes);
}

function encodeFrame(opcode, payload, maskBytes) {
  let header;
  if (payload.length < 126) {
    header = Buffer.alloc(2);
    header[1] = 0x80 | payload.length;
  } else if (payload.length <= 0xffff) {
    header = Buffer.alloc(4);
    header[1] = 0x80 | 126;
    header.writeUInt16BE(payload.length, 2);
  } else {
    header = Buffer.alloc(10);
    header[1] = 0x80 | 127;
    header.writeBigUInt64BE(BigInt(payload.length), 2);
  }
  header[0] = 0x80 | opcode;
  const masked = Buffer.alloc(payload.length);
  for (let i = 0; i < payload.length; i++) {
    masked[i] = payload[i] ^ maskBytes[i % 4];
  }
  return Buffer.concat([header, maskBytes, masked]);
}

function decodeServerFrames(buffer) {
  const messages = [];
  let offset = 0;
  while (offset + 2 <= buffer.length) {
    const b0 = buffer[offset];
    const b1 = buffer[offset + 1];
    const opcode = b0 & 0x0f;
    const fin = (b0 & 0x80) !== 0;
    let len = b1 & 0x7f;
    let pos = offset + 2;
    if (len === 126) {
      if (pos + 2 > buffer.length) {
        break;
      }
      len = buffer.readUInt16BE(pos);
      pos += 2;
    } else if (len === 127) {
      if (pos + 8 > buffer.length) {
        break;
      }
      const big = buffer.readBigUInt64BE(pos);
      if (big > BigInt(8 * 1024 * 1024)) {
        throw new Error("кадр слишком большой");
      }
      len = Number(big);
      pos += 8;
    }
    const masked = (b1 & 0x80) !== 0;
    let mask;
    if (masked) {
      if (pos + 4 > buffer.length) {
        break;
      }
      mask = buffer.subarray(pos, pos + 4);
      pos += 4;
    }
    if (pos + len > buffer.length) {
      break;
    }
    const payload = Buffer.from(buffer.subarray(pos, pos + len));
    if (masked) {
      for (let i = 0; i < payload.length; i++) {
        payload[i] ^= mask[i % 4];
      }
    }
    offset = pos + len;
    messages.push({ fin, opcode, payload, close: opcode === 0x8, ping: opcode === 0x9 });
  }
  return { messages, rest: Buffer.from(buffer.subarray(offset)) };
}

function connect(url) {
  const target = new URL(url);
  const secure = target.protocol === "wss:" || target.protocol === "https:";
  const lib = secure ? https : http;
  const key = crypto.randomBytes(16).toString("base64");
  return new Promise((resolve, reject) => {
    const req = lib.request({
      protocol: secure ? "https:" : "http:",
      hostname: target.hostname,
      port: target.port || (secure ? 443 : 80),
      path: target.pathname + target.search,
      headers: {
        Connection: "Upgrade",
        Upgrade: "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key": key,
      },
    });
    req.on("upgrade", (res, socket) => {
      if (res.headers["sec-websocket-accept"] !== acceptKey(key)) {
        socket.destroy();
        reject(new Error("неверный ответ WebSocket"));
        return;
      }
      resolve(wrap(socket));
    });
    req.on("response", (res) => {
      reject(new Error("шлюз ответил " + res.statusCode));
    });
    req.on("error", reject);
    req.end();
  });
}

function wrap(socket) {
  const pending = [];
  let waiter = null;
  let failed = null;
  let buf = Buffer.alloc(0);
  let frag = null;

  function fail(err) {
    if (failed) {
      return;
    }
    failed = err;
    if (waiter) {
      const current = waiter;
      waiter = null;
      current.reject(err);
    }
  }

  function push(text) {
    if (waiter) {
      const current = waiter;
      waiter = null;
      current.resolve(text);
      return;
    }
    pending.push(text);
  }

  socket.on("data", (chunk) => {
    buf = Buffer.concat([buf, chunk]);
    let decoded;
    try {
      decoded = decodeServerFrames(buf);
    } catch (err) {
      fail(err);
      socket.destroy();
      return;
    }
    buf = decoded.rest;
    for (const message of decoded.messages) {
      if (message.ping) {
        socket.write(encodeFrame(0xa, message.payload, crypto.randomBytes(4)));
        continue;
      }
      if (message.close) {
        fail(new Error("соединение закрыто"));
        socket.end();
        return;
      }
      if (message.opcode === 0x1 && message.fin) {
        push(message.payload.toString("utf8"));
        continue;
      }
      if (message.opcode === 0x1) {
        frag = message.payload;
        continue;
      }
      if (message.opcode === 0x0) {
        frag = Buffer.concat([frag || Buffer.alloc(0), message.payload]);
        if (message.fin) {
          push(frag.toString("utf8"));
          frag = null;
        }
      }
    }
  });
  socket.on("error", fail);
  socket.on("end", () => fail(new Error("соединение закрыто")));

  return {
    send(text) {
      socket.write(encodeClientFrame(text));
    },
    next() {
      if (pending.length > 0) {
        return Promise.resolve(pending.shift());
      }
      if (failed) {
        return Promise.reject(failed);
      }
      return new Promise((resolve, reject) => {
        waiter = { resolve, reject };
      });
    },
    close() {
      socket.destroy();
    },
  };
}

module.exports = { acceptKey, encodeClientFrame, decodeServerFrames, connect };
