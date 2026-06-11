import { FileKind } from "./api.js";
import { sanitizeHTML } from "./sanitize.js";

type LinkTarget =
  | { type: "external" }
  | { type: "anchor"; fragment: string }
  | { type: "workspace"; candidates: string[]; fragment: string };

const EXTERNAL_SCHEMES = new Set(["http:", "https:", "mailto:", "tel:"]);
const HAS_SCHEME = /^[a-zA-Z][a-zA-Z0-9+.-]*:/;
const MARKDOWN_EXTENSIONS = new Set([".md", ".markdown"]);
const HTML_EXTENSIONS = new Set([".html", ".htm"]);
const IFRAME_LINK_MESSAGE = "duffel:html-link";
let activeHTMLFrameCleanup: (() => void) | null = null;

export function renderContent(path: string, content: string, kind: FileKind): string {
  if (kind === "html" || isHTMLPath(path)) {
    return "";
  }

  if (typeof marked !== "undefined") {
    const markdown = content.replace(/\[\[([^\]]+)\]\]/g, (_: string, target: string) => {
      const label = target.split("/").pop()!.replace(/\.(md|markdown|html|htm)$/i, "").replace(/-/g, " ");
      return `[${label}](${target})`;
    });
    return sanitizeHTML(marked.parse(markdown));
  }

  return escapeHTML(content);
}

export function renderFileContent(
  container: HTMLElement,
  path: string,
  content: string,
  kind: FileKind,
  navigate: (candidates: string[], fragment: string) => void,
): HTMLIFrameElement | null {
  activeHTMLFrameCleanup?.();
  activeHTMLFrameCleanup = null;
  container.replaceChildren();
  container.classList.remove("html-page-host");
  if (kind === "html" || isHTMLPath(path)) {
    container.classList.add("html-page-host");
    const frame = document.createElement("iframe");
    frame.className = "html-page-frame";
    frame.setAttribute("sandbox", "allow-downloads allow-forms allow-modals allow-popups allow-scripts");
    frame.style.width = "100%";
    frame.style.border = "0";
    frame.srcdoc = withHTMLLinkBridge(content);
    const heightHandler = (event: MessageEvent) => {
      const data = event.data as { type?: string; height?: number };
      if (event.source !== frame.contentWindow || !data || data.type !== "duffel:html-height") {
        return;
      }
      if (typeof data.height === "number" && data.height > 0) {
        frame.style.height = `${Math.ceil(data.height)}px`;
      }
    };
    window.addEventListener("message", heightHandler);
    activeHTMLFrameCleanup = () => window.removeEventListener("message", heightHandler);
    frame.addEventListener("load", () => resizeHTMLFrame(frame));
    container.appendChild(frame);
    return frame;
  }

  container.innerHTML = renderContent(path, content, kind);
  attachWorkspaceLinkHandlers(container, path, navigate);
  return null;
}

export function attachWorkspaceLinkHandlers(
  container: HTMLElement,
  currentPath: string,
  navigate: (candidates: string[], fragment: string) => void,
): void {
  container.querySelectorAll("a[href]").forEach((anchor) => {
    const href = anchor.getAttribute("href");
    const target = resolveLinkTarget(currentPath, href || "");

    if (target.type === "external") {
      return;
    }

    anchor.addEventListener("click", (event) => {
      event.preventDefault();
      if (target.type === "anchor") {
        scrollToFragment(container, target.fragment);
        return;
      }
      navigate(target.candidates, target.fragment);
    });
  });
}

export function attachHTMLFrameLinkBridge(
  currentPath: string,
  navigate: (candidates: string[], fragment: string) => void,
): () => void {
  const handler = (event: MessageEvent) => {
    const data = event.data as { type?: string; href?: string };
    if (!data || data.type !== IFRAME_LINK_MESSAGE || typeof data.href !== "string") {
      return;
    }

    const target = resolveLinkTarget(currentPath, data.href);
    if (target.type === "external") {
      window.open(data.href, "_blank", "noopener,noreferrer");
      return;
    }
    if (target.type === "anchor") {
      return;
    }
    navigate(target.candidates, target.fragment);
  };
  window.addEventListener("message", handler);
  return () => window.removeEventListener("message", handler);
}

export function resolveLinkTarget(currentPath: string, rawHref: string): LinkTarget {
  const href = rawHref.trim();
  if (!href) {
    return { type: "external" };
  }

  if (href.startsWith("#")) {
    return { type: "anchor", fragment: href.slice(1) };
  }

  const parsed = splitHref(href);
  if (HAS_SCHEME.test(parsed.path)) {
    const scheme = parsed.path.slice(0, parsed.path.indexOf(":") + 1).toLowerCase();
    if (EXTERNAL_SCHEMES.has(scheme)) {
      return { type: "external" };
    }
  }

  if (parsed.path.startsWith("//")) {
    return { type: "external" };
  }

  const resolved = normalizeWorkspacePath(currentPath, parsed.path);
  return {
    type: "workspace",
    candidates: candidatePaths(resolved),
    fragment: parsed.fragment,
  };
}

export function routeHash(path: string, fragment = ""): string {
  return `#/${path}${fragment ? `#${fragment}` : ""}`;
}

export function parseRouteHash(hash: string): { path: string; fragment: string; edit: boolean } {
  const raw = (hash || "#/").replace(/^#\/?/, "");
  const hashIndex = raw.indexOf("#");
  const beforeFragment = hashIndex >= 0 ? raw.slice(0, hashIndex) : raw;
  return {
    path: beforeFragment.replace(/\?edit$/, ""),
    fragment: hashIndex >= 0 ? raw.slice(hashIndex + 1) : "",
    edit: beforeFragment.endsWith("?edit"),
  };
}

export function scrollToFragment(container: HTMLElement, fragment: string): void {
  if (!fragment) {
    return;
  }
  const decoded = safeDecodeURIComponent(fragment);
  const id = CSS.escape(decoded);
  const target = container.querySelector<HTMLElement>(`#${id}, [name="${id}"]`);
  target?.scrollIntoView({ block: "start" });
}

export function scrollHTMLFrameToFragment(frame: HTMLIFrameElement, fragment: string): void {
  if (!fragment) {
    return;
  }
  frame.contentWindow?.postMessage({ type: "duffel:scroll-fragment", fragment }, "*");
}

function resizeHTMLFrame(frame: HTMLIFrameElement): void {
  try {
    const doc = frame.contentDocument;
    if (!doc) {
      return;
    }
    const height = Math.max(doc.documentElement.scrollHeight, doc.body?.scrollHeight || 0);
    if (height > 0) {
      frame.style.height = `${height}px`;
    }
  } catch {
    // Sandboxed frames can become inaccessible if browser policy changes; keep the default height.
  }
}

function withHTMLLinkBridge(content: string): string {
  const bridge = `<script>
(() => {
  const postHeight = () => {
    try {
      parent.postMessage({ type: "duffel:html-height", height: Math.max(document.documentElement.scrollHeight, document.body ? document.body.scrollHeight : 0) }, "*");
    } catch {}
  };
  document.addEventListener("click", (event) => {
    const anchor = event.target && event.target.closest ? event.target.closest("a[href]") : null;
    if (!anchor) return;
    const href = anchor.getAttribute("href") || "";
    if (!href || anchor.target === "_blank" || href.startsWith("mailto:") || href.startsWith("tel:")) return;
    event.preventDefault();
    parent.postMessage({ type: "${IFRAME_LINK_MESSAGE}", href }, "*");
  });
  addEventListener("message", (event) => {
    if (!event.data || event.data.type !== "duffel:scroll-fragment") return;
    const raw = String(event.data.fragment || "");
    let decoded = raw;
    try { decoded = decodeURIComponent(raw); } catch {}
    const escaped = CSS.escape(decoded);
    const target = document.querySelector("#" + escaped + ", [name=\\"" + escaped + "\\"]");
    if (target) target.scrollIntoView({ block: "start" });
  });
  addEventListener("load", postHeight);
  if ("ResizeObserver" in window) {
    new ResizeObserver(postHeight).observe(document.documentElement);
  }
  setTimeout(postHeight, 0);
})();
</script>`;

  if (/<\/body\s*>/i.test(content)) {
    return content.replace(/<\/body\s*>/i, `${bridge}</body>`);
  }
  return `${content}${bridge}`;
}

function candidatePaths(path: string): string[] {
  if (hasExtension(path)) {
    return [path];
  }
  return [path, `${path}.html`, `${path}.md`];
}

function normalizeWorkspacePath(currentPath: string, hrefPath: string): string {
  const raw = hrefPath.startsWith("/") ? hrefPath.slice(1) : joinRelativePath(currentPath, hrefPath);
  const parts: string[] = [];
  raw.split("/").forEach((part) => {
    if (!part || part === ".") {
      return;
    }
    if (part === "..") {
      parts.pop();
      return;
    }
    parts.push(part);
  });
  return parts.join("/");
}

function joinRelativePath(currentPath: string, hrefPath: string): string {
  const dir = currentPath.includes("/") ? currentPath.substring(0, currentPath.lastIndexOf("/")) : "";
  return dir ? `${dir}/${hrefPath}` : hrefPath;
}

function splitHref(href: string): { path: string; fragment: string } {
  const hashIndex = href.indexOf("#");
  if (hashIndex < 0) {
    return { path: href, fragment: "" };
  }
  return {
    path: href.slice(0, hashIndex),
    fragment: href.slice(hashIndex + 1),
  };
}

function hasExtension(path: string): boolean {
  return /\.[^/.]+$/.test(path);
}

function isHTMLPath(path: string): boolean {
  return HTML_EXTENSIONS.has(extension(path));
}

function extension(path: string): string {
  const match = path.toLowerCase().match(/\.[^/.]+$/);
  return match ? match[0] : "";
}

function escapeHTML(raw: string): string {
  return raw.replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

export function isMarkdownKind(path: string, kind: FileKind): boolean {
  return kind === "markdown" || MARKDOWN_EXTENSIONS.has(extension(path));
}

function safeDecodeURIComponent(raw: string): string {
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}
