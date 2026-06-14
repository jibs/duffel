export interface DirEntry {
  name: string;
  isDir: boolean;
  isJournal: boolean;
  kind: FileKind;
  size: number;
  modTime: string;
}

export interface DirResponse {
  entries: DirEntry[];
}

export interface FileResponse {
  path: string;
  content: string;
  modTime: string;
  size: number;
  isJournal: boolean;
  kind: FileKind;
  recommended: RecommendedContent[];
}

export type FileKind = "directory" | "journal" | "markdown" | "html" | "text" | "image";

export interface RecommendedContent {
  path: string;
  title: string;
  snippet: string;
  score: number;
  modified_at: string;
  line?: number;
  context?: string;
}

export interface SearchResult {
  path: string;
  file: string;
  title: string;
  snippet: string;
  content: string;
  score: number;
  modified_at: string;
  line?: number;
  context?: string;
  explain?: unknown;
}

export interface SearchOptions {
  limit?: number;
  offset?: number;
  intent?: string;
  candidate_limit?: number;
  min_score?: number;
  explain?: boolean;
}

export interface MCPTokenRecord {
  id: string;
  label: string;
  client_id: string;
  scope: string;
  created_at: string;
  expires_at?: string;
  revoked: boolean;
}

export interface MCPTokenListResponse {
  tokens: MCPTokenRecord[];
}

export interface MCPTokenCreateResponse {
  token: string;
  record: MCPTokenRecord;
}

export interface TrustedDeviceRecord {
  ip: string;
  label: string;
  created_at: string;
  revoked: boolean;
}

export interface TrustedDeviceListResponse {
  devices: TrustedDeviceRecord[];
  current: string;
}

export interface TrustedDeviceCreateResponse {
  device: TrustedDeviceRecord;
}

const BASE = "/api";
const BEARER_TOKEN_KEYS = ["duffel:mcp-management-token", "duffel:api-token"];

type ApiError = {
  error?: string;
};

function bearerHeader(bearerToken = ""): Record<string, string> {
  const token = bearerToken || storedBearerToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

// parseJSONResponse reads a fetch Response as JSON, throwing the server-provided
// error message on a non-2xx status. Shared by all JSON-returning API calls.
async function parseJSONResponse<T>(resp: Response): Promise<T> {
  const text = await resp.text();
  let data: unknown = {};
  if (text) {
    try {
      data = JSON.parse(text) as unknown;
    } catch {
      data = { error: text.trim() || `HTTP ${resp.status}` };
    }
  }
  if (!resp.ok) {
    throw new Error((data as ApiError).error || `HTTP ${resp.status}`);
  }
  return data as T;
}

async function request<T>(method: string, path: string, body?: unknown, bearerToken = ""): Promise<T> {
  const opts: RequestInit = {
    method,
    headers: { "Content-Type": "application/json", ...bearerHeader(bearerToken) },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  return parseJSONResponse<T>(await fetch(`${BASE}${path}`, opts));
}

function storedBearerToken(): string {
  try {
    for (const key of BEARER_TOKEN_KEYS) {
      const token = sessionStorage.getItem(key);
      if (token) return token;
    }
  } catch {
    return "";
  }
  return "";
}

export function listDir(path: string, archived = false): Promise<DirResponse> {
  const qs = archived ? "?archived=true" : "";
  return request<DirResponse>("GET", `/fs/${path}${qs}`);
}

export function readFile(path: string): Promise<FileResponse> {
  return request<FileResponse>("GET", `/fs/${path}`);
}

export function writeFile(path: string, content: string): Promise<void> {
  return request<void>("PUT", `/fs/${path}`, { content });
}

/** Uploads raw image bytes to the workspace at `path`. */
export async function uploadImage(path: string, blob: Blob): Promise<FileResponse> {
  const resp = await fetch(`${BASE}/fs/${path}`, {
    method: "PUT",
    headers: { "Content-Type": blob.type || "application/octet-stream", ...bearerHeader() },
    body: blob,
  });
  return parseJSONResponse<FileResponse>(resp);
}

/** Fetches the raw bytes of a file (e.g. an image) as a Blob, with auth. */
export async function fetchRaw(path: string): Promise<Blob> {
  const resp = await fetch(`${BASE}/fs/${path}?raw=1`, { headers: bearerHeader() });
  if (!resp.ok) {
    let message = `HTTP ${resp.status}`;
    try {
      const data = JSON.parse(await resp.text()) as ApiError;
      if (data.error) message = data.error;
    } catch {
      // Non-JSON error body; fall back to the status code.
    }
    throw new Error(message);
  }
  return resp.blob();
}

export function deleteFile(path: string, recursive = false): Promise<void> {
  const qs = recursive ? "?recursive=true" : "";
  return request<void>("DELETE", `/fs/${path}${qs}`);
}

export function createDir(path: string): Promise<void> {
  return request<void>("POST", `/fs/${path}`, { type: "directory" });
}

export function archiveFile(path: string): Promise<void> {
  return request<void>("POST", `/archive/${path}`);
}

export function unarchiveFile(path: string): Promise<void> {
  return request<void>("POST", `/unarchive/${path}`);
}

export function createJournal(path: string, content: string): Promise<void> {
  return request<void>("POST", `/journal/${path}`, { content });
}

export function appendJournal(path: string, content: string): Promise<void> {
  return request<void>("POST", `/journal/${path}/append`, { content });
}

export function search(query: string, opts: SearchOptions = {}): Promise<SearchResult[]> {
  const params = new URLSearchParams({ q: query });
  if (opts.limit != null) params.set("limit", String(opts.limit));
  if (opts.offset != null) params.set("offset", String(opts.offset));
  if (opts.intent) params.set("intent", opts.intent);
  if (opts.candidate_limit != null) params.set("candidate_limit", String(opts.candidate_limit));
  if (opts.min_score != null) params.set("min_score", String(opts.min_score));
  if (opts.explain != null) params.set("explain", String(opts.explain));
  return request<SearchResult[]>("GET", `/search?${params.toString()}`);
}

export async function fetchAgentSnippet(path: string): Promise<string> {
  const qs = path ? `?path=${encodeURIComponent(path)}` : "";
  const resp = await fetch(`${BASE}/agent/snippet${qs}`);
  return resp.text();
}

export function listMCPTokens(bearerToken = ""): Promise<MCPTokenListResponse> {
  return request<MCPTokenListResponse>("GET", "/mcp/tokens", undefined, bearerToken);
}

export function createMCPToken(label: string, bearerToken = ""): Promise<MCPTokenCreateResponse> {
  return request<MCPTokenCreateResponse>("POST", "/mcp/tokens", { label }, bearerToken);
}

export function revokeMCPToken(id: string, bearerToken = ""): Promise<void> {
  return request<void>("DELETE", `/mcp/tokens/${encodeURIComponent(id)}`, undefined, bearerToken);
}

export function listTrustedDevices(bearerToken = ""): Promise<TrustedDeviceListResponse> {
  return request<TrustedDeviceListResponse>("GET", "/mcp/trusted-devices", undefined, bearerToken);
}

export function trustDevice(ip: string, label: string, bearerToken = ""): Promise<TrustedDeviceCreateResponse> {
  return request<TrustedDeviceCreateResponse>("POST", "/mcp/trusted-devices", { ip, label }, bearerToken);
}

export function revokeTrustedDevice(ip: string, bearerToken = ""): Promise<void> {
  return request<void>("DELETE", `/mcp/trusted-devices/${encodeURIComponent(ip)}`, undefined, bearerToken);
}
