import { listDir } from "./api.js";

const fileTree = document.getElementById("file-tree")!;

const currentExpandedPaths = new Set<string>();

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
      } else {
        icon.textContent = "📄";
      }
      item.appendChild(icon);

      const name = document.createElement("span");
      name.textContent = entry.name;
      item.appendChild(name);

      item.addEventListener("click", (e) => {
        e.stopPropagation();
        if (entry.isDir) {
          toggleFolder(fullPath, item, depth);
        } else {
          window.location.hash = `#/${fullPath}`;
        }
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
      icon.textContent = entry.isDir ? "▸" : entry.isJournal ? "📓" : "📄";
      child.appendChild(icon);

      const nameSpan = document.createElement("span");
      nameSpan.textContent = entry.name;
      child.appendChild(nameSpan);

      child.addEventListener("click", (e) => {
        e.stopPropagation();
        if (entry.isDir) {
          toggleFolder(fullPath, child, depth + 1);
        } else {
          window.location.hash = `#/${fullPath}`;
        }
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
