"""Safety regressions for retained-cluster fault boundaries; no cluster needed."""
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



class DrainingFaultFixture(Drill):
    """Old healthy endpoints remain usable after new replicas become ready."""
    def __init__(self):
        self.a = SimpleNamespace(api='api', policy='policy')
        self.report = {'checks': []}
        self.pending = set()
        self.port_fault = False
        self.policy = {'authorization.rego': 'valid'}

    def save(self):
        pass

    def run_path(self):
        return '/run'

    def rollout(self):
        pass  # New replicas ready; old pods are still draining.

    def disruptions(self):
        pass

    def request(self, path):
        faulty = self.port_fault or 'invalid' in self.policy['authorization.rego']
        return (503 if faulty and not self.pending else 200), ''

    def k(self, *args, **kwargs):
        if args[:2] == ('get', 'deployment/api'):
            return json.dumps({'spec': {'template': {'spec': {'containers': [{'name': 'opa', 'args': ['--addr=127.0.0.1:8181']}]}}}})
        if args[:2] == ('get', 'configmap/policy'):
            return json.dumps({'data': self.policy})
        if args[:2] == ('get', 'pod'):
            return json.dumps({'items': [{'metadata': {'name': n}} for n in ['api-old-1', 'api-old-2']]})
        if args[:2] == ('patch', 'configmap/policy'):
            self.policy = json.loads(args[-1])['data']
            return ''
        if args[:2] == ('patch', 'deployment/api'):
            self.port_fault = '8182' in args[-1]
            self.pending = {'api-old-1', 'api-old-2'}
            return ''
        if args[:2] == ('rollout', 'restart'):
            self.pending = {'api-old-1', 'api-old-2'}
            return ''
        if args[:2] == ('wait', '--for=delete'):
            self.pending.remove(args[2].split('/')[1])
            return ''
        raise AssertionError(args)


class FaultBoundaryTests(unittest.TestCase):
    def test_faults_are_checked_only_after_healthy_old_endpoints_stop(self):
        drill = DrainingFaultFixture()
        drill.runtime()
        self.assertTrue(all(c['passed'] for c in drill.report['checks']))
        self.assertEqual([c['statuses'] for c in drill.report['checks'] if 'statuses' in c], [[503]*3, [503]*3])
        self.assertFalse(drill.pending)

if __name__ == '__main__':
    unittest.main()
