#!/usr/bin/env node
const http = require("node:http");
const fs = require("node:fs");
const path = require("node:path");

const PORT = Number(process.env.PORT || 8765);
const HOST = process.env.HOST || "127.0.0.1";
const DEFAULT_STATUS_URL = "http://light.woogua.com/api/devices/desk-light-01/status";
const HTML_FILE = path.join(__dirname, "light-preview.html");

function send(res, statusCode, contentType, body) {
  res.writeHead(statusCode, {
    "content-type": contentType,
    "cache-control": "no-store"
  });
  res.end(body);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    let body = "";
    req.setEncoding("utf8");
    req.on("data", chunk => {
      body += chunk;
      if (body.length > 100_000) {
        reject(new Error("request body too large"));
        req.destroy();
      }
    });
    req.on("end", () => resolve(body));
    req.on("error", reject);
  });
}

async function proxyStatus(req, res) {
  let payload = {};
  try {
    payload = JSON.parse(await readBody(req) || "{}");
  } catch {
    send(res, 400, "application/json; charset=utf-8", JSON.stringify({ ok: false, error: "invalid json" }));
    return;
  }

  const statusUrl = String(payload.url || DEFAULT_STATUS_URL);
  const token = String(payload.token || "");
  if (!token) {
    send(res, 400, "application/json; charset=utf-8", JSON.stringify({ ok: false, error: "missing token" }));
    return;
  }

  try {
    const upstream = await fetch(statusUrl, {
      headers: { authorization: `Bearer ${token}` },
      cache: "no-store"
    });
    const text = await upstream.text();
    res.writeHead(upstream.status, {
      "content-type": upstream.headers.get("content-type") || "application/json; charset=utf-8",
      "cache-control": "no-store"
    });
    res.end(text);
  } catch (error) {
    send(res, 502, "application/json; charset=utf-8", JSON.stringify({ ok: false, error: String(error.message || error) }));
  }
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || HOST}`);
  if (req.method === "GET" && (url.pathname === "/" || url.pathname === "/light-preview.html")) {
    send(res, 200, "text/html; charset=utf-8", fs.readFileSync(HTML_FILE, "utf8"));
    return;
  }
  if (req.method === "POST" && url.pathname === "/proxy/status") {
    proxyStatus(req, res);
    return;
  }
  send(res, 404, "application/json; charset=utf-8", JSON.stringify({ ok: false, error: "not found" }));
});

server.listen(PORT, HOST, () => {
  console.log(`Agent Light preview: http://${HOST}:${PORT}/`);
});
