import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import { dirname } from "node:path";
import test from "node:test";
const require = createRequire(import.meta.url);
const mermaidEntry = require.resolve("mermaid", { paths: [dirname(require.resolve("@tldraw/mermaid"))] });

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
