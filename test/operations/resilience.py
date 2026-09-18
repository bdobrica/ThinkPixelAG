#!/usr/bin/env python3
"""Opt-in retained-cluster fault checks. No automatic resource teardown.

Run only against an isolated operations fixture. SSH/kubectl administration is
required. Credentials and raw responses never enter the aggregate report.
"""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import shlex
import subprocess
import time
from urllib.parse import urlsplit
import uuid


class Drill:
    def __init__(self, args):
        self.a = args
        self.base = urlsplit(args.base)
        if self.base.scheme not in ('http', 'https') or not self.base.hostname or self.base.username or self.base.password or self.base.query or self.base.fragment or self.base.path not in ('', '/'):
            raise ValueError('base must be an HTTP(S) origin')
        self.identity = json.loads((args.identities / 'identity.json').read_text())
        self.tenant = str(uuid.UUID(self.identity['Tenant']))
        self.token = (args.identities / 'caller.token').read_text().strip()
        args.state_dir.mkdir(parents=True, exist_ok=True)
        self.report = {'scenario': args.scenario, 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'checks': []}
        self.save()

    def k(self, *args, body=None):
        command = shlex.join(['sudo', 'kubectl', '-n', self.a.namespace, *args])
        p = subprocess.run(['ssh', self.a.ssh_host, command], input=body, text=True, capture_output=True, timeout=360)
        if p.returncode:
            raise RuntimeError('cluster command failed: ' + str(args[:2]))
        return p.stdout

    def sql(self, query, deployment=None):
        return self.k('exec', '-i', 'deployment/' + (deployment or self.a.primary), '--', 'psql', '-U', self.a.database_user, '-d', self.a.database, '-At', '-v', 'ON_ERROR_STOP=1', body=query).strip()

    def request(self, path):
        kind = http.client.HTTPSConnection if self.base.scheme == 'https' else http.client.HTTPConnection
        conn = kind(self.base.hostname, self.base.port, timeout=20)
        try:
            conn.request('GET', path, headers={'Authorization': 'Bearer ' + self.token})
            response = conn.getresponse()
            body = response.read()
            return response.status, hashlib.sha256(body).hexdigest()
        except OSError:
            return 0, ''
        finally:
            conn.close()

    def save(self):
        self.a.report.write_text(json.dumps(self.report, indent=2) + '\n')

    def check(self, name, condition, **data):
        self.report['checks'].append({'name': name, 'passed': bool(condition), **data})
        self.save()
        if not condition:
            raise RuntimeError('assertion failed: ' + name)

    def ready(self, seconds=240):
        started = time.monotonic()
        while time.monotonic() - started < seconds:
            if self.request('/readyz')[0] == 200:
                return round(time.monotonic() - started, 3)
            time.sleep(2)
        raise RuntimeError('readiness recovery timed out')

    def scale(self, deployment, count):
        self.k('scale', 'deployment/' + deployment, '--replicas=' + str(count))

    def wait_stopped(self, deployment):
        value = json.loads(self.k('get', 'deployment/' + deployment, '-o', 'json'))
        selector = ','.join(k + '=' + v for k, v in value['spec']['selector']['matchLabels'].items())
        self.k('wait', '--for=delete', 'pod', '-l', selector, '--timeout=180s')

    def rollout(self):
        self.k('rollout', 'status', 'deployment/' + self.a.api, '--timeout=240s')
        self.ready()

    def run_path(self):
        run = self.sql("SELECT id FROM runs WHERE tenant_id='" + self.tenant + "' ORDER BY created_at DESC,id DESC LIMIT 1;")
        return '/v1/runs/' + str(uuid.UUID(run))

    def prepare(self):
        import ipaddress
        import re
        import secrets
        if not self.a.standby_node:
            raise ValueError('prepare requires --standby-node')
        cidr = str(ipaddress.ip_network(self.a.pod_cidr))
        source = json.loads(self.k('get', 'deployment/' + self.a.primary, '-o', 'json'))
        if (self.a.state_dir / 'promotion-attempted').exists() or self.sql('SELECT pg_is_in_recovery();') != 'f':
            raise RuntimeError('prepare requires the original unfenced primary')
        private = self.a.state_dir / 'state.json'
        if not private.exists():
            # O_EXCL and 0600 keep newly generated credentials private.
            import os
            fd = os.open(private, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            with os.fdopen(fd, 'w') as output:
                json.dump({key: secrets.token_urlsafe(40) for key in ('replication_password', 'valkey_password', 'cache_key')}, output)
        state = json.loads(private.read_text())
        if any(not re.fullmatch(r'[A-Za-z0-9_-]{32,128}', state[key]) for key in ('replication_password', 'valkey_password', 'cache_key')):
            raise ValueError('private credential file has invalid values')
        self.sql("DO $$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='ops011_replicator') THEN CREATE ROLE ops011_replicator REPLICATION LOGIN; END IF; END $$;")
        self.sql("ALTER ROLE ops011_replicator PASSWORD '" + state['replication_password'] + "';")
        self.sql("ALTER SYSTEM SET wal_keep_size='256MB'; ALTER SYSTEM SET max_slot_wal_keep_size='256MB';")
        script = 'line="host replication ops011_replicator ' + cidr + ' scram-sha-256"\ngrep -qxF "$line" "$PGDATA/pg_hba.conf" || printf "%s\\n" "$line" >> "$PGDATA/pg_hba.conf"\n'
        self.k('exec', '-i', 'deployment/' + self.a.primary, '--', 'sh', '-s', body=script)
        self.sql('SELECT pg_reload_conf();')
        self.sql("SELECT pg_create_physical_replication_slot('ops011_standby') WHERE NOT EXISTS(SELECT 1 FROM pg_replication_slots WHERE slot_name='ops011_standby');")
        image = source['spec']['template']['spec']['containers'][0]['image']
        meta = lambda name: {'name': name, 'namespace': self.a.namespace}
        secret = lambda name, values: {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': meta(name), 'stringData': values}
        items = [secret('ops011-replication', {'password': state['replication_password']}), secret('ops011-valkey', {'password': state['valkey_password']}), {'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim', 'metadata': meta(self.a.standby), 'spec': {'accessModes': ['ReadWriteOnce'], 'resources': {'requests': {'storage': '4Gi'}}}}]
        bootstrap = '''set -eu
if [ ! -s "$PGDATA/PG_VERSION" ]; then
 mkdir -p "$PGDATA"
 chown postgres:postgres "$PGDATA"
 chmod 0700 "$PGDATA"
 gosu postgres pg_basebackup -h "$SOURCE_HOST" -U ops011_replicator -D "$PGDATA" -R -X stream --slot=ops011_standby --checkpoint=fast
fi
'''
        env = [{'name': 'PGDATA', 'value': '/var/lib/postgresql/18/docker'}, {'name': 'SOURCE_HOST', 'value': self.a.database_service}, {'name': 'PGPASSWORD', 'valueFrom': {'secretKeyRef': {'name': 'ops011-replication', 'key': 'password'}}}]
        mounts = [{'name': 'data', 'mountPath': '/var/lib/postgresql'}]
        pod = {'automountServiceAccountToken': False, 'nodeSelector': {'kubernetes.io/hostname': self.a.standby_node}, 'initContainers': [{'name': 'basebackup', 'image': image, 'command': ['sh', '-c', bootstrap], 'env': env, 'volumeMounts': mounts}], 'containers': [{'name': 'postgres', 'image': image, 'env': env, 'args': ['-c', 'max_connections=160', '-c', 'shared_buffers=512MB'], 'volumeMounts': mounts, 'resources': {'requests': {'cpu': '500m', 'memory': '768Mi'}, 'limits': {'cpu': '3', 'memory': '2Gi'}}, 'readinessProbe': {'exec': {'command': ['pg_isready', '-U', self.a.database_user, '-d', self.a.database]}, 'periodSeconds': 5}}], 'volumes': [{'name': 'data', 'persistentVolumeClaim': {'claimName': self.a.standby}}]}
        def deployment(name, spec, strategy=None):
            value = {'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': meta(name), 'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': name}}, 'template': {'metadata': {'labels': {'app': name}}, 'spec': spec}}}
            if strategy:
                value['spec']['strategy'] = strategy
            return value
        items.append(deployment(self.a.standby, pod, {'type': 'Recreate'}))
        valkey = {'name': 'valkey', 'image': self.a.valkey_image, 'command': ['sh', '-c', 'exec valkey-server --save "" --appendonly no --requirepass "$VALKEY_PASSWORD"'], 'env': [{'name': 'VALKEY_PASSWORD', 'valueFrom': {'secretKeyRef': {'name': 'ops011-valkey', 'key': 'password'}}}], 'resources': {'requests': {'cpu': '100m', 'memory': '64Mi'}, 'limits': {'cpu': '1', 'memory': '128Mi'}}, 'securityContext': {'allowPrivilegeEscalation': False, 'capabilities': {'drop': ['ALL']}, 'runAsUser': 999, 'runAsGroup': 1000}, 'readinessProbe': {'tcpSocket': {'port': 6379}, 'periodSeconds': 3}}
        items.append(deployment('ops011-valkey', {'automountServiceAccountToken': False, 'containers': [valkey]}))
        api = json.loads(self.k('get', 'deployment/' + self.a.api, '-o', 'json'))
        opa = next(c for c in api['spec']['template']['spec']['containers'] if c['name'] == 'opa')
        opa['args'] = [a.replace('127.0.0.1:8181', '0.0.0.0:8181') for a in opa['args']]
        items.append(deployment('ops011-policy', {'automountServiceAccountToken': False, 'securityContext': api['spec']['template']['spec']['securityContext'], 'containers': [opa], 'volumes': [{'name': 'policy', 'configMap': {'name': self.a.policy}}]}))
        for name, port in [('ops011-valkey', 6379), ('ops011-policy', 8181)]:
            items.append({'apiVersion': 'v1', 'kind': 'Service', 'metadata': meta(name), 'spec': {'selector': {'app': name}, 'ports': [{'port': port, 'targetPort': port}]}})
        if api['spec']['replicas'] < 2:
            raise ValueError('disruption drill requires at least two API replicas')
        items.append({'apiVersion': 'policy/v1', 'kind': 'PodDisruptionBudget', 'metadata': meta(self.a.api), 'spec': {'minAvailable': api['spec']['replicas'] - 1, 'selector': api['spec']['selector']}})
        self.k('apply', '-f', '-', body=json.dumps({'apiVersion': 'v1', 'kind': 'List', 'items': items}))
        for name in (self.a.standby, 'ops011-valkey', 'ops011-policy'):
            self.k('rollout', 'status', 'deployment/' + name, '--timeout=300s')
        # Match the production API's five-second preStop drain. Keep its
        # fixture OPA sidecar alive through that window as well.
        patch = {'spec': {'template': {'spec': {'terminationGracePeriodSeconds': 45, 'containers': [{'name': 'api', 'lifecycle': {'preStop': {'sleep': {'seconds': 5}}}}, {'name': 'opa', 'lifecycle': {'preStop': {'sleep': {'seconds': 10}}}}]}}}}
        self.k('patch', 'deployment/' + self.a.api, '--type=strategic', '-p', json.dumps(patch))
        self.rollout()
        self.check('retained_resources_ready', True)

    def runtime(self):
        path = self.run_path()
        self.check('baseline_authorized_read', self.request(path)[0] == 200)
        api = json.loads(self.k('get', 'deployment/' + self.a.api, '-o', 'json'))
        opa = next(c for c in api['spec']['template']['spec']['containers'] if c['name'] == 'opa')
        original_args = opa['args']
        def set_args(args):
            patch = {'spec': {'template': {'spec': {'containers': [{'name': 'opa', 'args': args}]}}}}
            self.k('patch', 'deployment/' + self.a.api, '--type=strategic', '-p', json.dumps(patch))
            self.rollout()
        try:
            set_args([a.replace('127.0.0.1:8181', '127.0.0.1:8182') for a in original_args])
            statuses = [self.request(path)[0] for _ in range(3)]
            self.check('opa_outage_fail_closed', all(s in (0, 503) for s in statuses) and 503 in statuses, statuses=statuses, transport_failures=statuses.count(0))
        finally:
            set_args(original_args)
        self.check('opa_outage_recovered', self.request(path)[0] == 200)
        policy = json.loads(self.k('get', 'configmap/' + self.a.policy, '-o', 'json'))['data']
        # Inject malformed decision output, not a valid policy DENY.
        malformed = {'authorization.rego': 'package thinkpixelag.authorization\nimport rego.v1\ndecision := {"allow": "invalid"}\n'}
        try:
            self.k('patch', 'configmap/' + self.a.policy, '--type=merge', '-p', json.dumps({'data': malformed}))
            self.k('rollout', 'restart', 'deployment/' + self.a.api)
            self.rollout()
            statuses = [self.request(path)[0] for _ in range(3)]
            self.check('opa_malformed_fail_closed', all(s in (0, 503) for s in statuses) and 503 in statuses, statuses=statuses, transport_failures=statuses.count(0))
        finally:
            self.k('patch', 'configmap/' + self.a.policy, '--type=merge', '-p', json.dumps({'data': policy}))
            self.k('rollout', 'restart', 'deployment/' + self.a.api)
            self.rollout()
        self.check('opa_malformed_recovered', self.request(path)[0] == 200)
        self.disruptions()

    def disruptions(self):
        path = self.run_path()
        pods = json.loads(self.k('get', 'pod', '-l', 'app=' + self.a.api, '-o', 'json'))['items']
        pod = next(p for p in pods if not p['metadata'].get('deletionTimestamp'))['metadata']['name']
        eviction = {'apiVersion': 'policy/v1', 'kind': 'Eviction', 'metadata': {'name': pod, 'namespace': self.a.namespace}}
        self.k('create', '--raw', '/api/v1/namespaces/' + self.a.namespace + '/pods/' + pod + '/eviction', '-f', '-', body=json.dumps(eviction))
        self.k('wait', '--for=delete', 'pod/' + pod, '--timeout=180s')
        self.rollout()
        self.check('pod_eviction_recovered', self.request(path)[0] == 200)
        self.k('rollout', 'restart', 'deployment/' + self.a.api)
        statuses = []
        for _ in range(20):
            statuses.append(self.request(path)[0]); time.sleep(.25)
        self.rollout()
        self.check('rolling_restart_sampled_availability', all(s == 200 for s in statuses), samples=len(statuses), errors=sum(s != 200 for s in statuses))

    def latency(self):
        path = self.run_path()
        # Preserve the exact existing setting, including an existing auto.conf value.
        old = self.sql('SHOW pre_auth_delay;')
        if old != '0':
            raise RuntimeError('latency injection requires default pre_auth_delay')
        try:
            self.sql("ALTER SYSTEM SET pre_auth_delay='5s'; SELECT pg_reload_conf();")
            self.sql("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend';")
            statuses = [self.request(path)[0] for _ in range(3)]
            self.check('database_latency_no_allow', all(s == 0 or s >= 500 for s in statuses), statuses=statuses)
        finally:
            self.sql('ALTER SYSTEM RESET pre_auth_delay; SELECT pg_reload_conf();')
        recovered = self.ready()
        self.check('database_latency_recovered', self.request(path)[0] == 200, recovery_seconds=recovered)

    def snapshot(self, deployment):
        # Fixed table names; hashes bind complete row contents without exporting them.
        tables = ('tenants', 'principals', 'agents', 'agent_versions', 'agent_capabilities', 'agent_version_approvals', 'policy_bundles', 'policy_activations', 'runs', 'run_version_resolutions', 'run_signals', 'run_events', 'idempotency_records', 'resource_dimensions', 'resource_envelopes', 'resource_envelope_grants', 'resource_balances', 'resource_reservations', 'resource_reservation_items', 'trusted_usage_entries', 'resource_settlements', 'resource_settlement_items', 'resource_extensions', 'resource_extension_items', 'resource_rate_windows', 'governance_approval_requests', 'governance_approval_decisions', 'governance_approval_consumptions', 'break_glass_grants', 'break_glass_events', 'outbox_messages', 'audit_events', 'revocations', 'revocation_changes', 'revocation_log', 'security_epochs', 'tenant_security_epochs', 'agent_security_epochs', 'gateway_checkpoints', 'evidence_delivery_receipts', 'evidence_sink_checkpoints')
        result = {}
        for table in tables:
            value = self.sql("SELECT count(*)||':'||md5(coalesce(string_agg(row_to_json(t)::text,E'\\n' ORDER BY row_to_json(t)::text),'')) FROM " + table + ' t;', deployment)
            result[table] = value
        return result

    def promote(self):
        marker = self.a.state_dir / 'promotion-attempted'
        if marker.exists():
            raise RuntimeError('promotion already attempted; inspect retained roles before another drill')
        source = json.loads(self.k('get', 'deployment/' + self.a.primary, '-o', 'json'))
        self.check('single_source_writer', source['spec']['replicas'] == 1)
        standby = json.loads(self.k('get', 'deployment/' + self.a.standby, '-o', 'json'))
        self.check('standby_in_recovery', self.sql('SELECT pg_is_in_recovery();', self.a.standby) == 't')
        self.check('separate_nodes', source['spec']['template']['spec']['nodeSelector']['kubernetes.io/hostname'] != standby['spec']['template']['spec']['nodeSelector']['kubernetes.io/hostname'])
        api_count = json.loads(self.k('get', 'deployment/' + self.a.api, '-o', 'json'))['spec']['replicas']
        old_selector = json.loads(self.k('get', 'service/' + self.a.database_service, '-o', 'json'))['spec']['selector']
        self.check('writer_service_targets_source', old_selector == source['spec']['selector']['matchLabels'])
        path = self.run_path()
        before_http = self.request(path)
        self.check('prepromotion_read', before_http[0] == 200)
        self.scale(self.a.api, 0)
        attempted = False
        promoted = False
        started = time.monotonic()
        try:
            self.wait_stopped(self.a.api)
            before = self.snapshot(self.a.primary)
            lsn = self.sql('SELECT pg_current_wal_flush_lsn();')
            deadline = time.monotonic() + 240
            while time.monotonic() < deadline:
                if self.sql("SELECT pg_last_wal_replay_lsn()>='" + lsn + "'::pg_lsn;", self.a.standby) == 't':
                    break
                time.sleep(2)
            else:
                raise RuntimeError('standby did not reach the quiesced WAL barrier')
            self.check('standby_matches_authoritative_rows', self.snapshot(self.a.standby) == before, tables=len(before))
            self.scale(self.a.primary, 0)
            self.wait_stopped(self.a.primary)
            self.check('source_fenced_before_promotion', json.loads(self.k('get', 'deployment/' + self.a.primary, '-o', 'json'))['spec']['replicas'] == 0)
            with os.fdopen(os.open(marker, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as checkpoint:
                checkpoint.write('Never restart the old primary as a writer. Promotion may have happened even if the request timed out.\n')
                checkpoint.flush()
                os.fsync(checkpoint.fileno())
            directory = os.open(marker.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
            attempted = True
            self.sql('SELECT pg_promote(true,120);', self.a.standby)
            self.check('standby_promoted', self.sql('SELECT pg_is_in_recovery();', self.a.standby) == 'f')
            promoted = True
            self.k('patch', 'service/' + self.a.database_service, '--type=merge', '-p', json.dumps({'spec': {'selector': standby['spec']['selector']['matchLabels']}}))
            self.check('promoted_rows_preserved', self.snapshot(self.a.standby) == before, tables=len(before))
            self.sql(self.a.invariants.read_text(), self.a.standby)
            self.check('promoted_database_invariants', True)
        finally:
            if not attempted:
                self.scale(self.a.primary, 1)
                self.scale(self.a.api, api_count)
            elif promoted:
                self.scale(self.a.api, api_count)
            # Unknown promotion outcome intentionally leaves the old primary
            # fenced and the API stopped; automatic fallback risks split brain.
        self.rollout()
        after_http = self.request(path)
        self.check('api_reconnected_to_promoted_database', after_http == before_http, interruption_seconds=round(time.monotonic()-started, 3))
        self.report['retained_topology'] = {'writer_deployment': self.a.standby, 'fenced_deployment': self.a.primary, 'service': self.a.database_service, 'automatic_failover': False, 'quiesced_promotion': True}
        self.save()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ssh-host', required=True)
    parser.add_argument('--namespace', required=True)
    parser.add_argument('--base', required=True)
    parser.add_argument('--identities', type=Path, required=True)
    parser.add_argument('--state-dir', type=Path, required=True)
    parser.add_argument('--report', type=Path, required=True)
    parser.add_argument('--scenario', choices=['prepare', 'runtime', 'latency', 'promote', 'disruptions'], required=True)
    parser.add_argument('--standby-node')
    parser.add_argument('--pod-cidr', default='10.42.0.0/16')
    parser.add_argument('--valkey-image', default='valkey/valkey:9.1.1-alpine3.24@sha256:ee91f7a174ac4d6a6b0685b3a60e321f0a9dbbb691f9b0e285be2ba1d1be8328')
    parser.add_argument('--api', default='ops-api')
    parser.add_argument('--policy', default='ops-policy')
    parser.add_argument('--primary', default='postgres')
    parser.add_argument('--standby', default='ops011-standby')
    parser.add_argument('--database-service', default='postgres')
    parser.add_argument('--database-user', default='thinkpixelag')
    parser.add_argument('--database', default='thinkpixelag')
    parser.add_argument('--invariants', type=Path, default=Path('scripts/check-restored-invariants.sql'))
    parser.add_argument('--allow-fault-injection', action='store_true', required=True)
    args = parser.parse_args()
    drill = Drill(args)
    try:
        getattr(drill, args.scenario)()
    except Exception as error:
        drill.report['error_category'] = type(error).__name__
        drill.save()
        print('Resilience scenario failed; inspect aggregate report and retained resource roles.', flush=True)
        return 1
    print('Resilience scenario passed: ' + args.scenario, flush=True)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
