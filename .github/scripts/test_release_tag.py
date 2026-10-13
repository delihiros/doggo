import os
import subprocess
import tempfile
import unittest
from pathlib import Path


class ReleaseTagTests(unittest.TestCase):
    def test_rejects_same_name_branch_and_accepts_annotated_tag_commit(self):
        script = Path(__file__).with_name("verify-release-tag.sh").resolve()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            def git(*arguments):
                return subprocess.check_output(["git", *arguments], cwd=root).decode().strip()
            git("init", "-q")
            git("config", "user.name", "Fixture")
            git("config", "user.email", "fixture@example.test")
            git("config", "commit.gpgsign", "false")
            (root / "main.go").write_text("package main\n", encoding="utf-8")
            git("add", ".")
            git("commit", "-qm", "tag source")
            tagged = git("rev-parse", "HEAD")
            git("tag", "-a", "v1.2.3", "-m", "release")
            git("checkout", "-qb", "v1.2.3")
            (root / "main.go").write_text("package wrong\n", encoding="utf-8")
            git("commit", "-qam", "same-name branch")
            environment = dict(os.environ, GITHUB_EVENT_NAME="workflow_dispatch", REQUESTED_VERSION="v1.2.3")
            def verify():
                return subprocess.run(["bash", str(script)], cwd=root, env=environment, capture_output=True, text=True)
            wrong = verify()
            self.assertNotEqual(wrong.returncode, 0, "A same-name branch must not be released")
            git("checkout", "-q", "--detach", tagged)
            correct = verify()
            self.assertEqual(correct.returncode, 0, correct.stderr)
            environment["EXPECTED_RELEASE_COMMIT"] = "0" * 40
            self.assertNotEqual(verify().returncode, 0, "Every job must use the resolved release commit")


if __name__ == "__main__":
    unittest.main()
