import { appendJournal } from "./api.js";
import { navigate } from "./app.js";
import { sanitizeHTML } from "./sanitize.js";

const journalEntries = document.getElementById("journal-entries")!;
const journalInput = document.getElementById("journal-input") as HTMLTextAreaElement;
const btnAppend = document.getElementById("btn-journal-append")!;

let currentPath: string | null = null;

export function showJournal(path: string, content: string): void {
  currentPath = path;
  journalInput.value = "";

  // Render the journal content (skip front-matter)
  const body = content.replace(/^---\ntype: journal\n---\n*/, "");
  if (typeof marked !== "undefined") {
    journalEntries.innerHTML = sanitizeHTML(marked.parse(body));
  } else {
    journalEntries.textContent = body;
  }
}

btnAppend.addEventListener("click", async () => {
  const content = journalInput.value.trim();
  if (!content || !currentPath) return;

  try {
    await appendJournal(currentPath, content);
    journalInput.value = "";
    navigate(`#/${currentPath}`);
  } catch (err) {
    alert(`Append failed: ${(err as Error).message}`);
  }
});
