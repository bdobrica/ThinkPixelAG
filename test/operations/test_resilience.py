"""Safety regressions for the opt-in promotion orchestrator; no cluster needed."""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

from resilience import Drill


class PromotionFixture(Drill):
    def __init__(self, directory, uncertain=False, mismatch=False):
        self.a = SimpleNamespace(state_dir=Path(directory), primary='old', standby='new', api='api', database_service='db', invariants=Path(directory)/'invariants.sql')
        self.a.invariants.write_text('invariants')
        self.uncertain = uncertain
        self.mismatch = mismatch
        self.scales = []
        self.promoted = False
        self.routed = False
        self.report = {'checks': []}

    def save(self):
        pass

    def k(self, *args, **kwargs):
        if args[:2] == ('get', 'service/db'):
            return json.dumps({'spec': {'selector': {'app': 'old'}}})
        if args[0] == 'get':
            name = args[1].split('/')[1]
            return json.dumps({'spec': {'replicas': 3 if name == 'api' else (0 if ('old', 0) in self.scales and name == 'old' else 1), 'selector': {'matchLabels': {'app': name}}, 'template': {'spec': {'nodeSelector': {'kubernetes.io/hostname': name}}}}})
        if args[:2] == ('patch', 'service/db'):
            assert self.promoted
            assert ('old', 0) in self.scales
            self.routed = True
            return ''
        raise AssertionError(args)

    def sql(self, statement, deployment=None):
        if 'pg_promote' in statement:
            assert ('old', 0) in self.scales
            assert (self.a.state_dir/'promotion-attempted').exists()
            if self.uncertain:
                raise RuntimeError('connection lost during promotion')
            self.promoted = True
            return 't'
        if 'pg_is_in_recovery' in statement:
            return 'f' if self.promoted else 't'
        if 'pg_current_wal_flush_lsn' in statement:
            return '0/100'
        return 't'

    def scale(self, name, count):
        self.scales.append((name, count))

    def wait_stopped(self, name):
        assert (name, 0) in self.scales

    def snapshot(self, name):
        return {'rows': 'different' if self.mismatch and name == 'new' else 'same'}

    def run_path(self):
        return '/run'

    def request(self, path):
        return 200, 'same-response-hash'

    def rollout(self):
        assert self.routed


class PromotionSafetyTests(unittest.TestCase):
    def test_unknown_promotion_outcome_never_restarts_original_writer(self):
        with tempfile.TemporaryDirectory() as directory:
            drill = PromotionFixture(directory, uncertain=True)
            with self.assertRaises(RuntimeError):
                drill.promote()
            self.assertEqual(drill.scales, [('api', 0), ('old', 0)])
            self.assertFalse(drill.routed)
            self.assertTrue((Path(directory)/'promotion-attempted').exists())

    def test_mismatched_replica_never_promotes_and_restores_original_service(self):
        with tempfile.TemporaryDirectory() as directory:
            drill = PromotionFixture(directory, mismatch=True)
            with self.assertRaises(RuntimeError):
                drill.promote()
            self.assertFalse(drill.promoted)
            self.assertFalse(drill.routed)
            self.assertIn(('old', 1), drill.scales)
            self.assertIn(('api', 3), drill.scales)

    def test_success_routes_only_after_fencing_and_never_restarts_old_writer(self):
        with tempfile.TemporaryDirectory() as directory:
            drill = PromotionFixture(directory)
            drill.promote()
            self.assertEqual(drill.scales, [('api', 0), ('old', 0), ('api', 3)])
            self.assertTrue(drill.routed)
            self.assertTrue(all(c['passed'] for c in drill.report['checks']))


if __name__ == '__main__':
    unittest.main()
