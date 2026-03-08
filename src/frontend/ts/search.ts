import { search, SearchResult, SearchOptions } from "./api.js";

const sidebarInput = document.getElementById("search-input") as HTMLInputElement;
const queryInput = document.getElementById("search-query-input") as HTMLInputElement;
const searchResults = document.getElementById("search-results")!;
const limitSelect = document.getElementById("search-limit") as HTMLSelectElement;
const intentInput = document.getElementById("search-intent") as HTMLInputElement;
const candidateLimitInput = document.getElementById("search-candidate-limit") as HTMLInputElement;
const minScoreInput = document.getElementById("search-min-score") as HTMLInputElement;
const explainInput = document.getElementById("search-explain") as HTMLInputElement;
const btnSearch = document.getElementById("btn-search")!;
const btnPrev = document.getElementById("btn-search-prev") as HTMLButtonElement;
const btnNext = document.getElementById("btn-search-next") as HTMLButtonElement;
const showingLabel = document.getElementById("search-showing")!;
const pagination = document.getElementById("search-pagination")!;

let currentQuery = "";
let currentOffset = 0;
let lastResultCount = 0;

function getOpts(): SearchOptions {
  const candidateLimit = parseInt(candidateLimitInput.value, 10);
  const minScore = parseFloat(minScoreInput.value);

  return {
    limit: parseInt(limitSelect.value, 10),
    offset: currentOffset,
    intent: intentInput.value.trim() || undefined,
    candidate_limit: Number.isNaN(candidateLimit) || candidateLimit <= 0 ? undefined : candidateLimit,
    min_score: Number.isNaN(minScore) || minScore < 0 ? undefined : minScore,
    explain: explainInput.checked || undefined,
  };
}

// Sidebar input: submit with Enter to avoid expensive per-keystroke hybrid queries
sidebarInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    const query = sidebarInput.value.trim();
    if (query) {
      openSearch(query);
    } else {
      hideSearch();
    }
  }
});

// Open search view with a query
function openSearch(query: string): void {
  showSearch();
  queryInput.value = query;
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

limitSelect.addEventListener("change", () => {
  const query = queryInput.value.trim();
  if (query) {
    currentOffset = 0;
    runSearch(query);
  }
});

for (const el of [intentInput, candidateLimitInput, minScoreInput]) {
  el.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      const query = queryInput.value.trim();
      if (query) {
        currentOffset = 0;
        runSearch(query);
      }
    }
  });
}

for (const el of [candidateLimitInput, minScoreInput, explainInput]) {
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
    renderStatus("No results found.");
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
    if (result.explain != null) {
      const explainEl = renderExplain(result.explain);
      resultEl.appendChild(explainEl);
    }

    resultEl.addEventListener("click", () => {
      if (path) {
        window.location.hash = `#/${path}`;
        hideSearch();
      }
    });

    searchResults.appendChild(resultEl);
  });
}

function renderExplain(explain: unknown): HTMLElement {
  const details = document.createElement("details");
  details.className = "search-explain";

  const summary = document.createElement("summary");
  summary.textContent = "Ranking details";
  details.appendChild(summary);

  const pre = document.createElement("pre");
  pre.textContent = formatExplain(explain);
  details.appendChild(pre);

  return details;
}

function formatExplain(explain: unknown): string {
  if (typeof explain === "string") {
    return explain;
  }
  try {
    return JSON.stringify(explain, null, 2);
  } catch {
    return String(explain);
  }
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
