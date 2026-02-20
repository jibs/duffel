import { search, SearchResult, SearchOptions } from "./api.js";

const sidebarInput = document.getElementById("search-input") as HTMLInputElement;
const queryInput = document.getElementById("search-query-input") as HTMLInputElement;
const searchResults = document.getElementById("search-results")!;
const sortSelect = document.getElementById("search-sort") as HTMLSelectElement;
const prefixInput = document.getElementById("search-prefix") as HTMLInputElement;
const afterInput = document.getElementById("search-after") as HTMLInputElement;
const beforeInput = document.getElementById("search-before") as HTMLInputElement;
const limitSelect = document.getElementById("search-limit") as HTMLSelectElement;
const btnSearch = document.getElementById("btn-search")!;
const btnPrev = document.getElementById("btn-search-prev") as HTMLButtonElement;
const btnNext = document.getElementById("btn-search-next") as HTMLButtonElement;
const showingLabel = document.getElementById("search-showing")!;
const pagination = document.getElementById("search-pagination")!;

let debounceTimer: number | null = null;
let currentQuery = "";
let currentOffset = 0;
let lastResultCount = 0;

// Get the current project (top-level folder) from the URL hash.
function getCurrentPrefix(): string {
  const hash = window.location.hash || "#/";
  const path = hash.replace(/^#\/?/, "").replace(/\?.*$/, "");
  if (!path) return "";
  const first = path.split("/")[0];
  return first ? first + "/" : "";
}

function getOpts(): SearchOptions {
  return {
    prefix: prefixInput.value.trim() || undefined,
    limit: parseInt(limitSelect.value, 10),
    offset: currentOffset,
    sort: sortSelect.value || undefined,
    after: afterInput.value || undefined,
    before: beforeInput.value || undefined,
  };
}

// Sidebar input: debounce → open search view
sidebarInput.addEventListener("input", () => {
  if (debounceTimer !== null) clearTimeout(debounceTimer);
  const query = sidebarInput.value.trim();
  if (!query) {
    hideSearch();
    return;
  }
  debounceTimer = window.setTimeout(() => openSearch(query), 300);
});

sidebarInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    if (debounceTimer !== null) clearTimeout(debounceTimer);
    const query = sidebarInput.value.trim();
    if (query) openSearch(query);
  }
});

// Open search view with a query
function openSearch(query: string): void {
  showSearch();
  queryInput.value = query;
  // Auto-fill prefix from URL hash if prefix field is empty
  if (!prefixInput.value.trim()) {
    prefixInput.value = getCurrentPrefix();
  }
  currentOffset = 0;
  runSearch(query);
}

// Search button in the detailed view
btnSearch.addEventListener("click", () => {
  const query = queryInput.value.trim();
  if (query) {
    currentOffset = 0;
    runSearch(query);
  }
});

queryInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    const query = queryInput.value.trim();
    if (query) {
      currentOffset = 0;
      runSearch(query);
    }
  }
});

// Filter changes reset offset and re-run
for (const el of [sortSelect, prefixInput, afterInput, beforeInput, limitSelect]) {
  el.addEventListener("change", () => {
    const query = queryInput.value.trim();
    if (query) {
      currentOffset = 0;
      runSearch(query);
    }
  });
}

// Pagination
btnPrev.addEventListener("click", () => {
  const limit = parseInt(limitSelect.value, 10);
  currentOffset = Math.max(0, currentOffset - limit);
  runSearch(currentQuery);
});

btnNext.addEventListener("click", () => {
  const limit = parseInt(limitSelect.value, 10);
  currentOffset += limit;
  runSearch(currentQuery);
});

async function runSearch(query: string): Promise<void> {
  currentQuery = query;
  const opts = getOpts();
  try {
    const results = await search(query, opts);
    lastResultCount = results.length;
    renderResults(results);
    updatePagination(opts.limit ?? 20);
  } catch (err) {
    renderStatus((err as Error).message);
    hidePagination();
  }
}

function sanitizeSnippet(raw: string): string {
  // Allow only <mark> tags from the snippet, escape everything else
  const temp = document.createElement("div");
  temp.textContent = raw;
  let escaped = temp.innerHTML;
  // Restore <mark> and </mark> that were in the original
  escaped = raw.replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;")
    .replace(/&lt;mark&gt;/g, "<mark>")
    .replace(/&lt;\/mark&gt;/g, "</mark>");
  return escaped;
}

function renderResults(data: SearchResult[]): void {
  searchResults.replaceChildren();

  if (data.length === 0) {
    const prefix = prefixInput.value.trim();
    const scope = prefix ? ` in ${prefix.replace(/\/$/, "")}` : "";
    renderStatus(`No results found${scope}.`);
    return;
  }

  data.forEach((result) => {
    const path = result.path || "";

    const resultEl = document.createElement("div");
    resultEl.className = "search-result";
    resultEl.dataset.path = path;

    const titleEl = document.createElement("div");
    titleEl.className = "title";
    titleEl.textContent = result.title || path.split("/").pop() || path;

    const pathEl = document.createElement("div");
    pathEl.className = "path";
    pathEl.textContent = result.path || result.file || "";

    const metaEl = document.createElement("div");
    metaEl.className = "meta-row";

    if (result.score != null) {
      const scoreEl = document.createElement("span");
      scoreEl.className = "score";
      scoreEl.textContent = result.score.toFixed(2);
      metaEl.appendChild(scoreEl);
    }

    if (result.modified_at) {
      const dateEl = document.createElement("span");
      dateEl.className = "date";
      dateEl.textContent = result.modified_at;
      metaEl.appendChild(dateEl);
    }

    const snippetEl = document.createElement("div");
    snippetEl.className = "snippet";
    const rawSnippet = result.snippet || result.content || "";
    snippetEl.innerHTML = sanitizeSnippet(rawSnippet);

    resultEl.appendChild(titleEl);
    resultEl.appendChild(pathEl);
    if (metaEl.children.length > 0) resultEl.appendChild(metaEl);
    resultEl.appendChild(snippetEl);

    resultEl.addEventListener("click", () => {
      if (path) {
        window.location.hash = `#/${path}`;
        hideSearch();
      }
    });

    searchResults.appendChild(resultEl);
  });
}

function updatePagination(limit: number): void {
  pagination.classList.remove("hidden");
  const from = currentOffset + 1;
  const to = currentOffset + lastResultCount;
  showingLabel.textContent = lastResultCount > 0 ? `Showing ${from}–${to}` : "";
  btnPrev.disabled = currentOffset === 0;
  btnNext.disabled = lastResultCount < limit;
}

function hidePagination(): void {
  pagination.classList.add("hidden");
}

function renderStatus(message: string): void {
  searchResults.replaceChildren();
  const empty = document.createElement("p");
  empty.className = "empty-state";
  empty.textContent = message;
  searchResults.appendChild(empty);
  hidePagination();
}

function showSearch(): void {
  document.querySelectorAll(".view").forEach((v) => v.classList.add("hidden"));
  document.getElementById("view-search")!.classList.remove("hidden");
}

function hideSearch(): void {
  sidebarInput.value = "";
  searchResults.replaceChildren();
  hidePagination();
}

export { hideSearch };
