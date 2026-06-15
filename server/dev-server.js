const http = require("node:http");

const PORT = Number(process.env.AGENT_LIGHT_PORT || 4318);
const HOST = process.env.AGENT_LIGHT_HOST || "127.0.0.1";
const COLLECTOR_TOKEN = process.env.AGENT_LIGHT_COLLECTOR_TOKEN || "dev-collector-token";
const DEVICE_TOKEN = process.env.AGENT_LIGHT_DEVICE_TOKEN || "dev-device-token";

// 超过这个时间没有新事件，灯自动回到 idle/绿灯。默认 20 分钟，可用环境变量调。
const IDLE_TTL_MS = Number(process.env.AGENT_LIGHT_IDLE_TTL_MS || 20 * 60 * 1000);
const MAX_RECENT_EVENTS = Number(process.env.AGENT_LIGHT_MAX_RECENT_EVENTS || 100);

const LIGHT_EFFECTS = {
  idle: { color: "green", effect: "solid" },
  thinking: { color: "yellow", effect: "breathing" },
  busy: { color: "red", effect: "solid" },
  approval: { color: "red", effect: "fast_blink" }
};

const VALID_STATES = new Set(["idle", "thinking", "busy", "approval"]);

// 每个 device-id（用户/灯）一份状态，互不影响。谁后发谁覆盖。updatedAt 给设备/人看，updatedAtMs 给 TTL 判断。
const devices = new Map();
const recentEvents = new Map();

function formatBeijingTime(date = new Date()) {
  const beijingMs = date.getTime() + 8 * 60 * 60 * 1000;
  return new Date(beijingMs).toISOString().slice(0, 19).replace("T", " ");
}

function publicStatus(status, options = {}) {
  const { updatedAtMs, details, ...rest } = status;
  if (options.includeDetails) {
    return { ...rest, details: details || null };
  }
  return rest;
}

// 读时计算 TTL：超过 IDLE_TTL_MS 没更新 → 强制 idle/绿灯。deviceState 为该 device-id 的状态（可能不存在）。
function resolveStatus(deviceState, options = {}) {
  const now = Date.now();
  const updatedAtMs = deviceState?.updatedAtMs || Date.parse(deviceState?.updatedAt || "");
  const expired = !deviceState || !Number.isFinite(updatedAtMs) || now - updatedAtMs > IDLE_TTL_MS;

  if (expired) {
    return {
      state: "idle",
      color: LIGHT_EFFECTS.idle.color,
      effect: LIGHT_EFFECTS.idle.effect,
      message: deviceState ? "空闲（超时未更新）" : "空闲",
      source: deviceState ? deviceState.source || "unknown" : null,
      event: deviceState ? deviceState.event || null : null,
      updatedAt: deviceState ? deviceState.updatedAt : formatBeijingTime(new Date(now))
    };
  }

  const light = LIGHT_EFFECTS[deviceState.state] || LIGHT_EFFECTS.idle;
  return publicStatus({
    state: deviceState.state,
    color: light.color,
    effect: light.effect,
    message: deviceState.message || "",
    source: deviceState.source || "unknown",
    event: deviceState.event || null,
    details: deviceState.details || null,
    updatedAt: deviceState.updatedAt,
    updatedAtMs: deviceState.updatedAtMs || null
  }, options);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    let body = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => {
      body += chunk;
      if (body.length > 1_000_000) {
        reject(new Error("Request body too large"));
        req.destroy();
      }
    });
    req.on("end", () => resolve(body));
    req.on("error", reject);
  });
}

function sendJson(res, statusCode, data) {
  res.writeHead(statusCode, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store"
  });
  res.end(`${JSON.stringify(data, null, 2)}\n`);
}

function getBearerToken(req) {
  const header = req.headers.authorization || "";
  const match = header.match(/^Bearer\s+(.+)$/i);
  return match ? match[1] : "";
}

function assertCollectorAuth(req) {
  return getBearerToken(req) === COLLECTOR_TOKEN;
}

function assertDeviceAuth(req) {
  return getBearerToken(req) === DEVICE_TOKEN || getBearerToken(req) === COLLECTOR_TOKEN;
}

function rememberEvent(deviceId, event, receivedAt, receivedAtMs) {
  const events = recentEvents.get(deviceId) || [];
  events.unshift({
    state: event.state,
    source: event.source || "unknown",
    event: event.event || null,
    message: event.message || "",
    details: event.details && typeof event.details === "object" ? event.details : null,
    receivedAt,
    receivedAtMs
  });

  if (events.length > MAX_RECENT_EVENTS) {
    events.length = MAX_RECENT_EVENTS;
  }

  recentEvents.set(deviceId, events);
}

async function handlePostEvent(req, res, deviceId) {
  if (!assertCollectorAuth(req)) {
    sendJson(res, 401, { ok: false, error: "unauthorized collector" });
    return;
  }

  let event;
  try {
    event = JSON.parse(await readBody(req));
  } catch (error) {
    sendJson(res, 400, { ok: false, error: "invalid json" });
    return;
  }

  // 只强校验 state；source/message 可选，缺省补默认。
  if (!event || !VALID_STATES.has(event.state)) {
    sendJson(res, 400, { ok: false, error: "state must be idle, thinking, busy, or approval" });
    return;
  }

  const updatedAt = formatBeijingTime();
  const updatedAtMs = Date.now();

  devices.set(deviceId, {
    state: event.state,
    source: event.source || "unknown",
    event: event.event || null,
    message: event.message || "",
    details: event.details && typeof event.details === "object" ? event.details : null,
    updatedAt,
    updatedAtMs
  });
  rememberEvent(deviceId, event, updatedAt, updatedAtMs);

  sendJson(res, 200, { ok: true, deviceId, status: resolveStatus(devices.get(deviceId)) });
}

function handleGetStatus(req, res, deviceId, url) {
  if (!assertDeviceAuth(req)) {
    sendJson(res, 401, { ok: false, error: "unauthorized device" });
    return;
  }
  const includeDetails = url.searchParams.get("details") === "1";
  sendJson(res, 200, resolveStatus(devices.get(deviceId), { includeDetails }));
}

function handleGetEvents(req, res, deviceId, url) {
  if (!assertDeviceAuth(req)) {
    sendJson(res, 401, { ok: false, error: "unauthorized device" });
    return;
  }

  const limit = Math.min(Math.max(Number(url.searchParams.get("limit") || 20), 1), MAX_RECENT_EVENTS);
  const includeDetails = url.searchParams.get("details") === "1";
  const events = (recentEvents.get(deviceId) || []).slice(0, limit).map((event) => {
    if (includeDetails) {
      return event;
    }
    const { details, receivedAtMs, ...rest } = event;
    return rest;
  });

  sendJson(res, 200, { deviceId, events });
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || "127.0.0.1"}`);

  const eventMatch = url.pathname.match(/^\/api\/devices\/([^/]+)\/events$/);
  if (req.method === "POST" && eventMatch) {
    handlePostEvent(req, res, decodeURIComponent(eventMatch[1]));
    return;
  }

  if (req.method === "GET" && eventMatch) {
    handleGetEvents(req, res, decodeURIComponent(eventMatch[1]), url);
    return;
  }

  const statusMatch = url.pathname.match(/^\/api\/devices\/([^/]+)\/status$/);
  if (req.method === "GET" && statusMatch) {
    handleGetStatus(req, res, decodeURIComponent(statusMatch[1]), url);
    return;
  }

  if (req.method === "GET" && url.pathname === "/health") {
    sendJson(res, 200, { ok: true });
    return;
  }

  sendJson(res, 404, { ok: false, error: "not found" });
});

server.listen(PORT, HOST, () => {
  console.log(`Agent Light dev server listening on http://${HOST}:${PORT}`);
  console.log(`Idle TTL: ${IDLE_TTL_MS / 1000}s (超时未更新 → 绿灯)`);
  console.log(`Collector token: ${COLLECTOR_TOKEN}`);
  console.log(`Device token: ${DEVICE_TOKEN}`);
});
