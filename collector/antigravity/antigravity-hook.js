const fs = require("node:fs");

// ============ 改这里（直接改下面四个值；若设了同名环境变量，环境变量优先）============
const DEVICE_ID = "desk-light-01";               // 这盏灯/这个用户的唯一 id；同一用户的所有工具填同一个，不同用户填不同的
const SERVER_URL = "http://light.woogua.com";       // server 地址
const COLLECTOR_TOKEN = "d00cb656a261c4dc14040d0154475aca5315e4aa58cbb217"; // 上报 token，要和 server 的 Collector token 一致
const POST_TIMEOUT_MS = 800;                     // POST 超时(ms)，server 长期不开可调小（如 200）
// ==============================================================================

const SOURCE = "antigravity";

const ANTIGRAVITY_HOOK_EVENTS = new Set([
  "PreToolUse",
  "PostToolUse",
  "PreInvocation",
  "PostInvocation",
  "Stop"
]);

// 拦截及审批规则定义（Antigravity 会读 stdout 的 decision 来决定是否弹审批）
const APPROVAL_TOOLS = {
  run_command: "ask",
  ask_permission: "force_ask",
  write_to_file: "ask",
  replace_file_content: "ask",
  multi_replace_file_content: "ask",
  search_web: "ask",
  read_url_content: "ask"
};

function readJsonFromStdin() {
  const raw = fs.readFileSync(0, "utf8");

  if (!raw.trim()) {
    return {
      hook_error: "Hook stdin is empty"
    };
  }

  try {
    return JSON.parse(raw);
  } catch (error) {
    return {
      hook_error: `Hook stdin JSON parse failed: ${error.message}`,
      raw_stdin: raw
    };
  }
}

// 同时产出状态灯用的 {state,message} 和 Antigravity 用的 decision。
// 这段逻辑必须完整保留：decision 经 stdout 回给 Antigravity 做审批拦截。
function detectStateAndDecision(eventName, input) {
  let state = "thinking";
  let message = "Antigravity 正在思考";
  let decision = null;

  switch (eventName) {
    case "PreInvocation":
      state = "thinking";
      message = "Antigravity 正在思考";
      decision = {};
      break;

    case "PostInvocation":
      state = "thinking";
      message = "Antigravity 工具调用阶段完成";
      decision = {};
      break;

    case "PreToolUse": {
      const toolCallName = input.toolCall?.name;
      const decisionType = APPROVAL_TOOLS[toolCallName];

      if (decisionType) {
        state = "approval";
        message = `Antigravity 等待工具执行审批: ${toolCallName}`;
        decision = {
          decision: decisionType,
          reason: `需要用户审批以执行高危工具: ${toolCallName}`
        };
      } else {
        state = "busy";
        message = `Antigravity 正在执行工具: ${toolCallName || "Unknown"}`;
        decision = { decision: "allow" };
      }
      break;
    }

    case "PostToolUse":
      state = "thinking";
      message = input.error
        ? "Antigravity 工具执行失败，正在处理"
        : "Antigravity 工具执行完成，正在判断下一步";
      decision = {};
      break;

    case "Stop": {
      const isFullyIdle = input.fullyIdle ?? true;
      const reason = input.terminationReason || "";
      const isCancelled = /cancel|interrupt|abort/i.test(reason);

      if (isCancelled) {
        state = "idle";
        message = `Antigravity 被手动停止 (${reason})`;
      } else if (isFullyIdle) {
        state = "idle";
        message = "Antigravity 空闲";
      } else {
        state = "busy";
        message = "Antigravity 仍有后台任务在运行";
      }
      decision = { decision: "" };
      break;
    }

    default:
      state = "thinking";
      message = `Antigravity 状态更新: ${eventName}`;
      decision = {};
      break;
  }

  return { state, message, decision };
}

function compactToolArgs(args) {
  if (!args || typeof args !== "object") {
    return null;
  }

  const keys = [
    "CommandLine",
    "Cwd",
    "AbsolutePath",
    "DirectoryPath",
    "SearchDirectory",
    "SearchPath",
    "Query",
    "Pattern",
    "TargetFile",
    "Action",
    "Target",
    "Reason",
    "Url",
    "query"
  ];
  const compacted = {};

  for (const key of keys) {
    if (args[key] !== undefined) {
      compacted[key] = args[key];
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

// state：粗粒度 4 态，喂红绿灯。event/details：保留 Antigravity 细状态给其他程序。
function buildEvent(detected, eventName, input) {
  const toolCall = input.toolCall || null;

  return {
    source: SOURCE,
    state: detected.state,
    event: eventName,
    message: detected.message,
    details: {
      hookEventName: eventName,
      knownAntigravityEvent: ANTIGRAVITY_HOOK_EVENTS.has(eventName),
      toolName: toolCall?.name || null,
      toolArgs: compactToolArgs(toolCall?.args),
      stepIdx: input.stepIdx ?? null,
      invocationNum: input.invocationNum ?? null,
      initialNumSteps: input.initialNumSteps ?? null,
      conversationId: input.conversationId || null,
      workspacePaths: Array.isArray(input.workspacePaths) ? input.workspacePaths : null,
      transcriptPath: input.transcriptPath || null,
      artifactDirectoryPath: input.artifactDirectoryPath || null,
      error: input.error || null,
      fullyIdle: input.fullyIdle ?? null,
      terminationReason: input.terminationReason || null,
      executionNum: input.executionNum ?? null,
      decision: detected.decision || null,
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
  const eventName = process.argv[2] || "Unknown";
  const input = readJsonFromStdin();
  const detected = detectStateAndDecision(eventName, input);
  const event = buildEvent(detected, eventName, input);

  try {
    await postEvent(event);
  } catch {
    // 上报失败静默丢弃，不落盘；但仍要输出 decision 给 Antigravity。
  }

  // 输出 Antigravity 预期的决策 JSON 到 stdout（审批拦截依赖它）。
  if (detected.decision !== null) {
    process.stdout.write(JSON.stringify(detected.decision));
  }
}

if (require.main === module) {
  main()
    .then(() => {
      process.exit(0);
    })
    .catch(() => {
      // 极端异常兜底：仍要给 Antigravity 一个安全 decision，绝不阻断它。
      const eventName = process.argv[2] || "Unknown";
      let fallbackDecision = {};
      if (eventName === "PreToolUse") {
        fallbackDecision = { decision: "allow" };
      } else if (eventName === "Stop") {
        fallbackDecision = { decision: "" };
      }

      try {
        process.stdout.write(JSON.stringify(fallbackDecision));
      } catch (_) {}

      process.exit(0);
    });
}

module.exports = {
  buildEvent,
  compactJsonValue,
  compactToolArgs,
  detectStateAndDecision,
  readJsonFromStdin
};
