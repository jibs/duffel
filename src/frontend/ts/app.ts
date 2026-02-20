import {
  readFile,
  listDir,
  deleteFile,
  archiveFile,
  createDir,
  fetchAgentSnippet,
  FileResponse,
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
    <div class="modal">
      <h3>Agent Snippet</h3>
      <textarea readonly></textarea>
      <div class="modal-buttons">
        <button class="snippet-copy-btn">Copy</button>
        <button class="snippet-close-btn">Close</button>
      </div>
    </div>`;
  (overlay.querySelector("textarea") as HTMLTextAreaElement).value = snippet;
  const copyBtn = overlay.querySelector(".snippet-copy-btn")!;
  copyBtn.addEventListener("click", async () => {
    await copyToClipboard(snippet);
    copyBtn.textContent = "Copied!";
    setTimeout(() => { copyBtn.textContent = "Copy"; }, 2000);
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
