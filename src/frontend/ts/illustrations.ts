import { readIllustration } from "./api.js";

const fileButton = document.getElementById("btn-illustrate")!;
const journalButton = document.getElementById("btn-journal-illustrate")!;
const fileSection = document.getElementById("file-illustration")!;
const journalSection = document.getElementById("journal-illustration")!;

let currentPath = "";
let currentTarget: "file" | "journal" | null = null;

function illustrationURL(path: string, mode: "edit" | "preview", revision = ""): string {
  const params = new URLSearchParams({ path, mode });
  if (revision) params.set("revision", revision);
  return `/illustration.html?${params.toString()}`;
}

function clearSection(section: HTMLElement): void {
  section.classList.add("hidden");
  section.replaceChildren();
}

function renderPreview(section: HTMLElement, path: string, revision: string): void {
  section.classList.remove("hidden");
  section.replaceChildren();

  const header = document.createElement("div");
  header.className = "illustration-preview-header";
  const title = document.createElement("h3");
  title.textContent = "Illustration";
  const open = document.createElement("button");
  open.textContent = "Edit diagram";
  open.addEventListener("click", () => openIllustration(path));
  header.append(title, open);

  const frame = document.createElement("iframe");
  frame.className = "illustration-preview-frame";
  frame.title = `Illustration for ${path}`;
  frame.loading = "lazy";
  frame.src = illustrationURL(path, "preview", revision);
  section.append(header, frame);
}

export async function updateIllustrationUI(path: string, target: "file" | "journal" | null): Promise<void> {
  currentPath = path;
  currentTarget = target;
  clearSection(fileSection);
  clearSection(journalSection);
  fileButton.classList.toggle("hidden", target !== "file");
  journalButton.classList.toggle("hidden", target !== "journal");
  if (!target || !path) return;

  const button = target === "file" ? fileButton : journalButton;
  button.textContent = "Illustrate";
  try {
    const result = await readIllustration(path);
    if (path !== currentPath || target !== currentTarget) return;
    if (!result.exists) return;
    button.textContent = "Open diagram";
    renderPreview(target === "file" ? fileSection : journalSection, path, result.revision || "");
  } catch (err) {
    console.warn("Failed to load illustration metadata:", err);
  }
}

function openIllustration(path: string): void {
  const overlay = document.createElement("div");
  overlay.className = "illustration-overlay";

  const header = document.createElement("div");
  header.className = "illustration-overlay-header";
  const label = document.createElement("span");
  label.textContent = `Illustration · ${path}`;
  const close = document.createElement("button");
  close.textContent = "Done";
  close.className = "primary";
  header.append(label, close);

  const frame = document.createElement("iframe");
  frame.className = "illustration-editor-frame";
  frame.title = `Edit illustration for ${path}`;
  frame.src = illustrationURL(path, "edit");
  overlay.append(header, frame);

  const dismiss = (): void => {
    overlay.remove();
    void updateIllustrationUI(currentPath, currentTarget);
  };
  close.addEventListener("click", dismiss);
  overlay.addEventListener("keydown", (event) => {
    if (event.key === "Escape") dismiss();
  });
  document.body.appendChild(overlay);
  close.focus();
}

fileButton.addEventListener("click", () => {
  if (currentPath) openIllustration(currentPath);
});
journalButton.addEventListener("click", () => {
  if (currentPath) openIllustration(currentPath);
});
