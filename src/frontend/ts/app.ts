import {
  readFile,
  listDir,
  deleteFile,
  archiveFile,
  createDir,
  fetchAgentSnippet,
  FileResponse,
  RecommendedContent,
} from "./api.js";
import { loadTree, highlightActive } from "./browser.js";
import { openEditor } from "./editor.js";
import { showJournal } from "./journal.js";
import { sanitizeHTML } from "./sanitize.js";
import "./search.js";

type ViewName = "file" | "editor" | "journal" | "dir" | "search" | "empty";

const views: Record<ViewName, HTMLElement> = {
  file: document.getElementById("view-file")!,
  editor: document.getElementById("view-editor")!,
  journal: document.getElementById("view-journal")!,
  dir: document.getElementById("view-dir")!,
  search: document.getElementById("view-search")!,
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

let currentFile: FileResponse | null = null;

function showView(name: ViewName): void {
  Object.entries(views).forEach(([key, el]) => {
    el.classList.toggle("hidden", key !== name);
  });
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

async function showFile(path: string): Promise<void> {
  try {
    const file = await readFile(path);
    currentFile = file;
    updateBreadcrumb(path);

    if (file.isJournal) {
      hideFileRecommendations();
      showJournal(path, file.content);
      showView("journal");
    } else {
      if (typeof marked !== "undefined") {
        // Convert [[wiki-links]] to standard markdown links before parsing
        const content = file.content.replace(/\[\[([^\]]+)\]\]/g, (_: string, target: string) => {
          const label = target.split("/").pop()!.replace(/\.md$/, "").replace(/-/g, " ");
          return `[${label}](${target})`;
        });
        fileRendered.innerHTML = sanitizeHTML(marked.parse(content));
        // Intercept internal links so they use hash-based routing
        fileRendered.querySelectorAll("a").forEach((a) => {
          const href = a.getAttribute("href");
          if (!href || href.startsWith("http://") || href.startsWith("https://") || href.startsWith("#")) return;
          a.addEventListener("click", (e) => {
            e.preventDefault();
            // Resolve relative links against the current file's directory
            const dir = path.includes("/") ? path.substring(0, path.lastIndexOf("/")) : "";
            const resolved = dir ? `${dir}/${href}` : href;
            window.location.hash = `#/${resolved}`;
          });
        });
      } else {
        fileRendered.textContent = file.content;
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

async function showDirectory(path: string): Promise<void> {
  try {
    const dir = await listDir(path);
    currentFile = null;
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
        const icon = e.isDir ? "📁" : e.isJournal ? "📓" : "📄";
        const fullPath = path ? `${path}/${e.name}` : e.name;
        const size = e.isDir ? "" : formatSize(e.size);

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
        metaEl.textContent = size;

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
      score.textContent = item.score.toFixed(2);
      meta.appendChild(score);
    }
    if (item.modified_at) {
      const date = document.createElement("span");
      date.className = "date";
      date.textContent = item.modified_at;
      meta.appendChild(date);
    }

    const snippet = document.createElement("div");
    snippet.className = "snippet";
    snippet.textContent = item.snippet || "";

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
  const path = hash.replace(/^#\/?/, "");

  if (!path) {
    updateBreadcrumb("");
    await showDirectory("");
    return;
  }

  // Check if path ends with ?edit
  if (path.endsWith("?edit")) {
    hideFileRecommendations();
    const filePath = path.replace(/\?edit$/, "");
    try {
      const file = await readFile(filePath);
      updateBreadcrumb(filePath);
      openEditor(filePath, file.content);
      showView("editor");
    } catch {
      // New file
      updateBreadcrumb(filePath);
      openEditor(filePath, "");
      showView("editor");
    }
    return;
  }

  await showFile(path);
}

// Button handlers
btnEdit.addEventListener("click", () => {
  if (currentFile) {
    window.location.hash = `#/${currentFile.path}?edit`;
  }
});

btnArchive.addEventListener("click", async () => {
  if (!currentFile) return;
  if (!confirm(`Archive "${currentFile.path}"?`)) return;
  try {
    await archiveFile(currentFile.path);
    loadTree();
    window.location.hash = "#/";
  } catch (err) {
    alert(`Archive failed: ${(err as Error).message}`);
  }
});

btnDelete.addEventListener("click", async () => {
  if (!currentFile) return;
  if (!confirm(`Delete "${currentFile.path}"? This cannot be undone.`)) return;
  try {
    await deleteFile(currentFile.path);
    loadTree();
    window.location.hash = "#/";
  } catch (err) {
    alert(`Delete failed: ${(err as Error).message}`);
  }
});

btnNewFile.addEventListener("click", () => {
  const name = prompt("File name (e.g. notes.md):");
  if (!name) return;
  const hash = window.location.hash || "#/";
  const currentPath = hash.replace(/^#\/?/, "").replace(/\?.*$/, "");
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
        <h3>Agent Snippet</h3>
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
  const path = hash.replace(/^#\/?/, "").replace(/\?.*$/, "");
  try {
    const snippet = await fetchAgentSnippet(path || "");
    showSnippetModal(snippet);
  } catch (err) {
    showSnippetModal(`Error fetching snippet: ${(err as Error).message}`);
  }
});

btnNewFolder.addEventListener("click", async () => {
  const name = prompt("Folder name:");
  if (!name) return;
  const hash = window.location.hash || "#/";
  const currentPath = hash.replace(/^#\/?/, "").replace(/\?.*$/, "");
  const fullPath = currentPath ? `${currentPath}/${name}` : name;
  try {
    await createDir(fullPath);
    loadTree();
    window.location.hash = `#/${fullPath}`;
  } catch (err) {
    alert(`Create folder failed: ${(err as Error).message}`);
  }
});

// Init
window.addEventListener("hashchange", handleRoute);
window.addEventListener("DOMContentLoaded", () => {
  loadTree();
  handleRoute();
});
