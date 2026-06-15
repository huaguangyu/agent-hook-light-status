const fs = require("node:fs");

// ============ 改这里（直接改下面四个值；若设了同名环境变量，环境变量优先）============
const DEVICE_ID = "desk-light-01";               // 这盏灯/这个用户的唯一 id；同一用户的所有工具填同一个，不同用户填不同的
const SERVER_URL = "http://127.0.0.1:4318";       // server 地址
const COLLECTOR_TOKEN = "replace-with-collector-token"; // 上报 token，要和 server 的 Collector token 一致
const POST_TIMEOUT_MS = 800;                     // POST 超时(ms)，server 长期不开可调小（如 200）
// ==============================================================================

const SOURCE = "claude-code";

const CLAUDE_CODE_HOOK_EVENTS = new Set([
  "SessionStart",
  "Setup",
  "UserPromptSubmit",
  "UserPromptExpansion",
  "PreToolUse",
  "PermissionRequest",
  "PermissionDenied",
  "PostToolUse",
  "PostToolUseFailure",
  "PostToolBatch",
  "Notification",
  "MessageDisplay",
  "SubagentStart",
  "SubagentStop",
  "TaskCreated",
  "TaskCompleted",
  "Stop",
  "StopFailure",
  "TeammateIdle",
  "InstructionsLoaded",
  "ConfigChange",
  "CwdChanged",
  "FileChanged",
  "WorktreeCreate",
  "WorktreeRemove",
  "PreCompact",
  "PostCompact",
  "Elicitation",
  "ElicitationResult",
  "SessionEnd"
]);

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
      hook_event_name: "Unknown",
      hook_error: `Hook stdin JSON parse failed: ${error.message}`,
      raw_stdin: raw
    };
  }
}

function notificationNeedsAttention(input) {
  const notificationType = getNotificationType(input);
  if (["permission_prompt", "idle_prompt", "elicitation_dialog"].includes(notificationType)) {
    return true;
  }

  if (["auth_success", "elicitation_complete", "elicitation_response"].includes(notificationType)) {
    return false;
  }

  const message = [
    input.message,
    input.title,
    notificationType,
    input.reason
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();

  return /permission|approval|approve|confirm|input|idle|waiting|需要|审批|确认|等待/.test(message);
}

function notificationMessage(input) {
  const notificationType = getNotificationType(input);

  switch (notificationType) {
    case "permission_prompt":
      return "Claude Code 等待工具审批";
    case "idle_prompt":
      return "Claude Code 已完成，等待你的下一步";
    case "elicitation_dialog":
      return "Claude Code MCP 请求用户输入";
    case "auth_success":
      return "Claude Code 认证完成";
    case "elicitation_complete":
      return "Claude Code MCP 输入流程完成";
    case "elicitation_response":
      return "Claude Code MCP 输入结果已返回";
    default:
      return notificationNeedsAttention(input)
        ? "Claude Code 需要你处理通知"
        : "Claude Code 通知更新";
  }
}

function commandFailed(input) {
  const response = input.tool_response || input.tool_result || {};
  const exitCode = response.exit_code ?? response.exitCode ?? input.exit_code;

  return (
    Boolean(input.tool_error) ||
    Boolean(response.error) ||
    response.success === false ||
    input.hook_event_name === "PostToolUseFailure" ||
    input.hook_event_name === "StopFailure" ||
    (typeof exitCode === "number" && exitCode !== 0)
  );
}

function compactToolInput(toolInput) {
  if (!toolInput || typeof toolInput !== "object") {
    return null;
  }

  const keys = [
    "command",
    "description",
    "file_path",
    "path",
    "pattern",
    "url",
    "workdir"
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

function getNotificationType(input) {
  return input.notification_type || input.notificationType || input.type || null;
}

function getSessionStartSource(input) {
  return input.source || input.session_source || input.sessionSource || null;
}

function getMatcherValue(input) {
  return (
    input.matcher ||
    input.notification_type ||
    input.notificationType ||
    input.error_type ||
    input.errorType ||
    input.source ||
    input.reason ||
    input.agent_type ||
    input.agentType ||
    null
  );
}

function firstString(...values) {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return null;
}

function shortText(value, maxLength = 80) {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.replace(/\s+/g, " ").trim();
  if (!normalized) {
    return null;
  }

  return normalized.length > maxLength ? `${normalized.slice(0, maxLength)}...` : normalized;
}

function withSuffix(message, value, wrapper = ": ") {
  return value ? `${message}${wrapper}${value}` : message;
}

function getToolName(input) {
  return firstString(input.tool_name, input.toolName, input.tool);
}

function getNotificationText(input) {
  return shortText(firstString(input.message, input.title, input.reason));
}

function getTaskLabel(input) {
  return shortText(firstString(
    input.task_title,
    input.taskTitle,
    input.task_name,
    input.taskName,
    input.title,
    input.name,
    input.task_id,
    input.taskId
  ));
}

function getChangedFile(input) {
  return firstString(
    input.file_path,
    input.filePath,
    input.filename,
    input.file,
    input.path,
    input.matcher
  );
}

function getWorktreePath(input) {
  return firstString(input.worktree_path, input.worktreePath, input.path, input.cwd);
}

// 把 hook 事件映射成 4 个统一状态；event/details 保留更细的 Claude Code 状态。
function detectState(input) {
  const eventName = input.hook_event_name || "Unknown";
  const toolName = getToolName(input);

  switch (eventName) {
    case "SessionStart":
      return { state: "idle", message: `Claude Code 会话已开始${getSessionStartSource(input) ? ` (${getSessionStartSource(input)})` : ""}` };

    case "Setup":
      return { state: "thinking", message: `Claude Code 正在初始化${getMatcherValue(input) ? ` (${getMatcherValue(input)})` : ""}` };

    case "MessageDisplay":
      return { state: "thinking", message: "Claude Code 正在显示回复" };

    case "UserPromptSubmit":
      return { state: "thinking", message: "Claude Code 正在思考" };

    case "UserPromptExpansion":
      return { state: "thinking", message: `Claude Code 正在展开指令${getMatcherValue(input) ? `: ${getMatcherValue(input)}` : ""}` };

    case "PreToolUse":
      return {
        state: "busy",
        message: `Claude Code 正在执行工具: ${toolName || "Unknown"}`
      };

    case "PostToolUse":
    case "PostToolUseFailure":
      return {
        state: "thinking",
        message: withSuffix(
          commandFailed(input)
            ? "Claude Code 工具执行失败，正在处理"
            : "Claude Code 工具执行完成，正在判断下一步",
          toolName
        )
      };

    case "PermissionRequest":
      return { state: "approval", message: withSuffix("Claude Code 需要审批", toolName) };

    case "PermissionDenied":
      return { state: "thinking", message: withSuffix("Claude Code 权限被拒，正在调整", toolName) };

    case "Notification":
      return {
        state: notificationNeedsAttention(input) ? "approval" : "thinking",
        message: withSuffix(notificationMessage(input), getNotificationText(input))
      };

    case "PostToolBatch":
      return { state: "thinking", message: "Claude Code 工具批次完成，正在继续处理" };

    case "SubagentStart":
      return { state: "busy", message: `Claude Code 子任务正在运行${getMatcherValue(input) ? `: ${getMatcherValue(input)}` : ""}` };

    case "SubagentStop":
      return { state: "thinking", message: `Claude Code 子任务完成，正在整理${getMatcherValue(input) ? `: ${getMatcherValue(input)}` : ""}` };

    case "TaskCreated":
      return { state: "busy", message: withSuffix("Claude Code 任务已创建", getTaskLabel(input)) };

    case "TaskCompleted":
      return { state: "thinking", message: withSuffix("Claude Code 任务已完成，正在整理", getTaskLabel(input)) };

    case "Stop":
      return { state: "idle", message: "Claude Code 空闲" };

    case "StopFailure":
      return { state: "idle", message: `Claude Code 异常结束，已停止运行${getMatcherValue(input) ? ` (${getMatcherValue(input)})` : ""}` };

    case "TeammateIdle":
      return { state: "idle", message: "Claude Code 队友空闲" };

    case "InstructionsLoaded":
      return { state: "thinking", message: `Claude Code 已加载指令${getMatcherValue(input) ? ` (${getMatcherValue(input)})` : ""}` };

    case "ConfigChange":
      return { state: "thinking", message: `Claude Code 配置已变更${getMatcherValue(input) ? `: ${getMatcherValue(input)}` : ""}` };

    case "CwdChanged":
      return { state: "thinking", message: `Claude Code 工作目录已变更${input.cwd ? `: ${input.cwd}` : ""}` };

    case "FileChanged":
      return { state: "thinking", message: withSuffix("Claude Code 监听文件已变更", getChangedFile(input)) };

    case "WorktreeCreate":
      return { state: "busy", message: withSuffix("Claude Code 正在创建 worktree", getWorktreePath(input)) };

    case "WorktreeRemove":
      return { state: "busy", message: withSuffix("Claude Code 正在移除 worktree", getWorktreePath(input)) };

    case "PreCompact":
      return { state: "thinking", message: `Claude Code 正在压缩上下文${getMatcherValue(input) ? ` (${getMatcherValue(input)})` : ""}` };

    case "PostCompact":
      return { state: "thinking", message: `Claude Code 已完成上下文压缩${getMatcherValue(input) ? ` (${getMatcherValue(input)})` : ""}` };

    case "Elicitation":
      return { state: "approval", message: "Claude Code MCP 需要用户输入" };

    case "ElicitationResult":
      return { state: "thinking", message: "Claude Code MCP 用户输入已返回" };

    case "SessionEnd":
      return { state: "idle", message: `Claude Code 会话已结束${getMatcherValue(input) ? ` (${getMatcherValue(input)})` : ""}` };

    default:
      return { state: "thinking", message: `Claude Code 状态更新: ${eventName}` };
  }
}

// state：粗粒度 4 态，喂红绿灯。
// event/details：保留原始 hook 事件和细节，给需要精确状态的其他程序（灯不读它）。
// 时间戳由 server 盖。
function buildEvent(input) {
  const detected = detectState(input);
  return {
    source: SOURCE,
    state: detected.state,
    event: input.hook_event_name || "Unknown",
    message: detected.message,
    details: {
      sessionId: input.session_id || input.sessionId || null,
      cwd: input.cwd || null,
      transcriptPath: input.transcript_path || input.transcriptPath || null,
      prompt: input.prompt || null,
      toolName: getToolName(input),
      toolInput: compactToolInput(input.tool_input || input.toolInput),
      notificationType: getNotificationType(input),
      notificationText: getNotificationText(input),
      sessionStartSource: getSessionStartSource(input),
      matcherValue: getMatcherValue(input),
      taskLabel: getTaskLabel(input),
      changedFile: getChangedFile(input),
      worktreePath: getWorktreePath(input),
      errorType: input.error_type || input.errorType || null,
      error: input.error || input.hook_error || null,
      reason: input.reason || null,
      rawHook: compactJsonValue(input),
      knownClaudeCodeEvent: CLAUDE_CODE_HOOK_EVENTS.has(input.hook_event_name || "Unknown")
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

// 上报失败或任何异常都静默丢弃：collector 绝不阻断 agent，也不落盘。
if (require.main === module) {
  main()
    .catch(() => {})
    .finally(() => process.exit(0));
}

module.exports = {
  buildEvent,
  commandFailed,
  compactJsonValue,
  compactToolInput,
  detectState,
  notificationNeedsAttention,
  readJsonFromStdin
};
