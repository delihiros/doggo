import importlib.util
import json
import os
from pathlib import Path
import re
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
        self.git("init", "-q", "--initial-branch=fixture-base")
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

    def ci(self, event_name, event):
        event_path = self.repository / "event.json"
        event_path.write_text(json.dumps(event), encoding="utf-8")
        env = dict(os.environ, GITHUB_EVENT_NAME=event_name, GITHUB_EVENT_PATH=str(event_path))
        command = [os.sys.executable, str(SCRIPT), "--repository", str(self.repository), "--ci"]
        result = subprocess.run(command, env=env, capture_output=True, text=True)
        self.assertNotIn("@example.invalid", result.stdout + result.stderr)
        return result

    def push_event(self, head, before=None, ref="refs/heads/topic"):
        return {"after": head, "before": before or "0" * 40, "ref": ref,
                "repository": {"default_branch": "main"}}

    def test_pr_checks_submitted_head_not_synthetic_merge(self):
        base = self.commit()
        head = self.commit()
        tree = self.git("rev-parse", "HEAD^{tree}")
        env = dict(os.environ, GIT_AUTHOR_NAME="Fixture", GIT_AUTHOR_EMAIL="merge@example.invalid",
                   GIT_COMMITTER_NAME="Fixture", GIT_COMMITTER_EMAIL="noreply@github.com")
        merge = self.git("-c", "commit.gpgsign=false", "commit-tree", tree, "-p", base, "-p", head,
                         "-m", "synthetic merge", env=env)
        self.git("checkout", "--detach", "-q", merge)
        event = {"pull_request": {"base": {"sha": base}, "head": {"sha": head}}}
        clean = self.ci("pull_request", event)
        self.assertEqual(clean.returncode, 0, clean.stderr)
        self.git("checkout", "--detach", "-q", head)
        bad = self.commit(committer="new@example.invalid")
        event["pull_request"]["head"]["sha"] = bad
        self.assertEqual(self.ci("pull_request", event).returncode, 1)

    def test_rebased_feature_push_checks_submitted_history(self):
        self.commit()
        before = self.git("rev-parse", "HEAD")
        self.git("checkout", "--orphan", "main")
        base = self.commit()
        self.git("update-ref", "refs/remotes/origin/main", base)
        head = self.commit()
        clean = self.ci("push", self.push_event(head, before))
        self.assertEqual(clean.returncode, 0, clean.stderr)
        bad = self.commit(author="new@example.invalid")
        self.assertEqual(self.ci("push", self.push_event(bad, head)).returncode, 1)
        self.assertEqual(len(self.checker.check_identities(self.repository)), 1)

    def test_default_branch_push_checks_new_commits(self):
        base = self.commit()
        head = self.commit()
        event = self.push_event(head, base, "refs/heads/main")
        clean = self.ci("push", event)
        self.assertEqual(clean.returncode, 0, clean.stderr)
        event["after"] = self.commit(committer="new@example.invalid")
        self.assertEqual(self.ci("push", event).returncode, 1)

    def test_ci_checks_complete_history_when_default_branch_previous_head_is_unavailable(self):
        head = self.commit()
        event = self.push_event(head, "f" * 40, "refs/heads/main")
        clean = self.ci("push", event)
        self.assertEqual(clean.returncode, 0, clean.stderr)
        event["after"] = self.commit(author="private@example.invalid")
        private = self.ci("push", event)
        self.assertEqual(private.returncode, 1)
        self.assertNotIn("private@example.invalid", private.stdout + private.stderr)

    def test_initial_default_branch_and_tag_push_check_all_history(self):
        head = self.commit(author="legacy@example.invalid")
        for ref in ["refs/heads/main", "refs/tags/v1"]:
            with self.subTest(ref=ref):
                self.assertEqual(self.ci("push", self.push_event(head, ref=ref)).returncode, 1)

    def test_ci_fails_closed_for_missing_or_invalid_required_context(self):
        head = self.commit()
        events = [
            ("pull_request", {}),
            ("pull_request", {"pull_request": {"base": {"sha": "f" * 40}, "head": {"sha": head}}}),
            ("pull_request", {"pull_request": {"base": {"sha": head}, "head": {"sha": "--all"}}}),
            ("push", self.push_event(head)),  # default branch ref is unavailable
            ("push", {**self.push_event(head), "repository": {"default_branch": "../bad"}}),
            ("push", {**self.push_event(head), "after": "0" * 40}),
            ("push", {**self.push_event(head), "before": "--all"}),
            ("unknown", {}),
        ]
        for name, event in events:
            with self.subTest(name=name, event=event):
                result = self.ci(name, event)
                self.assertEqual(result.returncode, 2, result.stderr)

    def test_ci_rejects_inherited_violations_in_pr_base(self):
        base = self.commit(author="inherited@example.invalid")
        head = self.commit()
        event = {"pull_request": {"base": {"sha": base}, "head": {"sha": head}}}
        result = self.ci("pull_request", event)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn(base, result.stderr)

    def test_ci_rejects_inherited_violations_on_default_and_feature_pushes(self):
        base = self.commit(author="inherited@example.invalid")
        self.git("update-ref", "refs/remotes/origin/main", base)
        head = self.commit()
        for ref in ["refs/heads/main", "refs/heads/topic", "refs/tags/v1"]:
            with self.subTest(ref=ref):
                result = self.ci("push", self.push_event(head, base, ref))
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(base, result.stderr)


class WorkflowIsolationTests(unittest.TestCase):
    def test_identity_failure_cannot_skip_secret_scan(self):
        workflow = (SCRIPT.parents[1] / "workflows" / "ci.yml").read_text(encoding="utf-8")
        match = re.search(r"\n  secret-scan:\n(.*?)(?=\n  [a-z-]+:\n|\Z)", workflow, re.S)
        self.assertIsNotNone(match, "missing independent secrets job")
        job = match.group(1)
        self.assertNotIn("check_commit_identity.py", job)
        self.assertNotRegex(job, r"(?m)^\s*needs:")
        self.assertIn("fetch-depth: 0", job)
        self.assertIn("gitleaks git . --log-opts=--all --redact", job)
        self.assertIn("\n  commit-identities:\n", workflow)
        gate = re.search(r"\n  secrets:\n(.*?)(?=\n  [a-z-]+:\n|\Z)", workflow, re.S).group(1)
        self.assertIn("needs: [secret-scan, commit-identities]", gate)
        self.assertIn("if: ${{ always() }}", gate)
        self.assertIn('test "$SCAN_RESULT" = success && test "$IDENTITY_RESULT" = success', gate)

    def test_release_identity_gate_does_not_prevent_credential_scanning(self):
        workflow = (SCRIPT.parents[1] / "workflows" / "release.yml").read_text(encoding="utf-8")
        scan = re.search(r"\n  secrets:\n(.*?)(?=\n  [a-z-]+:\n|\Z)", workflow, re.S).group(1)
        self.assertIn("needs: source", scan)
        self.assertNotIn("commit-identities", scan)
        self.assertIn("gitleaks git . --log-opts=--all --redact", scan)
        publish = re.search(r"\n  publish:\n(.*)", workflow, re.S).group(1)
        self.assertIn("needs: [source, cli, vscode, commit-identities]", publish)


if __name__ == "__main__":
    unittest.main()
