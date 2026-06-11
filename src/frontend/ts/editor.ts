import { writeFile, readFile, FileResponse } from "./api.js";
import { navigate } from "./app.js";
import { renderFileContent } from "./render.js";
import { showAlert, showConfirm } from "./dialog.js";

const viewEditor = document.getElementById("view-editor")!;
const editorSource = document.getElementById("editor-source") as HTMLTextAreaElement;
const editorPreview = document.getElementById("editor-preview")!;
const btnSave = document.getElementById("btn-save")!;
const btnCancel = document.getElementById("btn-cancel")!;

let currentPath: string | null = null;
let openedVersion: FileResponse | null = null;
let lastPromptedConflictKey = "";

export function openEditor(path: string, file: FileResponse | null): void {
  currentPath = path;
  openedVersion = file;
  lastPromptedConflictKey = "";
  editorSource.value = file?.content || "";
  updatePreview();
}

function updatePreview(): void {
  renderFileContent(editorPreview, currentPath || "", editorSource.value, currentPath?.match(/\.html?$/i) ? "html" : "markdown", () => undefined);
}

function backendKey(file: FileResponse | null): string {
  if (!file) return "missing";
  return `${file.modTime}:${file.size}:${file.content.length}`;
}

function backendChanged(file: FileResponse): boolean {
  return backendKey(file) !== backendKey(openedVersion);
}

function localDraftChanged(): boolean {
  return editorSource.value !== (openedVersion?.content || "");
}

function loadBackendVersion(file: FileResponse): void {
  openedVersion = file;
  lastPromptedConflictKey = "";
  editorSource.value = file.content;
  updatePreview();
}

export async function checkEditorBackendChange(): Promise<void> {
  if (!currentPath) return;

  let latest: FileResponse;
  try {
    latest = await readFile(currentPath);
  } catch {
    if (openedVersion && lastPromptedConflictKey !== "missing") {
      lastPromptedConflictKey = "missing";
      await showAlert(`"${currentPath}" was deleted or moved on the backend while you were editing.`);
    }
    return;
  }

  if (!backendChanged(latest) || latest.content === editorSource.value) {
    openedVersion = latest;
    return;
  }

  const latestKey = backendKey(latest);
  if (lastPromptedConflictKey === latestKey) return;
  lastPromptedConflictKey = latestKey;

  if (!localDraftChanged()) {
    loadBackendVersion(latest);
    return;
  }

  const shouldLoadBackend = await showConfirm(
    `"${currentPath}" changed on the backend while you were editing. Load the backend version and discard your draft?`,
    { confirmLabel: "Load backend version" },
  );

  if (shouldLoadBackend) {
    loadBackendVersion(latest);
  }
}

editorSource.addEventListener("input", updatePreview);

btnSave.addEventListener("click", async () => {
  if (!currentPath) return;
  try {
    let latest: FileResponse | null = null;
    try {
      latest = await readFile(currentPath);
    } catch {
      latest = null;
    }

    if (latest && backendChanged(latest) && latest.content !== editorSource.value) {
      const shouldOverwrite = await showConfirm(
        `"${currentPath}" has changed on the backend since you opened it. Overwrite with your draft?`,
        { confirmLabel: "Overwrite" },
      );
      if (!shouldOverwrite) return;
    }

    await writeFile(currentPath, editorSource.value);
    navigate(`#/${currentPath}`);
  } catch (err) {
    await showAlert(`Save failed: ${(err as Error).message}`);
  }
});

btnCancel.addEventListener("click", async () => {
  if (!currentPath) return;
  navigate(`#/${currentPath}`);
});

// Keyboard shortcut: Cmd/Ctrl+S to save
viewEditor.addEventListener("keydown", (e) => {
  if ((e as KeyboardEvent).metaKey || (e as KeyboardEvent).ctrlKey) {
    if ((e as KeyboardEvent).key === "s") {
      e.preventDefault();
      btnSave.click();
    }
  }
});
