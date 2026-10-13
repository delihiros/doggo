import { mkdir } from "node:fs/promises";

import { build } from "esbuild";
import { writeNotices } from "./scripts/licenses.mjs";

const checkNotices = process.argv.includes("--check-notices");
if (!checkNotices) await mkdir("dist", { recursive: true });

const result = await build({
  entryPoints: ["src/extension.ts"],
  outfile: "dist/extension.js",
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node20",
  minify: true,
  metafile: true,
  write: !checkNotices,
  external: ["vscode"],
  logLevel: "info"
});

await writeNotices(result.metafile, { check: checkNotices });
