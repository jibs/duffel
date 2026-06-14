import {
  readFile,
  listDir,
  deleteFile,
  archiveFile,
  createDir,
  fetchAgentSnippet,
  fetchRaw,
  FileResponse,
  RecommendedContent,
} from "./api.js";
import { loadTree, highlightActive } from "./browser.js";
import { checkEditorBackendChange, openEditor } from "./editor.js";
import { showJournal } from "./journal.js";
import { showMCPConnector } from "./mcp.js";
import { attachHTMLFrameLinkBridge, clearImageCache, parseRouteHash, renderFileContent, routeHash, scrollHTMLFrameToFragment, scrollToFragment } from "./render.js";
import { isSearchVisible, refreshSearch } from "./search.js";
import { showAlert, showConfirm, showPrompt } from "./dialog.js";

type ViewName = "file" | "editor" | "journal" | "dir" | "search" | "mcp" | "image" | "empty";

const views: Record<ViewName, HTMLElement> = {
  file: document.getElementById("view-file")!,
  editor: document.getElementById("view-editor")!,
  journal: document.getElementById("view-journal")!,
  dir: document.getElementById("view-dir")!,
  search: document.getElementById("view-search")!,
  mcp: document.getElementById("view-mcp")!,
  image: document.getElementById("view-image")!,
  empty: document.getElementById("view-empty")!,
};

const breadcrumb = document.getElementById("breadcrumb")!;
const fileRendered = document.getElementById("file-rendered")!;
const fileRecommended = document.getElementById("file-recommended")!;
const fileRecommendedList = document.getElementById("file-recommended-list")!;
const dirListing = document.getElementById("dir-listing")!;
const btnEdit = document.getElementById("btn-edit")!;
const btnArchive = document.getElementById("btn-archive")!;
const btnDelete = document.getElementById("btn-delete")!;
const btnNewFile = document.getElementById("btn-new-file")!;
const btnNewFolder = document.getElementById("btn-new-folder")!;
const btnAgentSnippet = document.getElementById("btn-agent-snippet")!;
const imageViewImg = document.getElementById("image-view-img") as HTMLImageElement;
const btnImageDownload = document.getElementById("btn-image-download") as HTMLAnchorElement;
const btnImageArchive = document.getElementById("btn-image-archive")!;
const btnImageDelete = document.getElementById("btn-image-delete")!;

let currentFile: FileResponse | null = null;
let cleanupHTMLFrameLinkBridge: (() => void) | null = null;
let activeImageObjectURL: string | null = null;

function showView(name: ViewName): void {
  Object.entries(views).forEach(([key, el]) => {
    el.classList.toggle("hidden", key !== name);
  });
}

function cleanupRenderedFile(): void {
  cleanupHTMLFrameLinkBridge?.();
  cleanupHTMLFrameLinkBridge = null;
  if (activeImageObjectURL) {
    URL.revokeObjectURL(activeImageObjectURL);
    activeImageObjectURL = null;
  }
}

function updateBreadcrumb(path: string): void {
  breadcrumb.innerHTML = "";
  if (!path || path === "/") {
    breadcrumb.innerHTML = '<span>~</span>';
    return;
  }
  const parts = path.split("/").filter(Boolean);
  const link = document.createElement("a");
  link.href = "#/";
  link.textContent = "~";
  breadcrumb.appendChild(link);

  let accumulated = "";
  for (const part of parts) {
    const sep = document.createElement("span");
    sep.textContent = " / ";
    breadcrumb.appendChild(sep);

    accumulated += (accumulated ? "/" : "") + part;
    const partLink = document.createElement("a");
    partLink.href = `#/${accumulated}`;
    partLink.textContent = part;
    breadcrumb.appendChild(partLink);
  }
}

async function showFile(path: string, fragment = ""): Promise<void> {
  try {
    const file = await readFile(path);
    currentFile = file;
    updateBreadcrumb(path);

    if (file.kind === "image") {
      cleanupRenderedFile();
      hideFileRecommendations();
      await showImage(file);
    } else if (file.isJournal) {
      cleanupRenderedFile();
      hideFileRecommendations();
      showJournal(path, file.content);
      showView("journal");
    } else {
      cleanupRenderedFile();
      const frame = renderFileContent(fileRendered, file.path, file.content, file.kind, navigateToFirstExisting);
      if (frame) {
        cleanupHTMLFrameLinkBridge = attachHTMLFrameLinkBridge(file.path, navigateToFirstExisting);
      }
      if (fragment) {
        requestAnimationFrame(() => {
          if (frame) {
            scrollHTMLFrameToFragment(frame, fragment);
          } else {
            scrollToFragment(fileRendered, fragment);
          }
        });
      }
      renderFileRecommendations(path, file.recommended || []);
      showView("file");
    }
    highlightActive(path);
  } catch {
    // Maybe it's a directory
    await showDirectory(path);
  }
}

async function showImage(file: FileResponse): Promise<void> {
  btnImageDownload.setAttribute("download", file.path.split("/").pop() || "image");
  btnImageDownload.removeAttribute("href");
  try {
    const blob = await fetchRaw(file.path);
    activeImageObjectURL = URL.createObjectURL(blob);
    imageViewImg.src = activeImageObjectURL;
    imageViewImg.alt = file.path;
    // Download from the fetched blob: a plain href to the API would 401 under
    // bearer auth since an <a download> can't attach the Authorization header.
    btnImageDownload.href = activeImageObjectURL;
  } catch (err) {
    imageViewImg.removeAttribute("src");
    imageViewImg.alt = `Failed to load image: ${(err as Error).message}`;
  }
  showView("image");
}

async function navigateToFirstExisting(candidates: string[], fragment: string): Promise<void> {
  for (const candidate of candidates) {
    try {
      await readFile(candidate);
      window.location.hash = routeHash(candidate, fragment);
      return;
    } catch {
      // Try the next extensionless-link candidate.
    }
  }

  window.location.hash = routeHash(candidates[0] || "", fragment);
}

async function showDirectory(path: string): Promise<void> {
  try {
    const dir = await listDir(path);
    currentFile = null;
    cleanupRenderedFile();
    hideFileRecommendations();
    updateBreadcrumb(path);

    const entries = (dir.entries || []).sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    dirListing.replaceChildren();
    if (entries.length === 0) {
      const empty = document.createElement("p");
      empty.className = "empty-state";
      empty.textContent = "Empty folder.";
      dirListing.appendChild(empty);
    } else {
      entries.forEach((e) => {
        const icon = e.isDir ? "📁" : e.isJournal ? "📓" : e.kind === "html" ? "🌐" : e.kind === "image" ? "🖼️" : "📄";
        const fullPath = path ? `${path}/${e.name}` : e.name;
        const size = e.isDir ? "" : formatSize(e.size);
        const updated = e.modTime ? `Last updated ${formatModifiedAt(e.modTime)}` : "";

        const row = document.createElement("div");
        row.className = "dir-entry";
        row.dataset.path = fullPath;

        const iconEl = document.createElement("span");
        iconEl.className = "icon";
        iconEl.textContent = icon;

        const nameEl = document.createElement("span");
        nameEl.className = "name";
        nameEl.textContent = e.name;

        const metaEl = document.createElement("span");
        metaEl.className = "meta";
        metaEl.textContent = [size, updated].filter(Boolean).join(" | ");

        row.appendChild(iconEl);
        row.appendChild(nameEl);
        row.appendChild(metaEl);
        row.addEventListener("click", () => {
          window.location.hash = `#/${fullPath}`;
        });

        dirListing.appendChild(row);
      });
    }
    showView("dir");
    highlightActive(path);
  } catch (err) {
    console.error("Navigation error:", err);
    showView("empty");
  }
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function sanitizeSnippet(raw: string): string {
  return raw.replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;")
    .replace(/&lt;mark&gt;/g, "<mark>")
    .replace(/&lt;\/mark&gt;/g, "</mark>");
}

function formatModifiedAt(raw: string): string {
  const dt = new Date(raw);
  if (Number.isNaN(dt.getTime())) return raw;
  const now = new Date();
  const diffDays = Math.floor((now.getTime() - dt.getTime()) / 86400000);
  if (diffDays === 0) return "Today";
  if (diffDays === 1) return "Yesterday";
  if (diffDays < 7) return `${diffDays}d ago`;
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}w ago`;
  return dt.toLocaleDateString(undefined, { month: "short", day: "numeric", year: diffDays > 365 ? "numeric" : undefined });
}

function renderFileRecommendations(currentPath: string, recommendations: RecommendedContent[]): void {
  fileRecommended.classList.remove("hidden");
  fileRecommendedList.replaceChildren();

  if (!recommendations || recommendations.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = "No recommendations available yet.";
    fileRecommendedList.appendChild(empty);
    return;
  }

  recommendations.forEach((item) => {
    if (!item.path || item.path === currentPath) return;

    const row = document.createElement("div");
    row.className = "recommended-item";

    const title = document.createElement("div");
    title.className = "title";
    title.textContent = item.title || item.path.split("/").pop() || item.path;

    const path = document.createElement("div");
    path.className = "path";
    path.textContent = item.path;

    const meta = document.createElement("div");
    meta.className = "meta-row";
    if (item.score != null) {
      const score = document.createElement("span");
      score.className = "score";
      score.textContent = `Score ${item.score.toFixed(2)}`;
      meta.appendChild(score);
    }
    if (item.modified_at) {
      const date = document.createElement("span");
      date.className = "date";
      date.textContent = formatModifiedAt(item.modified_at);
      meta.appendChild(date);
    }
    if (item.line && item.line > 0) {
      const line = document.createElement("span");
      line.className = "line";
      line.textContent = `Line ${item.line}`;
      meta.appendChild(line);
    }
    if (item.context) {
      const context = document.createElement("span");
      context.className = "context";
      context.textContent = item.context;
      meta.appendChild(context);
    }

    const snippet = document.createElement("div");
    snippet.className = "snippet";
    snippet.innerHTML = sanitizeSnippet(item.snippet || "");

    row.appendChild(title);
    row.appendChild(path);
    if (meta.children.length > 0) row.appendChild(meta);
    row.appendChild(snippet);
    row.addEventListener("click", () => {
      window.location.hash = `#/${item.path}`;
    });

    fileRecommendedList.appendChild(row);
  });

  if (fileRecommendedList.children.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty-state";
    empty.textContent = "No recommendations available yet.";
    fileRecommendedList.appendChild(empty);
  }
}

function hideFileRecommendations(): void {
  fileRecommended.classList.add("hidden");
  fileRecommendedList.replaceChildren();
}

export function navigate(hash: string): void {
  window.location.hash = hash;
}

async function handleRoute(): Promise<void> {
  const hash = window.location.hash || "#/";
  const route = parseRouteHash(hash);
  const path = route.path;

  if (path === "_mcp") {
    currentFile = null;
    cleanupRenderedFile();
    hideFileRecommendations();
    updateBreadcrumb("MCP Connector");
    showView("mcp");
    await showMCPConnector();
    highlightActive("");
    return;
  }

  if (!path) {
    updateBreadcrumb("");
    await showDirectory("");
    return;
  }

  if (route.edit) {
    hideFileRecommendations();
    const filePath = path;
    try {
      const file = await readFile(filePath);
      updateBreadcrumb(filePath);
      openEditor(filePath, file);
      showView("editor");
    } catch {
      // New file
      updateBreadcrumb(filePath);
      openEditor(filePath, null);
      showView("editor");
    }
    return;
  }

  await showFile(path, route.fragment);
}

function connectRealtimeUpdates(): void {
  let refreshTimer: number | undefined;

  const scheduleRefresh = (): void => {
    if (refreshTimer !== undefined) {
      window.clearTimeout(refreshTimer);
    }
    refreshTimer = window.setTimeout(async () => {
      refreshTimer = undefined;
      // Workspace content changed: drop cached image object URLs so re-uploads
      // to the same path render fresh and URLs don't accumulate.
      clearImageCache();
      await loadTree();
      if (isSearchVisible()) {
        refreshSearch();
        return;
      }

      const route = parseRouteHash(window.location.hash || "#/");
      if (route.edit) {
        await checkEditorBackendChange();
        return;
      }
      await handleRoute();
    }, 150);
  };

  const events = new EventSource("/api/events");
  events.addEventListener("content-changed", scheduleRefresh);
  events.onerror = () => {
    console.warn("Realtime update stream disconnected; the browser will retry automatically.");
  };
}

// Button handlers
btnEdit.addEventListener("click", () => {
  if (currentFile) {
    window.location.hash = `#/${currentFile.path}?edit`;
  }
});

async function archiveCurrentFile(): Promise<void> {
  if (!currentFile) return;
  if (!await showConfirm(`Archive "${currentFile.path}"?`, { confirmLabel: "Archive" })) return;
  try {
    await archiveFile(currentFile.path);
    loadTree();
    window.location.hash = "#/";
  } catch (err) {
    await showAlert(`Archive failed: ${(err as Error).message}`);
  }
}

async function deleteCurrentFile(): Promise<void> {
  if (!currentFile) return;
  if (!await showConfirm(`Delete "${currentFile.path}"? This cannot be undone.`, { danger: true })) return;
  try {
    await deleteFile(currentFile.path);
    loadTree();
    window.location.hash = "#/";
  } catch (err) {
    await showAlert(`Delete failed: ${(err as Error).message}`);
  }
}

btnArchive.addEventListener("click", archiveCurrentFile);
btnDelete.addEventListener("click", deleteCurrentFile);
btnImageArchive.addEventListener("click", archiveCurrentFile);
btnImageDelete.addEventListener("click", deleteCurrentFile);

btnNewFile.addEventListener("click", async () => {
  const name = await showPrompt("New file name:", { placeholder: "notes.md" });
  if (!name) return;
  const hash = window.location.hash || "#/";
  const currentPath = parseRouteHash(hash).path;
  const fullPath = currentPath ? `${currentPath}/${name}` : name;
  window.location.hash = `#/${fullPath}?edit`;
});

function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    return navigator.clipboard.writeText(text);
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  document.execCommand("copy");
  document.body.removeChild(ta);
  return Promise.resolve();
}

function showSnippetModal(snippet: string): void {
  const overlay = document.createElement("div");
  overlay.className = "modal-overlay";
  overlay.innerHTML = `
    <div class="modal snippet-modal">
      <div class="snippet-header">
        <h3>MCP Snippet</h3>
        <button class="snippet-close-btn" title="Close">&times;</button>
      </div>
      <div class="snippet-code-wrap">
        <pre class="snippet-code"></pre>
      </div>
      <div class="modal-buttons">
        <button class="snippet-copy-btn primary">Copy</button>
      </div>
    </div>`;
  (overlay.querySelector(".snippet-code") as HTMLPreElement).textContent = snippet;
  const copyBtn = overlay.querySelector(".snippet-copy-btn")!;
  copyBtn.addEventListener("click", async () => {
    await copyToClipboard(snippet);
    copyBtn.textContent = "Copied!";
    copyBtn.classList.add("copied");
    setTimeout(() => { copyBtn.textContent = "Copy"; copyBtn.classList.remove("copied"); }, 2000);
  });
  overlay.querySelector(".snippet-close-btn")!.addEventListener("click", () => overlay.remove());
  overlay.addEventListener("click", (e) => { if (e.target === overlay) overlay.remove(); });
  document.body.appendChild(overlay);
}

btnAgentSnippet.addEventListener("click", async () => {
  const hash = window.location.hash || "#/";
  const path = parseRouteHash(hash).path;
  try {
    const snippet = await fetchAgentSnippet(path || "");
    showSnippetModal(snippet);
  } catch (err) {
    showSnippetModal(`Error fetching snippet: ${(err as Error).message}`);
  }
});

btnNewFolder.addEventListener("click", async () => {
  const name = await showPrompt("New folder name:", { placeholder: "my-folder" });
  if (!name) return;
  const hash = window.location.hash || "#/";
  const currentPath = parseRouteHash(hash).path;
  const fullPath = currentPath ? `${currentPath}/${name}` : name;
  try {
    await createDir(fullPath);
    loadTree();
    window.location.hash = `#/${fullPath}`;
  } catch (err) {
    await showAlert(`Create folder failed: ${(err as Error).message}`);
  }
});

// Init
window.addEventListener("hashchange", handleRoute);
window.addEventListener("duffel-auth-token-changed", () => {
  loadTree();
  handleRoute();
});
window.addEventListener("DOMContentLoaded", () => {
  loadTree();
  handleRoute();
  connectRealtimeUpdates();
});
