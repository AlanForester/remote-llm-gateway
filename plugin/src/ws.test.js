const test = require("node:test");
const assert = require("node:assert/strict");
const http = require("http");
const ws = require("./ws");

test("client frame roundtrip and echo", async () => {
  const server = http.createServer();
  server.on("upgrade", (req, socket) => {
    const accept = ws.acceptKey(req.headers["sec-websocket-key"]);
    socket.write(
      "HTTP/1.1 101 Switching Protocols\r\n" +
        "Upgrade: websocket\r\n" +
        "Connection: Upgrade\r\n" +
        "Sec-WebSocket-Accept: " +
        accept +
        "\r\n\r\n"
    );
    socket.on("data", (chunk) => {
      const decoded = ws.decodeServerFrames(chunk);
      for (const message of decoded.messages) {
        if (message.opcode !== 0x1) {
          continue;
        }
        const payload = Buffer.from("echo:" + message.payload.toString("utf8"));
        const header = Buffer.from([0x81, payload.length]);
        socket.write(Buffer.concat([header, payload]));
      }
    });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  server.unref();
  const addr = server.address();
  const client = await ws.connect("ws://127.0.0.1:" + addr.port + "/ws/test");
  client.send("привет");
  const reply = await client.next();
  assert.equal(reply, "echo:привет");
  client.close();
  server.closeAllConnections();
  server.close();
});
