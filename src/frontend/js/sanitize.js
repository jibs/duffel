const BLOCKED_TAGS = new Set([
  "script",
  "style",
  "iframe",
  "object",
  "embed",
  "link",
  "meta",
  "form",
  "input",
  "button",
  "textarea",
  "select",
  "option",
]);

const URL_ATTRS = new Set(["href", "src", "xlink:href", "formaction"]);
const SAFE_SCHEMES = new Set(["http:", "https:", "mailto:", "tel:"]);
const HAS_SCHEME = /^[a-zA-Z][a-zA-Z0-9+.-]*:/;

function isSafeURL(url) {
  const value = (url || "").trim();
  if (!value) {
    return true;
  }

  if (value.startsWith("#") || value.startsWith("/") || value.startsWith("./") || value.startsWith("../")) {
    return true;
  }

  if (!HAS_SCHEME.test(value)) {
    return true;
  }

  const scheme = value.slice(0, value.indexOf(":") + 1).toLowerCase();
  return SAFE_SCHEMES.has(scheme);
}

function withSafeLinkRel(anchor) {
  const existing = (anchor.getAttribute("rel") || "").split(/\s+/).filter(Boolean);
  const required = new Set(["noopener", "noreferrer"]);
  existing.forEach((token) => required.add(token));
  anchor.setAttribute("rel", Array.from(required).join(" "));
}

export function sanitizeHTML(html) {
  const template = document.createElement("template");
  template.innerHTML = html || "";

  template.content.querySelectorAll("*").forEach((node) => {
    const tag = node.tagName.toLowerCase();
    if (BLOCKED_TAGS.has(tag)) {
      node.remove();
      return;
    }

    Array.from(node.attributes).forEach((attr) => {
      const name = attr.name.toLowerCase();
      const value = attr.value;

      if (name.startsWith("on") || name === "style" || name === "srcdoc") {
        node.removeAttribute(attr.name);
        return;
      }

      if (URL_ATTRS.has(name) && !isSafeURL(value)) {
        node.removeAttribute(attr.name);
      }
    });

    if (tag === "a") {
      withSafeLinkRel(node);
    }
  });

  return template.innerHTML;
}
