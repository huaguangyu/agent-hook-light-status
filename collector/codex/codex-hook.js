const fs = require("node:fs");

// ============ 改这里（直接改下面四个值；若设了同名环境变量，环境变量优先）============
const DEVICE_ID = "desk-light-01";               // 这盏灯/这个用户的唯一 id；同一用户的所有工具填同一个，不同用户填不同的
const SERVER_URL = "http://127.0.0.1:4318";      // server 地址；远程部署时改成你的服务端地址
const COLLECTOR_TOKEN = "请替换为你的-collector-token"; // 上报 token，要和 server/env.json 的 collectorToken 一致
const POST_TIMEOUT_MS = 800;                     // POST 超时(ms)，server 长期不开可调小（如 200）
// ==============================================================================

const SOURCE = "codex";

const CODEX_HOOK_EVENTS = new Set([
  "SessionStart",
  "SubagentStart",
  "PreToolUse",
  "PermissionRequest",
  "PostToolUse",
  "PreCompact",
  "PostCompact",
  "UserPromptSubmit",
  "SubagentStop",
  "Stop"
]);

const STATE_BY_EVENT = {
  SessionStart: { state: "idle", message: "Codex 会话已开始" },
  SubagentStart: { state: "busy", message: "Codex 子任务正在运行" },
  UserPromptSubmit: { state: "thinking", message: "Codex 正在思考" },
  PreToolUse: { state: "busy", message: "Codex 正在动手" },
  PermissionRequest: { state: "approval", message: "Codex 需要审批" },
  PreCompact: { state: "thinking", message: "Codex 正在压缩上下文" },
  PostCompact: { state: "thinking", message: "Codex 已完成上下文压缩" },
  SubagentStop: { state: "thinking", message: "Codex 子任务完成，正在整理" },
  Stop: { state: "idle", message: "Codex 空闲" }
};

function readJsonFromStdin() {
  const raw = fs.readFileSync(0, "utf8");

  if (!raw.trim()) {
    return {
      hook_event_name: "Unknown",
      hook_error: "Hook stdin is empty"
    };
  }

  try {
    return JSON.parse(raw);
  } catch (error) {
    return {
      hook_event_name: process.argv[2] || "Unknown",
      hook_error: `Hook stdin JSON parse failed: ${error.message}`,
      raw_stdin: raw
    };
  }
}

function commandFailed(input) {
  const response = input.tool_response || input.toolResponse || input.tool_result || {};
  const exitCode = response.exit_code ?? response.exitCode ?? input.exit_code;

  return (
    Boolean(input.tool_error) ||
    Boolean(response.error) ||
    response.success === false ||
    (typeof exitCode === "number" && exitCode !== 0)
  );
}

function detectState(input) {
  const hookEventName = getHookEventName(input);

  if (hookEventName === "PostToolUse") {
    return {
      state: "thinking",
      message: commandFailed(input) ? "Codex 工具执行失败，正在处理" : "Codex 正在思考"
    };
  }

  return STATE_BY_EVENT[hookEventName] || { state: "thinking", message: "Codex 正在思考" };
}

function compactToolInput(toolInput) {
  if (!toolInput || typeof toolInput !== "object") {
    return null;
  }

  const keys = [
    "command",
    "cmd",
    "description",
    "file_path",
    "path",
    "pattern",
    "url",
    "workdir",
    "sandbox_permissions",
    "sandboxPermissions",
    "justification",
    "approval_reason",
    "approvalReason"
  ];
  const compacted = {};

  for (const key of keys) {
    if (toolInput[key] !== undefined) {
      compacted[key] = toolInput[key];
    }
  }

  return Object.keys(compacted).length ? compacted : null;
}

function truncateString(value, maxLength = 4000) {
  if (typeof value !== "string" || value.length <= maxLength) {
    return value;
  }

  return `${value.slice(0, maxLength)}...[truncated ${value.length - maxLength} chars]`;
}

function compactJsonValue(value, depth = 0) {
  if (value === null || value === undefined) {
    return value ?? null;
  }

  if (typeof value === "string") {
    return truncateString(value);
  }

  if (typeof value !== "object") {
    return value;
  }

  if (depth >= 4) {
    return Array.isArray(value) ? `[array:${value.length}]` : "[object]";
  }

  if (Array.isArray(value)) {
    return value.slice(0, 50).map((item) => compactJsonValue(item, depth + 1));
  }

  const compacted = {};
  for (const [key, item] of Object.entries(value)) {
    compacted[key] = compactJsonValue(item, depth + 1);
  }
  return compacted;
}

function compactToolResponse(response) {
  if (!response || typeof response !== "object") {
    return null;
  }

  const keys = [
    "exit_code",
    "exitCode",
    "success",
    "error",
    "duration_ms",
    "durationMs"
  ];
  const compacted = {};

  for (const key of keys) {
    if (response[key] !== undefined) {
      compacted[key] = response[key];
    }
  }

  return Object.keys(compacted).length ? compacted : null;
}

function getHookEventName(input) {
  return input.hook_event_name || input.hookEventName || process.argv[2] || "Unknown";
}

function getMatcherValue(input) {
  return (
    input.matcher ||
    input.tool_name ||
    input.tool ||
    input.source ||
    input.trigger ||
    input.agent_type ||
    input.agentType ||
    null
  );
}

function buildEvent(input) {
  const hookEventName = getHookEventName(input);
  const detected = detectState(input);
  const toolInput = input.tool_input || input.toolInput || null;
  const toolResponse = input.tool_response || input.toolResponse || input.tool_result || null;

  return {
    source: SOURCE,
    state: detected.state,
    event: hookEventName,
    message: detected.message,
    details: {
      hookEventName,
      knownCodexEvent: CODEX_HOOK_EVENTS.has(hookEventName),
      matcherValue: getMatcherValue(input),
      sessionId: input.session_id || input.sessionId || null,
      turnId: input.turn_id || input.turnId || null,
      cwd: input.cwd || null,
      transcriptPath: input.transcript_path || input.transcriptPath || null,
      model: input.model || null,
      permissionMode: input.permission_mode || input.permissionMode || null,
      prompt: input.prompt || null,
      source: input.source || input.session_source || input.sessionSource || null,
      sessionStartSource: input.source || input.session_source || input.sessionSource || null,
      trigger: input.trigger || null,
      toolName: input.tool_name || input.tool || null,
      toolUseId: input.tool_use_id || input.toolUseId || null,
      toolInputDescription: toolInput?.description || null,
      toolInput: compactToolInput(toolInput),
      toolResponse: compactToolResponse(toolResponse),
      toolFailed: commandFailed(input),
      agentId: input.agent_id || input.agentId || null,
      agentType: input.agent_type || input.agentType || null,
      agentTranscriptPath: input.agent_transcript_path || input.agentTranscriptPath || null,
      stopHookActive: input.stop_hook_active ?? input.stopHookActive ?? null,
      lastAssistantMessage: input.last_assistant_message || input.lastAssistantMessage || null,
      error: input.tool_error || toolResponse?.error || input.hook_error || null,
      rawHook: compactJsonValue(input)
    }
  };
}

async function postEvent(event) {
  const baseUrl = process.env.AGENT_LIGHT_SERVER_URL || SERVER_URL;
  const token = process.env.AGENT_LIGHT_COLLECTOR_TOKEN || COLLECTOR_TOKEN;
  const deviceId = process.env.AGENT_LIGHT_DEVICE_ID || DEVICE_ID;
  const timeoutMs = Number(process.env.AGENT_LIGHT_POST_TIMEOUT_MS || POST_TIMEOUT_MS);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(`${baseUrl}/api/devices/${encodeURIComponent(deviceId)}/events`, {
      method: "POST",
      headers: {
        "authorization": `Bearer ${token}`,
        "content-type": "application/json"
      },
      body: JSON.stringify(event),
      signal: controller.signal
    });

    if (!response.ok) {
      const body = await response.text().catch(() => "");
      throw new Error(`Server responded ${response.status}: ${body}`);
    }
  } finally {
    clearTimeout(timeout);
  }
}

async function main() {
  const input = readJsonFromStdin();
  const event = buildEvent(input);

  await postEvent(event);
}

function debugLog(error) {
  const logPath = process.env.AGENT_LIGHT_DEBUG_LOG;
  if (!logPath) {
    return;
  }

  const message = [
    new Date().toISOString(),
    error?.stack || error?.message || String(error)
  ].join(" ");

  fs.appendFileSync(logPath, `${message}\n`);
}

// 上报失败或任何异常都静默丢弃：collector 绝不阻断 agent，也不落盘。
if (require.main === module) {
  main()
    .catch((error) => debugLog(error))
    .finally(() => process.exit(0));
}

module.exports = {
  buildEvent,
  commandFailed,
  compactJsonValue,
  compactToolInput,
  compactToolResponse,
  detectState,
  readJsonFromStdin
};
