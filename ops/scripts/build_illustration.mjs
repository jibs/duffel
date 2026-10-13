import { build } from "esbuild";
import { rm } from "node:fs/promises";

// Only remove this build's generated directory; keep the vanilla TS modules.
await rm("src/frontend/js/drawing", { recursive: true, force: true });
// Remove the previous unsplit bundle, including its vulnerable paste handler.
await rm("src/frontend/js/illustration.js", { force: true });
await rm("src/frontend/js/illustration.css", { force: true });
const result = await build({
  entryPoints: ["src/frontend/drawing/illustration.tsx"],
  bundle: true,
  format: "esm",
  splitting: true,
  minify: true,
  outdir: "src/frontend/js/drawing",
  chunkNames: "chunks/[name]-[hash]",
  assetNames: "assets/[name]-[hash]",
  loader: { ".woff": "file", ".woff2": "file", ".svg": "file" },
  define: { "process.env.NODE_ENV": '"production"' },
  metafile: true,
  logLevel: "info",
});

// Fail CI if a transitive dependency reintroduces either reviewed advisory.
const inputs = Object.keys(result.metafile.inputs);
for (const [name, version] of [["prosemirror-view", "1.42.3"], ["mermaid", "11.16.1"]]) {
  const packages = inputs.filter((input) => input.includes(`/node_modules/${name}/`));
  if (!packages.length || packages.some((input) => !input.includes(`/${name}@${version}/`))) {
    throw new Error(`Drawing bundle must use patched ${name}@${version}`);
  }
}

const outputs = result.metafile.outputs;
const eager = new Set();
function visit(path) {
  if (eager.has(path)) return;
  eager.add(path);
  for (const dependency of outputs[path].imports) {
    if (dependency.kind === "import-statement" && outputs[dependency.path]) visit(dependency.path);
  }
}
visit("src/frontend/js/drawing/illustration.js");
const initialBytes = [...eager].reduce((sum, path) => sum + outputs[path].bytes, 0);
const totalBytes = Object.entries(outputs).filter(([path]) => path.endsWith(".js"))
  .reduce((sum, [, output]) => sum + output.bytes, 0);
console.log(`Drawing JavaScript: ${initialBytes} initial bytes; ${totalBytes} total bytes (lazy chunks included)`);
if (initialBytes > 3 << 20 || totalBytes > 6 << 20) throw new Error("Drawing bundle exceeds size budget");
