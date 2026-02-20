import { search, SearchResult } from "./api.js";

const searchInput = document.getElementById("search-input") as HTMLInputElement;
const searchResults = document.getElementById("search-results")!;

let debounceTimer: number | null = null;

searchInput.addEventListener("input", () => {
  if (debounceTimer !== null) clearTimeout(debounceTimer);
  const query = searchInput.value.trim();
  if (!query) {
    hideSearch();
    return;
  }
  debounceTimer = window.setTimeout(() => runSearch(query), 300);
});

// Get the current project (top-level folder) from the URL hash.
function getCurrentPrefix(): string {
  const hash = window.location.hash || "#/";
  const path = hash.replace(/^#\/?/, "").replace(/\?.*$/, "");
  if (!path) return "";
  // Use the top-level folder as the project prefix
  const first = path.split("/")[0];
  return first ? first + "/" : "";
}

async function runSearch(query: string): Promise<void> {
  showSearch();
  try {
    const prefix = getCurrentPrefix();
    const results = await search(query, { prefix });
    renderResults(results, prefix);
  } catch (err) {
    renderStatus((err as Error).message);
  }
}

function renderResults(data: SearchResult[], prefix: string): void {
  searchResults.replaceChildren();

  if (Array.isArray(data) && data.length === 0) {
    const scope = prefix ? ` in ${prefix.replace(/\/$/, "")}` : "";
    renderStatus(`No results found${scope}.`);
    return;
  }

  if (Array.isArray(data)) {
    data.forEach((result) => {
      const path = result.path || "";

      const resultEl = document.createElement("div");
      resultEl.className = "search-result";
      resultEl.dataset.path = path;

      const pathEl = document.createElement("div");
      pathEl.className = "path";
      pathEl.textContent = result.path || result.file || "";

      const snippetEl = document.createElement("div");
      snippetEl.className = "snippet";
      snippetEl.textContent = result.snippet || result.content || "";

      resultEl.appendChild(pathEl);
      resultEl.appendChild(snippetEl);
      resultEl.addEventListener("click", () => {
        if (path) {
          window.location.hash = `#/${path}`;
          hideSearch();
        }
      });

      searchResults.appendChild(resultEl);
    });
  } else {
    renderStatus("Search returned unexpected format.");
  }
}

function renderStatus(message: string): void {
  searchResults.replaceChildren();
  const empty = document.createElement("p");
  empty.className = "empty-state";
  empty.textContent = message;
  searchResults.appendChild(empty);
}

function showSearch(): void {
  document.querySelectorAll(".view").forEach((v) => v.classList.add("hidden"));
  document.getElementById("view-search")!.classList.remove("hidden");
}

function hideSearch(): void {
  searchInput.value = "";
  searchResults.innerHTML = "";
}

export { hideSearch };
