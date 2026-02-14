const BASE = "/api";

async function request(method, path, body) {
  const opts = {
    method,
    headers: { "Content-Type": "application/json" },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(`${BASE}${path}`, opts);
  const data = await resp.json();
  if (!resp.ok) {
    throw new Error(data.error || `HTTP ${resp.status}`);
  }
  return data;
}

export function listDir(path, archived = false) {
  const qs = archived ? "?archived=true" : "";
  return request("GET", `/fs/${path}${qs}`);
}

export function readFile(path) {
  return request("GET", `/fs/${path}`);
}

export function writeFile(path, content) {
  return request("PUT", `/fs/${path}`, { content });
}

export function deleteFile(path) {
  return request("DELETE", `/fs/${path}`);
}

export function createDir(path) {
  return request("POST", `/fs/${path}`, { type: "directory" });
}

export function archiveFile(path) {
  return request("POST", `/archive/${path}`);
}

export function unarchiveFile(path) {
  return request("POST", `/unarchive/${path}`);
}

export function createJournal(path, content) {
  return request("POST", `/journal/${path}`, { content });
}

export function appendJournal(path, content) {
  return request("POST", `/journal/${path}/append`, { content });
}

export function search(query, { prefix = "" } = {}) {
  let url = `/search?q=${encodeURIComponent(query)}`;
  if (prefix) {
    url += `&prefix=${encodeURIComponent(prefix)}`;
  }
  return request("GET", url);
}

export async function fetchAgentSnippet(path) {
  const qs = path ? `?path=${encodeURIComponent(path)}` : "";
  const resp = await fetch(`${BASE}/agent/snippet${qs}`);
  return resp.text();
}
