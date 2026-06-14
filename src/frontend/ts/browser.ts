import { deleteFile, listDir } from "./api.js";
import { showAlert, showConfirm } from "./dialog.js";

const fileTree = document.getElementById("file-tree")!;

const currentExpandedPaths = new Set<string>();
const treeContextMenu = createTreeContextMenu();

export async function loadTree(basePath = "", depth = 0): Promise<void> {
  if (depth === 0) {
    fileTree.innerHTML = "";
  }

  try {
    const dir = await listDir(basePath);
    const entries = dir.entries || [];

    // Sort: folders first, then alpha
    entries.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    for (const entry of entries) {
      const fullPath = basePath ? `${basePath}/${entry.name}` : entry.name;
      const item = document.createElement("div");
      item.className = "tree-item";
      item.dataset.path = fullPath;
      item.dataset.isDir = String(entry.isDir);
      item.dataset.isJournal = String(entry.isJournal || false);

      // Indentation
      for (let i = 0; i < depth; i++) {
        const indent = document.createElement("span");
        indent.className = "tree-indent";
        item.appendChild(indent);
      }

      const icon = document.createElement("span");
      icon.className = "icon";
      if (entry.isDir) {
        icon.textContent = currentExpandedPaths.has(fullPath) ? "▾" : "▸";
      } else if (entry.isJournal) {
        icon.textContent = "📓";
      } else if (entry.kind === "html") {
        icon.textContent = "🌐";
      } else if (entry.kind === "image") {
        icon.textContent = "🖼️";
      } else {
        icon.textContent = "📄";
      }
      item.appendChild(icon);

      const name = document.createElement("span");
      name.className = "tree-name";
      name.textContent = entry.name;
      name.title = fullPath;
      item.appendChild(name);

      item.addEventListener("click", (e) => {
        e.stopPropagation();
        hideTreeContextMenu();
        if (entry.isDir) {
          toggleFolder(fullPath, item, depth);
        } else {
          window.location.hash = `#/${fullPath}`;
        }
      });
      item.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        e.stopPropagation();
        showTreeContextMenu(fullPath, entry.isDir, e.clientX, e.clientY);
      });

      fileTree.appendChild(item);

      // If folder was expanded, re-expand it
      if (entry.isDir && currentExpandedPaths.has(fullPath)) {
        await loadTree(fullPath, depth + 1);
      }
    }
  } catch (err) {
    console.error("Failed to load tree:", err);
  }
}

async function toggleFolder(path: string, item: HTMLElement, depth: number): Promise<void> {
  if (currentExpandedPaths.has(path)) {
    currentExpandedPaths.delete(path);
    // Remove children
    removeChildItems(item);
    const icon = item.querySelector(".icon");
    if (icon) icon.textContent = "▸";
  } else {
    currentExpandedPaths.add(path);
    const icon = item.querySelector(".icon");
    if (icon) icon.textContent = "▾";
    // Load children inline
    const dir = await listDir(path);
    const entries = (dir.entries || []).sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });

    let insertAfter: Element = item;
    for (const entry of entries) {
      const fullPath = `${path}/${entry.name}`;
      const child = document.createElement("div");
      child.className = "tree-item";
      child.dataset.path = fullPath;
      child.dataset.isDir = String(entry.isDir);
      child.dataset.parentPath = path;

      for (let i = 0; i <= depth; i++) {
        const indent = document.createElement("span");
        indent.className = "tree-indent";
        child.appendChild(indent);
      }

      const icon = document.createElement("span");
      icon.className = "icon";
      icon.textContent = entry.isDir ? "▸" : entry.isJournal ? "📓" : entry.kind === "html" ? "🌐" : entry.kind === "image" ? "🖼️" : "📄";
      child.appendChild(icon);

      const nameSpan = document.createElement("span");
      nameSpan.className = "tree-name";
      nameSpan.textContent = entry.name;
      nameSpan.title = fullPath;
      child.appendChild(nameSpan);

      child.addEventListener("click", (e) => {
        e.stopPropagation();
        hideTreeContextMenu();
        if (entry.isDir) {
          toggleFolder(fullPath, child, depth + 1);
        } else {
          window.location.hash = `#/${fullPath}`;
        }
      });
      child.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        e.stopPropagation();
        showTreeContextMenu(fullPath, entry.isDir, e.clientX, e.clientY);
      });

      insertAfter.after(child);
      insertAfter = child;
    }
  }
  // Also navigate to folder view
  window.location.hash = `#/${path}`;
}

function removeChildItems(item: HTMLElement): void {
  let next = item.nextElementSibling as HTMLElement | null;
  while (next && next.dataset.parentPath && isChildOf(next, item.dataset.path || "")) {
    const toRemove = next;
    next = next.nextElementSibling as HTMLElement | null;
    toRemove.remove();
  }
}

function isChildOf(element: HTMLElement, parentPath: string): boolean {
  const path = element.dataset.path || "";
  return path.startsWith(parentPath + "/");
}

export function highlightActive(path: string): void {
  fileTree.querySelectorAll(".tree-item").forEach((el) => {
    el.classList.toggle("active", (el as HTMLElement).dataset.path === path);
  });
}

function createTreeContextMenu(): HTMLDivElement {
  const menu = document.createElement("div");
  menu.id = "tree-context-menu";
  menu.className = "hidden";
  menu.setAttribute("role", "menu");

  const deleteButton = document.createElement("button");
  deleteButton.type = "button";
  deleteButton.dataset.action = "delete";
  menu.appendChild(deleteButton);

  document.body.appendChild(menu);
  document.addEventListener("click", hideTreeContextMenu);
  window.addEventListener("blur", hideTreeContextMenu);
  window.addEventListener("resize", hideTreeContextMenu);
  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape") hideTreeContextMenu();
  });

  return menu;
}

function showTreeContextMenu(path: string, isDir: boolean, clientX: number, clientY: number): void {
  treeContextMenu.dataset.path = path;
  treeContextMenu.dataset.isDir = String(isDir);

  const deleteButton = treeContextMenu.querySelector<HTMLButtonElement>('[data-action="delete"]');
  if (deleteButton) {
    deleteButton.textContent = isDir ? "Delete folder" : "Delete file";
  }

  // Position off-screen first so dimensions are measurable after unhiding
  treeContextMenu.style.left = "-9999px";
  treeContextMenu.style.top = "-9999px";
  treeContextMenu.classList.remove("hidden");

  const { width, height } = treeContextMenu.getBoundingClientRect();
  const left = Math.min(clientX, window.innerWidth - width - 8);
  const top = Math.min(clientY, window.innerHeight - height - 8);
  treeContextMenu.style.left = `${Math.max(8, left)}px`;
  treeContextMenu.style.top = `${Math.max(8, top)}px`;

  deleteButton?.focus();
}

function hideTreeContextMenu(): void {
  treeContextMenu.classList.add("hidden");
  delete treeContextMenu.dataset.path;
  delete treeContextMenu.dataset.isDir;
}

treeContextMenu.addEventListener("click", async (e) => {
  e.stopPropagation();
  const target = e.target as HTMLElement;
  if (target.dataset.action !== "delete") return;

  const path = treeContextMenu.dataset.path;
  const isDir = treeContextMenu.dataset.isDir === "true";
  hideTreeContextMenu();
  if (!path) return;

  const message = isDir
    ? `Delete folder "${path}" and all of its contents? This cannot be undone.`
    : `Delete "${path}"? This cannot be undone.`;
  if (!await showConfirm(message, { danger: true })) return;

  try {
    await deleteFile(path, isDir);
    if (isDir) clearExpandedPath(path);
    await loadTree();
    if (routePathIsInside(path)) {
      window.location.hash = "#/";
    }
  } catch (err) {
    await showAlert(`Delete failed: ${(err as Error).message}`);
  }
});

function clearExpandedPath(path: string): void {
  currentExpandedPaths.delete(path);
  for (const expandedPath of Array.from(currentExpandedPaths)) {
    if (expandedPath.startsWith(path + "/")) {
      currentExpandedPaths.delete(expandedPath);
    }
  }
}

function routePathIsInside(path: string): boolean {
  const hashPath = window.location.hash.replace(/^#\/?/, "").split("?")[0].split("#")[0];
  return hashPath === path || hashPath.startsWith(path + "/");
}
