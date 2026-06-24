import { writeFile, readFile, uploadImage, FileResponse } from "./api.js";
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

// --- Image upload (paste + drag-drop) ---

type ImageUploadType = { contentType: string; ext: string };

const MIME_TO_EXT: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/jpg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
  "image/avif": "avif",
  "image/bmp": "bmp",
  "image/x-ms-bmp": "bmp",
  "image/x-icon": "ico",
  "image/vnd.microsoft.icon": "ico",
  "image/svg+xml": "svg",
};

const EXT_TO_MIME: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  avif: "image/avif",
  bmp: "image/bmp",
  ico: "image/x-icon",
  svg: "image/svg+xml",
};

function imageUploadType(blob: Blob): ImageUploadType | null {
  const mime = blob.type.toLowerCase();
  const mimeExt = MIME_TO_EXT[mime];
  if (mimeExt) {
    return { contentType: EXT_TO_MIME[mimeExt], ext: mimeExt };
  }

  if (blob instanceof File) {
    const ext = blob.name.split(".").pop()?.toLowerCase() || "";
    const contentType = EXT_TO_MIME[ext];
    if (contentType) {
      return { contentType, ext: ext === "jpeg" ? "jpg" : ext };
    }
  }

  return null;
}

function insertAtCaret(text: string): void {
  const start = editorSource.selectionStart;
  const end = editorSource.selectionEnd;
  editorSource.value = editorSource.value.slice(0, start) + text + editorSource.value.slice(end);
  const pos = start + text.length;
  editorSource.selectionStart = editorSource.selectionEnd = pos;
}

function replaceFirst(needle: string, replacement: string): void {
  const idx = editorSource.value.indexOf(needle);
  if (idx < 0) return;
  editorSource.value = editorSource.value.slice(0, idx) + replacement + editorSource.value.slice(idx + needle.length);
}

async function uploadImageBlob(blob: Blob): Promise<void> {
  const uploadType = imageUploadType(blob);
  if (!uploadType) {
    await showAlert("Image upload failed: unsupported image type");
    return;
  }
  const ext = uploadType.ext;
  const name = `pasted-${Date.now()}-${Math.random().toString(36).slice(2, 8)}.${ext}`;
  const path = `_attachments/${name}`;
  // Placeholder has no src, so the preview won't try to fetch it mid-upload.
  const placeholder = `![uploading ${name}…]()`;
  insertAtCaret(placeholder);
  updatePreview();
  try {
    await uploadImage(path, blob, uploadType.contentType);
    replaceFirst(placeholder, `![](/${path})`);
  } catch (err) {
    replaceFirst(placeholder, "");
    await showAlert(`Image upload failed: ${(err as Error).message}`);
  }
  updatePreview();
}

editorSource.addEventListener("paste", (e) => {
  const items = (e as ClipboardEvent).clipboardData?.items;
  if (!items) return;
  const images = Array.from(items).filter((it) => it.kind === "file" && it.type.startsWith("image/"));
  if (!images.length) return;
  e.preventDefault();
  for (const item of images) {
    const file = item.getAsFile();
    if (file) void uploadImageBlob(file);
  }
});

editorSource.addEventListener("dragover", (e) => {
  const items = (e as DragEvent).dataTransfer?.items;
  if (items && Array.from(items).some((it) => it.kind === "file")) {
    e.preventDefault();
  }
});

editorSource.addEventListener("drop", (e) => {
  const files = (e as DragEvent).dataTransfer?.files;
  if (!files || !files.length) return;
  const images = Array.from(files).filter((f) => f.type.startsWith("image/"));
  if (!images.length) return;
  e.preventDefault();
  for (const file of images) {
    void uploadImageBlob(file);
  }
});

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
