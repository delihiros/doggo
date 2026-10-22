"""Exercise the workflow's actual npm version command in disposable packages."""
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


def version_command(version):
    lines = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8").splitlines()
    start = next(i for i, line in enumerate(lines) if "name: Set extension version" in line)
    command = next(line.strip()[5:] for line in lines[start + 1:] if line.strip().startswith("run: "))
    args = shlex.split(command)
    if args[:2] != ["npm", "version"] or args[-1] != "${VERSION#v}":
        raise ValueError("Unexpected release version command; update its execution fixture")
    return [shutil.which("npm.cmd" if os.name == "nt" else "npm"), *args[1:-1], version.removeprefix("v")]


class ReleaseVersionTests(unittest.TestCase):
    def test_same_and_different_versions_keep_both_manifests_consistent(self):
        manifest = json.loads((ROOT / "editors/vscode/package.json").read_text(encoding="utf-8"))
        current = manifest["version"]
        major, minor, patch = map(int, current.split("."))
        for version in [current, f"{major}.{minor}.{patch + 1}"]:
            with self.subTest(version=version), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                package = {"name": "release-version-fixture", "version": current}
                lock = {"name": package["name"], "version": current, "lockfileVersion": 3,
                        "packages": {"": dict(package)}}
                (directory / "package.json").write_text(json.dumps(package), encoding="utf-8")
                (directory / "package-lock.json").write_text(json.dumps(lock), encoding="utf-8")
                result = subprocess.run(version_command("v" + version), cwd=directory,
                                        capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(json.loads((directory / "package.json").read_text())["version"], version)
                updated = json.loads((directory / "package-lock.json").read_text())
                self.assertEqual(updated["version"], version)
                self.assertEqual(updated["packages"][""]["version"], version)


if __name__ == "__main__":
    unittest.main()
