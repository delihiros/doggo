import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import semver from "semver";

export function compatibilityErrors(manifest, lock) {
  const errors = [];
  const supported = manifest.engines.vscode;
  const minimum = semver.minVersion(supported);
  if (!minimum) return ["Invalid engines.vscode range"];

  if (lock.packages[""].engines.vscode !== supported) {
    errors.push("package-lock.json engines.vscode differs from package.json");
  }
  for (const [name, dependency] of Object.entries(lock.packages)) {
    if (name === "" || dependency.dev || !dependency.engines?.vscode) continue;
    if (!semver.subset(supported, dependency.engines.vscode)) {
      errors.push(`${name} requires VS Code ${dependency.engines.vscode}, but the extension supports ${supported}`);
    }
  }

  // Compile against the oldest supported API, not whatever a caret resolves to.
  const types = lock.packages["node_modules/@types/vscode"].version;
  if (manifest.devDependencies["@types/vscode"] !== types ||
      lock.packages[""].devDependencies["@types/vscode"] !== types ||
      !semver.satisfies(types, `~${minimum.version}`)) {
    errors.push(`Pin @types/vscode to an exact ${minimum.major}.${minimum.minor}.x version (resolved ${types})`);
  }
  return errors;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const read = (name) => JSON.parse(readFileSync(new URL(`../${name}`, import.meta.url), "utf8"));
  const errors = compatibilityErrors(read("package.json"), read("package-lock.json"));
  if (errors.length) {
    console.error(errors.join("\n"));
    process.exitCode = 1;
  }
}
