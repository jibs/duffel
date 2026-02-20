export interface DirEntry {
  name: string;
  isDir: boolean;
  isJournal: boolean;
  size: number;
}

export interface DirResponse {
  entries: DirEntry[];
}

export interface FileResponse {
  path: string;
  content: string;
  isJournal: boolean;
}

export interface SearchResult {
  path: string;
  file: string;
  snippet: string;
  content: string;
}

const BASE = "/api";

type ApiError = {
  error?: string;
};

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const opts: RequestInit = {
    method,
    headers: { "Content-Type": "application/json" },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(`${BASE}${path}`, opts);
  const data: unknown = await resp.json();
  if (!resp.ok) {
    const err = data as ApiError;
    throw new Error(err.error || `HTTP ${resp.status}`);
  }
  return data as T;
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

export function deleteFile(path: string): Promise<void> {
  return request<void>("DELETE", `/fs/${path}`);
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

export function search(query: string, { prefix = "" } = {}): Promise<SearchResult[]> {
  let url = `/search?q=${encodeURIComponent(query)}`;
  if (prefix) {
    url += `&prefix=${encodeURIComponent(prefix)}`;
  }
  return request<SearchResult[]>("GET", url);
}

export async function fetchAgentSnippet(path: string): Promise<string> {
  const qs = path ? `?path=${encodeURIComponent(path)}` : "";
  const resp = await fetch(`${BASE}/agent/snippet${qs}`);
  return resp.text();
}
