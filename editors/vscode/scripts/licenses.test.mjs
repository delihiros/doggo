import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

test("bundled extension has complete source license notices", async () => {
  const notices = await readFile(new URL("../THIRD_PARTY_NOTICES", import.meta.url), "utf8");
  assert.match(notices, /Copyright \(c\) Microsoft Corporation/);
  assert.match(notices, /Permission is hereby granted, free of charge/);
  assert.match(notices, /ISC License/);
});

test("generator follows bundled inputs and preserves complete license text", async () => {
  const { generateNotices, writeNotices } = await import("./licenses.mjs");
  const root = await mkdtemp(path.join(os.tmpdir(), "doggo-licenses-"));
  try {
    const directory = path.join(root, "node_modules", "fixture");
    await mkdir(directory, { recursive: true });
    await writeFile(path.join(directory, "package.json"), JSON.stringify({ name: "fixture", version: "1.2.3", author: "Author <person@example.test>" }));
    const license = Buffer.from("Copyright Person <person@example.test>\r\nFull permission text\r\n");
    await writeFile(path.join(directory, "LICENSE"), license);
    const metafile = { outputs: { "dist/extension.js": { inputs: {
      "node_modules/fixture/index.js": { bytesInOutput: 12 },
      "node_modules/unused/index.js": { bytesInOutput: 0 },
      "src/extension.ts": { bytesInOutput: 20 }
    } } } };
    const notices = await generateNotices(metafile, root);
    assert.ok(notices.includes(license), "license bytes including email must remain unchanged");
    assert.match(notices.toString(), /Component: fixture 1\.2\.3/);
    assert.doesNotMatch(notices.toString(), /Component: unused/);
    assert.deepEqual(await generateNotices(metafile, root), notices, "generation must be deterministic");
    await writeNotices(metafile, { root });
    await writeNotices(metafile, { root, check: true });
    await writeFile(path.join(root, "THIRD_PARTY_NOTICES"), "stripped license");
    await assert.rejects(writeNotices(metafile, { root, check: true }), /THIRD_PARTY_NOTICES is stale/);
    assert.equal(await readFile(path.join(root, "THIRD_PARTY_NOTICES"), "utf8"), "stripped license", "checking must not rewrite a stale file");
    await rm(path.join(directory, "LICENSE"));
    await assert.rejects(generateNotices(metafile, root), /missing license.*fixture/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
