import importlib.util
import os
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path


class PublicSnapshotTests(unittest.TestCase):
    def test_git_routing_environment_cannot_modify_original_repository(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source"
            (source / "docs").mkdir(parents=True)
            (source / "README.md").write_text("Public\n", encoding="utf-8")
            (source / "docs/public-documents.txt").write_text("README.md\n", encoding="utf-8")
            subprocess.run(["git", "init", "-q", str(source)], check=True)
            subprocess.run(["git", "symbolic-ref", "HEAD", "refs/heads/private"], cwd=source, check=True)
            subprocess.run(["git", "add", "."], cwd=source, check=True)
            subprocess.run(["git", "-c", "user.name=Fixture", "-c", "user.email=private@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "private"], cwd=source, check=True)
            config_before = (source / ".git/config").read_bytes()
            head_before = (source / ".git/HEAD").read_bytes()
            spec = importlib.util.spec_from_file_location("public_snapshot", Path(__file__).with_name("prepare-public-source.py"))
            exporter = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(exporter)
            with patch.dict(os.environ, {"GIT_DIR": str(source / ".git"), "GIT_WORK_TREE": str(source)}):
                try:
                    exporter.export_public_source(source, root / "public")
                finally:
                    self.assertEqual((source / ".git/config").read_bytes(), config_before, "Exporter changed original Git config")
                    self.assertEqual((source / ".git/HEAD").read_bytes(), head_before, "Exporter changed original HEAD")
            self.assertTrue((root / "public/.git").is_dir())

    def test_option_like_ref_is_rejected_before_git_can_write_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            spec = importlib.util.spec_from_file_location("public_snapshot", Path(__file__).with_name("prepare-public-source.py"))
            exporter = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(exporter)
            with patch.object(exporter, "git", side_effect=AssertionError("Malformed ref reached Git")):
                with self.assertRaisesRegex(ValueError, "ref"):
                    exporter.export_public_source(root, root.parent / (root.name + "-public"), "--output=redirect")

    def test_rejects_private_document_in_manifest_before_creating_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source"
            (source / "docs/dev").mkdir(parents=True)
            (source / "docs/dev/private.md").write_text("private", encoding="utf-8")
            (source / "docs/public-documents.txt").write_text("docs/dev/private.md\n", encoding="utf-8")
            subprocess.run(["git", "init", "-q", str(source)], check=True)
            subprocess.run(["git", "add", "."], cwd=source, check=True)
            subprocess.run(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture"], cwd=source, check=True)
            spec = importlib.util.spec_from_file_location("public_snapshot", Path(__file__).with_name("prepare-public-source.py"))
            exporter = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(exporter)
            destination = root / "public"
            with self.assertRaisesRegex(ValueError, "private document"):
                exporter.export_public_source(source, destination)
            self.assertFalse(destination.exists())

    def test_export_excludes_private_docs_and_history_preserving_attribution(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source"
            source.mkdir()
            subprocess.run(["git", "init", "-q", str(source)], check=True)
            files = {
                "README.md": "See [guide](docs/guide.md).\n",
                "docs/public-documents.txt": "README.md\ndocs/guide.md\n",
                "docs/guide.md": "Public guide.\n",
                "docs/dev/private.md": "private design\n",
                "docs/language_specification.md": "unlisted internal specification\n",
                ".claude/settings.json": "{}\n",
                "LICENSE": "Public license\n",
                "THIRD_PARTY_NOTICES": "Copyright Example <author@example.org>\n",
                "main.go": "package main\nfunc main() {}\n",
                "run.sh": "#!/bin/sh\nexit 0\n",
            }
            for name, content in files.items():
                target = source / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(content, encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=source, check=True)
            subprocess.run(["git", "update-index", "--chmod=+x", "run.sh"], cwd=source, check=True)
            subprocess.run(["git", "-c", "user.name=Private Author", "-c", "user.email=private@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "private history"], cwd=source, check=True)
            script = Path(__file__).with_name("prepare-public-source.py")
            self.assertTrue(script.is_file(), "Missing public snapshot exporter")
            spec = importlib.util.spec_from_file_location("public_snapshot", script)
            self.assertIsNotNone(spec)
            self.assertIsNotNone(spec.loader)
            exporter = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(exporter)
            destination = root / "public"
            ignored = root / "global-ignore"
            ignored.write_text("*.go\n", encoding="utf-8")
            global_config = root / "global-gitconfig"
            global_config.write_text(f"[core]\n\texcludesFile = {ignored.as_posix()}\n", encoding="utf-8")
            with patch.dict(os.environ, {"GIT_CONFIG_GLOBAL": str(global_config)}):
                exporter.export_public_source(source, destination)
            self.assertTrue((destination / "main.go").exists())
            self.assertIn("main.go", subprocess.check_output(["git", "ls-files"], cwd=destination).decode().splitlines())
            self.assertTrue((destination / "docs/guide.md").exists())
            self.assertFalse((destination / "docs/dev").exists())
            self.assertFalse((destination / "docs/language_specification.md").exists())
            self.assertFalse((destination / ".claude").exists())
            self.assertIn("author@example.org", (destination / "THIRD_PARTY_NOTICES").read_text())
            count = subprocess.check_output(["git", "rev-list", "--all", "--count"], cwd=destination).strip()
            self.assertEqual(count, b"1")
            metadata = subprocess.check_output(["git", "log", "--all", "--format=%ae%n%ce%n%B"], cwd=destination).decode()
            self.assertNotIn("private@example.test", metadata)
            self.assertNotIn("private history", metadata)
            self.assertIn("users.noreply.github.com", metadata)
            mode = subprocess.check_output(["git", "ls-files", "--stage", "run.sh"], cwd=destination).decode()
            self.assertTrue(mode.startswith("100755"), "Public snapshot lost executable script permissions")
            self.assertEqual(subprocess.check_output(["git", "rev-list", "--count", "HEAD"], cwd=source).strip(), b"1")


if __name__ == "__main__":
    unittest.main()
