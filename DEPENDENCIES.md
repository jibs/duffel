# Dependency security maintenance

`package.json` pins reviewed transitive fixes through `pnpm.overrides`. All Tiptap
packages stay on the same version to preserve their peer dependency contracts.
The drawing build checks the resolved browser versions as well as bundle size.
Use pnpm and retain the configured seven-day minimum release age when updating.

Braces 3.0.3 has no published fix for
[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm).
The tracked `patches/braces-3.0.3.patch` limits parser nesting and recursive AST
walks to a maximum depth of 128. pnpm applies it to qmd's fast-glob dependency on
every installation; the lockfile records the patch hash. Normal collection masks
and brace expansion remain supported. Regression tests exercise deeply nested
patterns, direct AST inputs, and normal glob expansion.

Version-based audits still report this Braces advisory because the local patch
does not change the upstream version. Do not suppress the alert or describe the
audit as clean. Replace the patch with a published fix when an eligible release
is available, and keep the regression tests.

Run `pnpm audit` and `make ci` after dependency updates. The dependency tests also
cover Tiptap attribute and Markdown parser attacks, Mermaid parser exhaustion,
and Simple Git's unsafe configuration/environment guards and normal cloning.
