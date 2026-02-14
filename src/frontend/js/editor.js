import { writeFile, readFile } from "./api.js";
import { navigate } from "./app.js";
import { sanitizeHTML } from "./sanitize.js";

const viewEditor = document.getElementById("view-editor");
const editorSource = document.getElementById("editor-source");
const editorPreview = document.getElementById("editor-preview");
const btnSave = document.getElementById("btn-save");
const btnCancel = document.getElementById("btn-cancel");

let currentPath = null;

export function openEditor(path, content) {
  currentPath = path;
  editorSource.value = content;
  updatePreview();
}

function updatePreview() {
  if (typeof marked !== "undefined") {
    editorPreview.innerHTML = sanitizeHTML(marked.parse(editorSource.value));
  } else {
    editorPreview.textContent = editorSource.value;
  }
}

editorSource.addEventListener("input", updatePreview);

btnSave.addEventListener("click", async () => {
  if (!currentPath) return;
  try {
    await writeFile(currentPath, editorSource.value);
    navigate(`#/${currentPath}`);
  } catch (err) {
    alert(`Save failed: ${err.message}`);
  }
});

btnCancel.addEventListener("click", async () => {
  if (!currentPath) return;
  // Reload original content
  try {
    await readFile(currentPath);
  } catch {
    // file might be new
  }
  navigate(`#/${currentPath}`);
});

// Keyboard shortcut: Cmd/Ctrl+S to save
viewEditor.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === "s") {
    e.preventDefault();
    btnSave.click();
  }
});
