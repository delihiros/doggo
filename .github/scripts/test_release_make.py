import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


class ReleaseMakeTests(unittest.TestCase):
    @unittest.skipIf(os.name == "nt" or shutil.which("make") is None, "GNU Make shell fixture requires a POSIX host")
    def test_parallel_build_refreshes_notices_before_embedding_them(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = Path(__file__).resolve().parents[2]
            shutil.copyfile(source / "Makefile", root / "Makefile")
            (root / "THIRD_PARTY_NOTICES").write_text("stale\n")
            compiler = root / "fixture-go"
            compiler.write_text("""#!/bin/sh
set -eu
case "$*" in
  'run ./cmd/license-notices')
    sleep 0.2
    printf 'fresh\\n' > THIRD_PARTY_NOTICES
    ;;
  build*)
    test "$(cat THIRD_PARTY_NOTICES)" = fresh || exit 41
    mkdir -p dist/bin
    cp THIRD_PARTY_NOTICES dist/bin/embedded-notices
    ;;
  *) exit 42 ;;
esac
""")
            compiler.chmod(0o755)
            result = subprocess.run(["make", "-j4", "build-doggo", f"GO={compiler}"], cwd=root, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual((root / "dist/bin/embedded-notices").read_text(), "fresh\n")


if __name__ == "__main__":
    unittest.main()
