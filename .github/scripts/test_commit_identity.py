import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("check_commit_identity.py")


class CommitIdentityTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(), "Missing commit identity checker")
        spec = importlib.util.spec_from_file_location("commit_identity", SCRIPT)
        self.checker = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.checker)
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repository = Path(self.temporary.name)
        self.git("init", "-q")
        self.number = 0

    def git(self, *args, env=None):
        return subprocess.check_output(["git", *args], cwd=self.repository, env=env).decode().strip()

    def commit(self, author="fixture@users.noreply.github.com", committer="noreply@github.com"):
        self.number += 1
        (self.repository / "file").write_text(str(self.number), encoding="utf-8")
        self.git("add", "file")
        env = dict(os.environ, GIT_AUTHOR_NAME="Fixture", GIT_AUTHOR_EMAIL=author,
                   GIT_COMMITTER_NAME="Fixture", GIT_COMMITTER_EMAIL=committer)
        self.git("-c", "commit.gpgsign=false", "commit", "-qm", "fixture", env=env)
        return self.git("rev-parse", "HEAD")

    def test_accepts_noreply_and_bot_identities(self):
        self.commit()
        self.commit("123+fixture@users.noreply.github.com", "456+github-actions[bot]@users.noreply.github.com")
        self.commit("49699333+dependabot[bot]@users.noreply.github.com")
        self.assertEqual(self.checker.check_identities(self.repository), [])

    def test_detects_author_and_committer_and_redacts_values(self):
        first = self.commit(author="private@example.invalid")
        second = self.commit(committer="other@example.invalid")
        issues = self.checker.check_identities(self.repository)
        self.assertEqual({(x["commit"], x["role"]) for x in issues}, {(first, "author"), (second, "committer")})
        result = subprocess.run([os.sys.executable, str(SCRIPT), "--repository", str(self.repository)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertNotIn("private@example.invalid", result.stdout + result.stderr)
        self.assertNotIn("other@example.invalid", result.stdout + result.stderr)

    def test_new_commit_range_excludes_prior_history_but_full_check_detects_it(self):
        base = self.commit(author="private@example.invalid")
        self.commit()
        self.assertEqual(self.checker.check_identities(self.repository, base=base), [])
        self.assertEqual(len(self.checker.check_identities(self.repository)), 1)
        self.commit(author="new@example.invalid")
        self.assertEqual(len(self.checker.check_identities(self.repository, base=base)), 1)

    def test_merge_commit_identity_is_checked(self):
        self.commit()
        base_branch = self.git("symbolic-ref", "--short", "HEAD")
        self.git("checkout", "-qb", "topic")
        self.commit()
        self.git("checkout", "-q", base_branch)
        env = dict(os.environ, GIT_AUTHOR_NAME="Fixture", GIT_AUTHOR_EMAIL="merge@example.invalid",
                   GIT_COMMITTER_NAME="GitHub", GIT_COMMITTER_EMAIL="noreply@github.com")
        self.git("-c", "commit.gpgsign=false", "merge", "--no-ff", "-qm", "merge", "topic", env=env)
        self.assertEqual(self.checker.check_identities(self.repository), [{"commit": self.git("rev-parse", "HEAD"), "role": "author"}])

    def test_option_like_refs_are_rejected(self):
        self.commit()
        for kw in [{"revision": "--all"}, {"base": "--all"}]:
            with self.assertRaises(ValueError):
                self.checker.check_identities(self.repository, **kw)

    def test_replace_refs_cannot_hide_raw_personal_identity(self):
        original = self.commit(author="private@example.invalid")
        tree = self.git("rev-parse", "HEAD^{tree}")
        env = dict(os.environ, GIT_AUTHOR_NAME="Fixture", GIT_AUTHOR_EMAIL="fixture@users.noreply.github.com",
                   GIT_COMMITTER_NAME="Fixture", GIT_COMMITTER_EMAIL="noreply@github.com")
        replacement = self.git("-c", "commit.gpgsign=false", "commit-tree", tree, "-m", "public replacement", env=env)
        self.git("replace", original, replacement)
        self.assertEqual(self.checker.check_identities(self.repository), [{"commit": original, "role": "author"}])

    def test_signature_display_preference_does_not_change_machine_records(self):
        original = self.commit()
        raw = subprocess.check_output(["git", "cat-file", "commit", original], cwd=self.repository)
        headers, message = raw.split(b"\n\n", 1)
        signed = headers + b"\ngpgsig -----BEGIN PGP SIGNATURE-----\n invalid\n -----END PGP SIGNATURE-----\n\n" + message
        commit = subprocess.check_output(["git", "hash-object", "-w", "-t", "commit", "--stdin"], cwd=self.repository, input=signed).decode().strip()
        self.git("update-ref", "HEAD", commit)
        self.git("config", "log.showSignature", "true")
        self.assertEqual(self.checker.check_identities(self.repository), [])

    def test_ci_checks_complete_history_when_previous_head_is_unavailable(self):
        self.commit()
        env = dict(os.environ, COMMIT_BASE="f" * 40)
        command = [os.sys.executable, str(SCRIPT), "--repository", str(self.repository), "--ci"]
        clean = subprocess.run(command, env=env, capture_output=True, text=True)
        self.assertEqual(clean.returncode, 0, clean.stderr)
        self.commit(author="private@example.invalid")
        private = subprocess.run(command, env=env, capture_output=True, text=True)
        self.assertEqual(private.returncode, 1)
        self.assertNotIn("private@example.invalid", private.stdout + private.stderr)


if __name__ == "__main__":
    unittest.main()
