import {
  createMCPToken,
  listMCPTokens,
  listTrustedDevices,
  MCPTokenRecord,
  revokeMCPToken,
  revokeTrustedDevice,
  trustDevice,
  TrustedDeviceRecord,
} from "./api.js";
import { showAlert, showConfirm, showPrompt } from "./dialog.js";

const tokenCreated = document.getElementById("mcp-token-created")!;
const tokenValue = document.getElementById("mcp-token-value")!;
const tokenList = document.getElementById("mcp-token-list")!;
const trustedCurrent = document.getElementById("trusted-device-current")!;
const trustedList = document.getElementById("trusted-device-list")!;
const jsonConfig = document.getElementById("mcp-json-config")!;
const claudeConfig = document.getElementById("mcp-claude-config")!;
const curlConfig = document.getElementById("mcp-curl-config")!;
const agentsPrompt = document.getElementById("mcp-agents-prompt")!;
const managementTokenInput = document.getElementById("mcp-management-token") as HTMLInputElement;
const btnRefresh = document.getElementById("btn-mcp-refresh")!;
const btnNewToken = document.getElementById("btn-mcp-new-token")!;
const btnSaveManagementToken = document.getElementById("btn-mcp-save-management-token")!;
const btnCopyToken = document.getElementById("btn-mcp-copy-token")!;
const btnCopyJSON = document.getElementById("btn-mcp-copy-json")!;
const btnCopyClaude = document.getElementById("btn-mcp-copy-claude")!;
const btnCopyCurl = document.getElementById("btn-mcp-copy-curl")!;
const btnCopyAgentsPrompt = document.getElementById("btn-mcp-copy-agents-prompt")!;
const btnTrustedDeviceAdd = document.getElementById("btn-trusted-device-add")!;

let latestToken = "";
let managementToken = sessionStorage.getItem("duffel:mcp-management-token") || "";
let authEnabled = true;

export async function showMCPConnector(): Promise<void> {
  authEnabled = await detectAuthEnabled();
  syncAuthControls();
  managementTokenInput.value = managementToken;
  renderInstructions(authEnabled);
  await Promise.all([refreshTokens(), refreshTrustedDevices()]);
}

async function detectAuthEnabled(): Promise<boolean> {
  try {
    const resp = await fetch("/oauth/setup");
    return resp.status !== 404;
  } catch {
    return true;
  }
}

function syncAuthControls(): void {
  managementTokenInput.disabled = !authEnabled;
  btnSaveManagementToken.toggleAttribute("disabled", !authEnabled);
  btnNewToken.toggleAttribute("disabled", !authEnabled);
  btnTrustedDeviceAdd.toggleAttribute("disabled", !authEnabled);
  if (!authEnabled) {
    saveManagementToken("");
  }
}

function renderInstructions(withAuth: boolean): void {
  const baseURL = window.location.origin;
  const endpoint = `${baseURL}/mcp`;
  const trustedServerConfig: Record<string, unknown> = {
    type: "streamable-http",
    url: endpoint,
  };
  const bearerServerConfig: Record<string, unknown> = {
    ...trustedServerConfig,
    headers: {
      Authorization: "Bearer ${DUFFEL_MCP_TOKEN}",
    },
  };

  const openAIConfigs = [
    "Trusted Tailscale device:",
    JSON.stringify({
      mcpServers: {
        duffel: trustedServerConfig,
      },
    }, null, 2),
  ];
  if (withAuth) {
    openAIConfigs.push("", "Other devices with bearer token:", JSON.stringify({
      mcpServers: {
        duffel: bearerServerConfig,
      },
    }, null, 2),
    );
  }
  jsonConfig.textContent = openAIConfigs.join("\n");

  const claudeTrusted = {
    type: "http",
    url: endpoint,
  };
  const claudeBearer = {
    ...claudeTrusted,
    headers: {
      Authorization: "Bearer ${DUFFEL_MCP_TOKEN}",
    },
  };
  const claudeConfigs = [
    "Trusted Tailscale device:",
    `claude mcp add-json duffel '${JSON.stringify(claudeTrusted)}'`,
    "",
    "claude_desktop_config.json:",
    JSON.stringify({ mcpServers: { duffel: claudeTrusted } }, null, 2),
  ];
  if (withAuth) {
    claudeConfigs.push(
      "",
      "Other devices with bearer token:",
      `claude mcp add-json duffel '${JSON.stringify(claudeBearer)}'`,
      "",
      "claude_desktop_config.json:",
      JSON.stringify({ mcpServers: { duffel: claudeBearer } }, null, 2),
    );
  }
  claudeConfig.textContent = claudeConfigs.join("\n");
  const lines = [
    `curl -s ${shellQuote(endpoint)} \\`,
    "  -H 'Content-Type: application/json' \\",
  ];
  if (withAuth) {
    lines.push("  -H 'Authorization: Bearer ${DUFFEL_MCP_TOKEN}' \\");
  }
  lines.push("  --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\",\"params\":{}}'");
  curlConfig.textContent = lines.join("\n");
  agentsPrompt.textContent = buildAgentsPrompt();
}

function buildAgentsPrompt(): string {
  return `## Duffel Workspace Usage

Use the Duffel MCP server for project memory, planning artifacts, research notes, and changelog entries when it is available.

- Before starting a new task, use Duffel search when relevant to find prior plans, research, decisions, or journal entries that may affect the work.
- Store plans, implementation plans, migration plans, rollout plans, and similar planning artifacts in a \`plans/\` subfolder. Write these plan artifacts as HTML files unless the user explicitly asks for another format.
- When executing a plan, record any meaningful divergence from the original plan in Duffel. Include what changed, why it changed, and whether follow-up work remains.
- Use a Duffel journal file for change history. Append concise entries for completed changes, deployments, important operational discoveries, and user-visible behavior changes.
- Store research artifacts in a dedicated \`research/\` subfolder. Keep research source-faithful and use clear filenames so future agents can retrieve it through Duffel search.
- Use MCP tools such as \`duffel_search\`, \`duffel_read\`, \`duffel_write\`, and \`duffel_journal_append\` for Duffel operations.`;
}

async function refreshTrustedDevices(): Promise<void> {
  trustedList.replaceChildren();
  trustedCurrent.replaceChildren();
  if (!authEnabled) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = "Bearer auth is disabled, so trusted-device bypass is not active.";
    trustedList.appendChild(empty);
    return;
  }

  try {
    const { devices, current } = await listTrustedDevices(managementToken);
    trustedCurrent.textContent = current ? `Current Tailscale IP: ${current}` : "Current request is not from a Tailscale IP.";
    renderTrustedDevices(devices);
  } catch (err) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = `Trusted devices unavailable: ${(err as Error).message}`;
    trustedList.appendChild(empty);
  }
}

function renderTrustedDevices(devices: TrustedDeviceRecord[]): void {
  trustedList.replaceChildren();
  const activeDevices = devices.filter((device) => !device.revoked);
  if (activeDevices.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = "No trusted Tailscale devices.";
    trustedList.appendChild(empty);
    return;
  }

  activeDevices.forEach((device) => {
    const row = document.createElement("div");
    row.className = "token-row";

    const details = document.createElement("div");
    details.className = "token-details";

    const title = document.createElement("div");
    title.className = "token-title";
    title.textContent = device.label || device.ip;

    const meta = document.createElement("div");
    meta.className = "token-meta";
    meta.textContent = [`ip ${device.ip}`, `created ${formatDateTime(device.created_at)}`].join(" | ");

    details.appendChild(title);
    details.appendChild(meta);

    const revoke = document.createElement("button");
    revoke.className = "danger";
    revoke.textContent = "Revoke";
    revoke.addEventListener("click", async () => {
      if (!await showConfirm(`Revoke trusted device "${device.label || device.ip}"?`, { danger: true, confirmLabel: "Revoke" })) {
        return;
      }
      try {
        await revokeTrustedDevice(device.ip, managementToken);
        await refreshTrustedDevices();
      } catch (err) {
        await showAlert(`Revoke failed: ${(err as Error).message}`);
      }
    });

    row.appendChild(details);
    row.appendChild(revoke);
    trustedList.appendChild(row);
  });
}

async function refreshTokens(): Promise<void> {
  tokenList.replaceChildren();
  if (!authEnabled) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = "Bearer auth is disabled. MCP clients should connect without a token.";
    tokenList.appendChild(empty);
    return;
  }

  const loading = document.createElement("p");
  loading.className = "empty-state";
  loading.textContent = "Loading tokens...";
  tokenList.appendChild(loading);

  try {
    const { tokens } = await listMCPTokens(managementToken);
    renderTokens(tokens);
  } catch (err) {
    tokenList.replaceChildren();
    const empty = document.createElement("p");
    empty.className = "empty-state";
    const message = (err as Error).message;
    empty.textContent = message === "auth disabled"
      ? "Bearer auth is disabled. MCP clients should connect without a token."
      : message === "missing bearer token"
      ? "Paste a management token, or create a new token from a trusted Tailscale device."
      : `Token management unavailable: ${message}`;
    tokenList.appendChild(empty);
  }
}

function renderTokens(tokens: MCPTokenRecord[]): void {
  tokenList.replaceChildren();
  const activeTokens = tokens.filter((token) => !token.revoked);
  if (activeTokens.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = "No active MCP tokens.";
    tokenList.appendChild(empty);
    return;
  }

  activeTokens.forEach((token) => {
    const row = document.createElement("div");
    row.className = "token-row";

    const details = document.createElement("div");
    details.className = "token-details";

    const title = document.createElement("div");
    title.className = "token-title";
    title.textContent = token.label || "MCP token";

    const meta = document.createElement("div");
    meta.className = "token-meta";
    meta.textContent = [
      `id ${shortTokenID(token.id)}`,
      `scope ${token.scope}`,
      `created ${formatDateTime(token.created_at)}`,
    ].join(" | ");

    details.appendChild(title);
    details.appendChild(meta);

    const revoke = document.createElement("button");
    revoke.className = "danger";
    revoke.textContent = "Revoke";
    revoke.addEventListener("click", async () => {
      if (!await showConfirm(`Revoke "${token.label || shortTokenID(token.id)}"?`, { danger: true, confirmLabel: "Revoke" })) {
        return;
      }
      try {
        await revokeMCPToken(token.id, managementToken);
        await refreshTokens();
      } catch (err) {
        await showAlert(`Revoke failed: ${(err as Error).message}`);
      }
    });

    row.appendChild(details);
    row.appendChild(revoke);
    tokenList.appendChild(row);
  });
}

function shortTokenID(id: string): string {
  if (id.length <= 12) return id;
  return `${id.slice(0, 6)}...${id.slice(-6)}`;
}

function formatDateTime(raw: string): string {
  const dt = new Date(raw);
  if (Number.isNaN(dt.getTime())) return raw;
  return dt.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

async function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Fall through to the textarea fallback for browsers that expose the
      // Clipboard API but deny writes in the current context.
    }
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  document.execCommand("copy");
  document.body.removeChild(ta);
}

function flashCopied(button: Element): void {
  const original = button.textContent || "Copy";
  button.textContent = "Copied";
  setTimeout(() => { button.textContent = original; }, 1600);
}

btnRefresh.addEventListener("click", () => {
  refreshTokens();
});

btnSaveManagementToken.addEventListener("click", async () => {
  managementToken = managementTokenInput.value.trim();
  saveManagementToken(managementToken);
  await Promise.all([refreshTokens(), refreshTrustedDevices()]);
});

btnNewToken.addEventListener("click", async () => {
  const label = await showPrompt("Token label:", { placeholder: "Codex desktop" });
  if (label == null) return;
  try {
    const created = await createMCPToken(label, managementToken);
    latestToken = created.token;
    managementToken = latestToken;
    managementTokenInput.value = managementToken;
    saveManagementToken(managementToken);
    tokenCreated.classList.remove("hidden");
    tokenValue.textContent = latestToken;
    await Promise.all([refreshTokens(), refreshTrustedDevices()]);
  } catch (err) {
    await showAlert(`Create token failed: ${(err as Error).message}`);
  }
});

btnCopyToken.addEventListener("click", async () => {
  if (!latestToken) return;
  await copyToClipboard(latestToken);
  flashCopied(btnCopyToken);
});

btnCopyJSON.addEventListener("click", async () => {
  await copyToClipboard(jsonConfig.textContent || "");
  flashCopied(btnCopyJSON);
});

btnCopyCurl.addEventListener("click", async () => {
  await copyToClipboard(curlConfig.textContent || "");
  flashCopied(btnCopyCurl);
});

btnCopyAgentsPrompt.addEventListener("click", async () => {
  await copyToClipboard(agentsPrompt.textContent || "");
  flashCopied(btnCopyAgentsPrompt);
});

function saveManagementToken(token: string): void {
  if (token) {
    sessionStorage.setItem("duffel:mcp-management-token", token);
    sessionStorage.setItem("duffel:api-token", token);
  } else {
    sessionStorage.removeItem("duffel:mcp-management-token");
    sessionStorage.removeItem("duffel:api-token");
  }
  window.dispatchEvent(new CustomEvent("duffel-auth-token-changed"));
}

btnTrustedDeviceAdd.addEventListener("click", async () => {
  const label = await showPrompt("Trusted device label:", { placeholder: "Jibran laptop" });
  if (label == null) return;
  try {
    await trustDevice("", label, managementToken);
    await refreshTrustedDevices();
  } catch (err) {
    await showAlert(`Trust device failed: ${(err as Error).message}`);
  }
});

btnCopyClaude.addEventListener("click", async () => {
  await copyToClipboard(claudeConfig.textContent || "");
  flashCopied(btnCopyClaude);
});
