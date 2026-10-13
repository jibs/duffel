import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
const require = createRequire(import.meta.url);
const mermaidEntry = require.resolve("mermaid", { paths: [dirname(require.resolve("@tldraw/mermaid"))] });
const katex = createRequire(mermaidEntry)("katex");
const tiptapEntry = require.resolve("@tiptap/core", { paths: [dirname(require.resolve("tldraw"))] });
const tiptap = await import(tiptapEntry);
const qmdRequire = createRequire(import.meta.resolve("@tobilu/qmd"));
const globRequire = createRequire(qmdRequire.resolve("fast-glob"));
const braces = createRequire(globRequire.resolve("micromatch"))("braces");
const llamaRequire = createRequire(qmdRequire.resolve("node-llama-cpp"));
const { simpleGit } = llamaRequire("simple-git");

test("Mermaid advisory payload cannot exhaust the parser", () => {
  // Bound heap and time so a regression cannot crash or hang CI itself.
  // Include enough wall time for cold module imports on a busy CI worker.
  const result = spawnSync(process.execPath, ["--max-old-space-size=128", "--input-type=module", "-e", `
    import mermaid from ${JSON.stringify(mermaidEntry)};
    const parsed = await mermaid.parse('xychart\\n  x-axis 1 --> 1\\n  line [1, 2]');
    if (parsed?.diagramType !== 'xychart') process.exit(2);
    process.exit(0);
  `], { timeout: 15000, encoding: "utf8" });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.stderr);
});

test("Tiptap attributes cannot inherit executable properties from JSON", () => {
  const attrs = tiptap.mergeAttributes(JSON.parse('{"__proto__":{"onclick":"probe()"}}'));
  assert.equal(Object.getPrototypeOf(attrs), Object.prototype);
  assert.equal(attrs.onclick, undefined);
  assert.equal(Object.prototype.onclick, undefined);
  assert.deepEqual(tiptap.mergeAttributes({ class: "one" }, { class: "two", title: "safe" }), {
    class: "one two", title: "safe",
  });
});

test("KaTeX ignores inherited trust settings and still renders normal math", () => {
  const options = Object.assign(Object.create({ trust: true }), { strict: "ignore" });
  const output = katex.renderToString("\\htmlClass{dependency-canary}{x}", options);
  assert.doesNotMatch(output, /class="dependency-canary"/);
  assert.match(katex.renderToString("\\frac{a}{b}"), /<mfrac>/);
});

test("Tiptap block and inline Markdown adversarial attributes finish promptly", () => {
  const result = spawnSync(process.execPath, ["--max-old-space-size=128", "--input-type=module", "-e", `
    import { createAtomBlockMarkdownSpec, createInlineMarkdownSpec } from ${JSON.stringify(tiptapEntry)};
    const block = createAtomBlockMarkdownSpec({ nodeName: 'probe' }).markdownTokenizer;
    const inline = createInlineMarkdownSpec({ nodeName: 'probe', selfClosing: true }).markdownTokenizer;
    const started = performance.now();
    if (!block.tokenize(':::probe {' + '__QUOTED_0'.repeat(8192) + '__QUOTED_0__} :::\\n', [], {})) process.exit(2);
    if (!inline.tokenize('[probe ' + '0'.repeat(65536) + ']', [], {})) process.exit(3);
    if (performance.now() - started > 2000) process.exit(4);
  `], { timeout: 15000, encoding: "utf8" });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.stderr);
});

test("Braces rejects excessive nesting before recursive AST processing", () => {
  for (const [open, close] of [["{", "}"], ["(", ")"]]) {
    for (const pattern of [open.repeat(4000) + "a,b" + close.repeat(4000), open.repeat(4000)]) {
      for (const operation of [braces.parse, braces.compile, braces.expand]) {
        assert.throws(() => operation(pattern), /maximum depth of 128/);
      }
    }
  }
  for (const operation of [braces.compile, braces.expand, braces.stringify]) {
    let ast = { type: "text", value: "safe", nodes: [] };
    for (let i = 0; i < 4000; i++) ast = { type: "root", nodes: [ast] };
    assert.throws(() => operation(ast), /maximum depth of 128/);
  }
  assert.deepEqual(braces.expand("**/*.{md,html}"), ["**/*.md", "**/*.html"]);
  assert.deepEqual(braces.expand("{a,{b,c}}/{1..3}"), ["a/1", "a/2", "a/3", "b/1", "b/2", "b/3", "c/1", "c/2", "c/3"]);
  assert.equal(braces.stringify(braces.parse("{a,{b,c}}")), "{a,{b,c}}");
});

test("Simple Git blocks unsafe config and environment while preserving qmd clone options", async () => {
  const root = await mkdtemp(join(tmpdir(), "duffel-git-dependency-"));
  try {
    const git = simpleGit(root);
    await git.init(["--initial-branch=main"]);
    await git.addConfig("user.name", "Dependency Test");
    await git.addConfig("user.email", "dependency-test@example.org");
    await writeFile(join(root, "fixture.txt"), "Local clone fixture\n");
    await git.add("fixture.txt");
    await git.commit("Fixture");
    const target = join(root, "clone");
    await git.clone(root, target, ["--quiet", "--depth", "1", "--branch", "main"]);
    assert.equal((await simpleGit(target).log()).latest.message, "Fixture");
    await assert.rejects(git.raw(["config", "core.sshCommand", "blocked-canary"]), /not permitted|unsafe/i);
    await assert.rejects(git.raw(["clone", "-c", "core.hooksPath=blocked-canary", root, join(root, "unsafe")]), /not permitted|unsafe/i);
    await assert.rejects(simpleGit(root).env({ ...process.env, VISUAL: "blocked-canary" }).raw(["status"]), /not permitted|unsafe/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
