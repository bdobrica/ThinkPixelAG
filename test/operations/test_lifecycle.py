"""Operational safety checks; these never connect to Kubernetes."""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from lifecycle import Lifecycle, immutable_image

OLD = 'registry.example/ag@sha256:' + 'a' * 64
NEW = 'registry.example/ag@sha256:' + 'b' * 64


class UpgradeFixture(Lifecycle):
    def __init__(self, fail=False):
        self.a = SimpleNamespace(candidate=NEW, api='api', namespace='ops')
        self.report = {'checks': []}
        self.images = []
        self.fail = fail
        self.replays = []

    def save(self):
        pass

    def posture(self):
        pass

    def workflow(self, name):
        if name == 'upgraded' and self.fail:
            raise RuntimeError('candidate workflow failed')
        return name

    def replay(self, intent, name):
        self.replays.append((intent, name))

    def sql(self, query):
        return '18'

    def set_image(self, image):
        self.images.append(image)

    def k(self, *args, **kwargs):
        if args[:2] == ('get', 'deployment/api'):
            return json.dumps({'spec': {'template': {'spec': {'containers': [{'name': 'api', 'image': OLD}]}}}})
        if args[:2] == ('get', 'job/migrate-018'):
            return json.dumps({'spec': {'template': {'spec': {'containers': [{'image': OLD}]}}}})
        return ''


class LifecycleSafetyTests(unittest.TestCase):
    def test_candidate_failure_restores_original_digest(self):
        drill = UpgradeFixture(fail=True)
        with self.assertRaises(RuntimeError):
            drill.upgrade()
        self.assertEqual(drill.images, [NEW, OLD])
        self.assertNotIn(('upgraded', 'rollback_candidate'), drill.replays)

    def test_rollback_checks_state_created_by_both_images(self):
        drill = UpgradeFixture()
        drill.upgrade()
        self.assertEqual(drill.images, [NEW, OLD])
        self.assertIn(('before', 'rollback_original'), drill.replays)
        self.assertIn(('upgraded', 'rollback_candidate'), drill.replays)

    def test_mutable_tag_is_rejected(self):
        with self.assertRaises(ValueError):
            immutable_image('registry.example/ag:latest')

    def test_uncertain_admission_is_not_retried_on_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            drill = Lifecycle.__new__(Lifecycle)
            drill.a = SimpleNamespace(state_dir=Path(directory))
            drill.identity = {'Agent': '019feba6-b9bb-7fff-bfff-fffffffffff1'}
            drill.report = {'checks': []}
            drill.save = lambda: None
            requests = []
            def http(*args):
                requests.append(args)
                return 503, b'{}'
            drill.http = http
            with self.assertRaises(RuntimeError):
                drill.workflow()
            with self.assertRaises(FileExistsError):
                drill.workflow()
            self.assertEqual(len(requests), 1)
            self.assertEqual((Path(directory)/'workflow.json').stat().st_mode & 0o777, 0o600)


if __name__ == '__main__':
    unittest.main()
