#!/usr/bin/env python3
"""Opt-in lifecycle rehearsal for the retained, isolated OPS fixture."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
from pathlib import Path
import re
import threading
import time
import uuid

from resilience import Drill


def immutable_image(value):
    if not re.fullmatch(r'[a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}', value):
        raise ValueError('an immutable image reference is required')
    return value


class Lifecycle(Drill):
    def http(self, method, path, body=None, key=None, token=None):
        connection = self.connection()
        headers = {'Authorization': 'Bearer ' + (self.token if token is None else token), 'Content-Type': 'application/json'}
        if key:
            headers['Idempotency-Key'] = key
        try:
            connection.request(method, path, None if body is None else json.dumps(body), headers)
            response = connection.getresponse()
            return response.status, response.read()
        finally:
            connection.close()

    def connection(self):
        kind = http.client.HTTPSConnection if self.base.scheme == 'https' else http.client.HTTPConnection
        return kind(self.base.hostname, self.base.port, timeout=20)

    def stream(self, path, count, cursor=None):
        connection = self.connection()
        headers = {'Authorization': 'Bearer ' + self.token}
        if cursor:
            headers['Last-Event-ID'] = cursor
        events, event = [], {}
        try:
            connection.request('GET', path + '/events', headers=headers)
            response = connection.getresponse()
            if response.status != 200:
                raise RuntimeError('event stream unavailable')
            deadline = time.monotonic() + 30
            while len(events) < count and time.monotonic() < deadline:
                line = response.readline(65537)
                if not line or len(line) > 65536:
                    break
                line = line.decode().strip()
                if not line and 'data' in event:
                    events.append(event)
                    event = {}
                elif ': ' in line:
                    k, v = line.split(': ', 1)
                    if k in ('id', 'event', 'data'):
                        event[k] = json.loads(v) if k == 'data' else v
            return events
        finally:
            connection.close()

    def workflow(self, name='workflow'):
        # Persist intent before admission. Never automatically retry an uncertain
        # mutation; restarting with this state directory requires inspection.
        checkpoint = self.a.state_dir / (name + '.json')
        body = {'objective': 'OPS-012 synthetic lifecycle', 'input': {}, 'constraints': {'max_execution_time_seconds': 300, 'max_llm_tokens': 100, 'max_tool_calls': 10}}
        intent = {'key': 'ops012-' + str(uuid.uuid4()), 'body': body}
        with os.fdopen(os.open(checkpoint, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as output:
            json.dump(intent, output)
            output.flush()
            os.fsync(output.fileno())
        agent = str(uuid.UUID(self.identity['Agent']))
        admission = '/v1/agents/' + agent + '/runs'
        status, original = self.http('POST', admission, body, intent['key'])
        self.check(name + '_admitted', status == 201, status=status)
        run = str(uuid.UUID(json.loads(original)['id']))
        path = '/v1/runs/' + run
        intent.update({'run': run, 'response': original.decode()})
        checkpoint.write_text(json.dumps(intent))
        self.check(name + '_admission_replay', self.http('POST', admission, body, intent['key']) == (201, original))
        self.check(name + '_key_conflict', self.http('POST', admission, {**body, 'objective': 'different'}, intent['key'])[0] == 409)
        self.check(name + '_authorized_read', self.http('GET', path)[0] == 200)
        foreign = (self.a.identities / 'foreign.token').read_text().strip()
        self.check(name + '_tenant_isolation', self.http('GET', path, token=foreign)[0] == 404)
        self.check(name + '_forged_token_denied', self.http('GET', path, token='invalid')[0] == 401)
        time.sleep(5)
        signal = {'type': 'CUSTOM', 'payload': {'name': 'ops012.check', 'data': {}}}
        key = 'ops012-signal-' + str(uuid.uuid4())
        result = self.http('POST', path + '/signals', signal, key)
        self.check(name + '_signal', result[0] == 202, status=result[0])
        self.check(name + '_signal_replay', self.http('POST', path + '/signals', signal, key) == result)
        time.sleep(5)
        key = 'ops012-cancel-' + str(uuid.uuid4())
        result = self.http('POST', path + '/cancel', {'reason_code': 'caller.request'}, key)
        self.check(name + '_cancel', result[0] == 200 and json.loads(result[1])['state'] == 'CANCELLED', status=result[0])
        self.check(name + '_cancel_replay', self.http('POST', path + '/cancel', {'reason_code': 'caller.request'}, key) == result)
        events = self.stream(path, 3)
        self.check(name + '_ordered_events', [e['event'] for e in events] == ['run.admitted', 'run.signal.accepted', 'run.cancelled'] and [e['data']['sequence'] for e in events] == [1, 2, 3])
        resumed = self.stream(path, 2, events[0]['id'])
        self.check(name + '_cursor_resume', resumed == events[1:])
        self.check(name + '_no_duplicate_events', self.sql("SELECT count(*) FROM run_events WHERE tenant_id='" + self.tenant + "' AND run_id='" + run + "';") == '3')
        self.sql(self.a.invariants.read_text())
        self.check(name + '_database_invariants', True)
        return intent

    def replay(self, intent, name):
        path = '/v1/agents/' + str(uuid.UUID(self.identity['Agent'])) + '/runs'
        self.check(name + '_durable_replay', self.http('POST', path, intent['body'], intent['key']) == (201, intent['response'].encode()))
        status, value = self.http('GET', '/v1/runs/' + intent['run'])
        self.check(name + '_terminal_state_preserved', status == 200 and json.loads(value)['state'] == 'CANCELLED')

    def posture(self):
        pods = json.loads(self.k('get', 'pods', '-l', 'app=' + self.a.api, '-o', 'json'))['items']
        self.check('pods_present', len(pods) >= 3)
        for pod in pods:
            spec = pod['spec']
            security = spec['securityContext']
            valid = security.get('runAsNonRoot') and security.get('runAsUser') == 65532 and security.get('seccompProfile', {}).get('type') == 'RuntimeDefault' and spec.get('automountServiceAccountToken') is False
            for container in spec['containers']:
                security = container['securityContext']
                valid = valid and security.get('readOnlyRootFilesystem') and security.get('allowPrivilegeEscalation') is False and security.get('capabilities', {}).get('drop') == ['ALL']
            self.check('running_pod_restricted', valid)
        self.check('schema_18', self.sql('SELECT version FROM thinkpixelag_schema_version;') == '18')
        self.check('ready', self.http('GET', '/readyz')[0] == 200)
        self.check('metrics', b'thinkpixelag_build_info' in self.http('GET', '/metrics')[1])

    def upgrade(self):
        candidate = immutable_image(self.a.candidate)
        deployment = json.loads(self.k('get', 'deployment/' + self.a.api, '-o', 'json'))
        original = immutable_image(next(c['image'] for c in deployment['spec']['template']['spec']['containers'] if c['name'] == 'api'))
        self.check('distinct_immutable_images', candidate != original)
        self.report['images'] = {'original': original, 'candidate': candidate}
        self.save()
        self.posture()
        prior = self.workflow('before')
        # Explicit migration job; no down-migration or API-startup migration.
        pod = json.loads(self.k('get', 'job/migrate-018', '-o', 'json'))['spec']['template']['spec']
        pod['containers'][0]['image'] = candidate
        job = 'ops012-migrate-' + candidate.rsplit(':', 1)[1][:12]
        resource = {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {'name': job, 'namespace': self.a.namespace}, 'spec': {'backoffLimit': 0, 'template': {'spec': pod}}}
        self.k('apply', '-f', '-', body=json.dumps(resource))
        self.k('wait', '--for=condition=complete', 'job/' + job, '--timeout=240s')
        self.check('candidate_migration_compatible', self.sql('SELECT version FROM thinkpixelag_schema_version;') == '18')
        try:
            self.set_image(candidate)
            self.replay(prior, 'upgrade')
            upgraded = self.workflow('upgraded')
            self.posture()
        finally:
            self.set_image(original)
        self.replay(prior, 'rollback_original')
        self.replay(upgraded, 'rollback_candidate')
        self.workflow('rollback')
        self.posture()

    def set_image(self, image):
        self.k('set', 'image', 'deployment/' + self.a.api, 'api=' + immutable_image(image))
        self.rollout()
        pods = json.loads(self.k('get', 'pods', '-l', 'app=' + self.a.api, '-o', 'json'))['items']
        active = [p for p in pods if not p['metadata'].get('deletionTimestamp')]
        self.check('image_converged', len(active) >= 3 and all(next(c['image'] for c in p['spec']['containers'] if c['name'] == 'api') == image and all(c['ready'] for c in p['status']['containerStatuses']) for p in active), image=image)

    def hpa(self):
        # Low-device diagnostic metric, explicitly separate from the production
        # 70% pod CPU policy. No requests, limits or production manifests change.
        name = 'ops012-api'
        metric = lambda value: [{'type': 'ContainerResource', 'containerResource': {'name': 'cpu', 'container': 'api', 'target': {'type': 'AverageValue', 'averageValue': value}}}]
        resource = {'apiVersion': 'autoscaling/v2', 'kind': 'HorizontalPodAutoscaler', 'metadata': {'name': name, 'namespace': self.a.namespace}, 'spec': {'scaleTargetRef': {'apiVersion': 'apps/v1', 'kind': 'Deployment', 'name': self.a.api}, 'minReplicas': 3, 'maxReplicas': 4, 'metrics': metric('20m'), 'behavior': {'scaleDown': {'stabilizationWindowSeconds': 60}}}}
        existing = json.loads(self.k('get', 'hpa', '-o', 'json'))['items']
        if any(h['spec']['scaleTargetRef']['name'] == self.a.api and h['metadata']['name'] != name for h in existing):
            raise RuntimeError('another HPA already controls this Deployment')
        initial = json.loads(self.k('get', 'deployment/' + self.a.api, '-o', 'json'))
        self.check('hpa_baseline_three', initial['spec']['replicas'] == 3 and initial['status'].get('availableReplicas') == 3)
        self.k('apply', '-f', '-', body=json.dumps(resource))
        stop = threading.Event()
        counts = {'requests': 0, 'errors': 0}
        lock = threading.Lock()
        path = self.run_path()
        def traffic():
            while not stop.is_set():
                start = time.monotonic()
                status = self.request(path)[0]
                with lock:
                    counts['requests'] += 1
                    counts['errors'] += int(status != 200)
                stop.wait(max(0, .4 - (time.monotonic() - start)))
        samples = []
        try:
            with ThreadPoolExecutor(max_workers=16) as executor:
                futures = [executor.submit(traffic) for _ in range(16)]
                try:
                    deadline = time.monotonic() + 300
                    while time.monotonic() < deadline:
                        value = json.loads(self.k('get', 'hpa/' + name, '-o', 'json'))['status']
                        samples.append({k: value.get(k) for k in ('currentReplicas', 'desiredReplicas', 'currentMetrics')})
                        if value.get('currentReplicas') == 4 and value.get('desiredReplicas') == 4:
                            self.rollout()
                            break
                        time.sleep(10)
                    self.check('hpa_scaled_up', any(v.get('currentReplicas') == 4 for v in samples), samples=samples)
                finally:
                    stop.set()
                    for future in futures:
                        future.result()
            self.check('bounded_read_traffic', counts['errors'] == 0, **counts, maximum_rate=40, concurrency=16)
            deadline = time.monotonic() + 240
            while time.monotonic() < deadline:
                value = json.loads(self.k('get', 'hpa/' + name, '-o', 'json'))['status']
                if value.get('currentReplicas') == 3 and value.get('desiredReplicas') == 3:
                    self.rollout()
                    self.check('hpa_scaled_down', True, retained_minimum=3, retained_maximum=4)
                    return
                time.sleep(10)
            self.check('hpa_scaled_down', False)
        finally:
            stop.set()
            self.report['traffic'] = dict(counts)
            self.save()
            # Retain HPA with an idle-safe ceiling matching 70% of the unchanged
            # 250m API request; retain the normal 300-second stabilization window.
            self.k('patch', 'hpa/' + name, '--type=merge', '-p', json.dumps({'spec': {'metrics': metric('175m'), 'behavior': {'scaleDown': {'stabilizationWindowSeconds': 300}}}}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for option in ('ssh-host', 'namespace', 'base'):
        parser.add_argument('--' + option, required=True)
    for option in ('identities', 'state-dir', 'report'):
        parser.add_argument('--' + option, type=Path, required=True)
    parser.add_argument('--scenario', choices=['workflow', 'posture', 'upgrade', 'hpa'], required=True)
    parser.add_argument('--api', default='ops-api')
    parser.add_argument('--primary', required=True, help='current writer Deployment, never the fenced old writer')
    parser.add_argument('--database-user', default='thinkpixelag')
    parser.add_argument('--database', default='thinkpixelag')
    parser.add_argument('--candidate')
    parser.add_argument('--invariants', type=Path, default=Path('scripts/check-restored-invariants.sql'))
    parser.add_argument('--allow-fault-injection', action='store_true', required=True)
    args = parser.parse_args()
    drill = Lifecycle(args)
    try:
        getattr(drill, args.scenario)()
    except Exception as error:
        drill.report['error_category'] = type(error).__name__
        drill.save()
        print('Lifecycle scenario failed; inspect aggregate report and private checkpoint before retrying.')
        return 1
    print('Lifecycle scenario passed: ' + args.scenario)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
