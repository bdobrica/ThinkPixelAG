#!/usr/bin/env python3
"""Render a private, repeatable Kubernetes evaluation installation (stdlib only)."""
import argparse
import json
import os
from pathlib import Path
import re
import secrets

AG_IMAGE = 'quay.io/bdobrica/thinkpixelag@sha256:82ad793f6b19460e58aeb43203bfd2f8a3563bc2d8bf53a120e92dbafa78a53a'
PG_IMAGE = 'postgres:18.4-alpine3.23@sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e'
OPA_IMAGE = 'openpolicyagent/opa:1.19.0-debug@sha256:ec3c7a29a21ce96d71231cb4befa2561205fe84e5a2dc3cc46ac7bc8bd21b3a4'

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--support-image', required=True, help='Published demo support image, pinned by SHA-256')
    parser.add_argument('--output', type=Path, default=Path.home() / '.local/state/thinkpixelag-poc')
    parser.add_argument('--namespace', default='thinkpixelag-poc')
    args = parser.parse_args()
    if not re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}', args.support_image):
        parser.error('support image must be an immutable registry digest')
    if not re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', args.namespace):
        parser.error('invalid namespace')
    os.umask(0o077)
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    args.output.chmod(0o700)
    if args.output.stat().st_mode & 0o077:
        parser.error('output must be on a filesystem enforcing private Unix permissions; choose a private home directory')
    private = args.output / 'credentials.json'
    if private.exists():
        credentials = json.loads(private.read_text())
        if credentials['namespace'] != args.namespace:
            parser.error('use a separate output directory for a different namespace')
    else:
        credentials = {name: secrets.token_hex(32) for name in ['database', 'cursor', 'evidence']}
        credentials['namespace'] = args.namespace
        with private.open('x') as f:
            json.dump(credentials, f)
    ns = args.namespace
    url = f'postgresql://thinkpixelag:{credentials["database"]}@postgres:5432/thinkpixelag?sslmode=disable'
    labels = lambda app: {'app': app, 'app.kubernetes.io/part-of': 'thinkpixelag-poc'}
    def obj(kind, name, **fields):
        versions = {'Deployment': 'apps/v1', 'Job': 'batch/v1', 'NetworkPolicy': 'networking.k8s.io/v1'}
        return {'apiVersion': versions.get(kind, 'v1'), 'kind': kind,
                'metadata': {'name': name, 'namespace': ns}, **fields}
    def pvc(name):
        return obj('PersistentVolumeClaim', name, spec={'accessModes': ['ReadWriteOnce'], 'resources': {'requests': {'storage': '1Gi'}}})
    def container(name, image, env=None, mounts=None, command=None):
        c = {'name': name, 'image': image, 'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True, 'capabilities': {'drop': ['ALL']}},
             'resources': {'requests': {'cpu': '100m', 'memory': '128Mi'}, 'limits': {'cpu': '1', 'memory': '512Mi'}}}
        if env: c['env'] = [{'name': k, 'value': v} for k, v in env.items()]
        if mounts: c['volumeMounts'] = [{'name': k, 'mountPath': v, 'readOnly': name in ['api', 'opa']} for k, v in mounts.items()]
        if command: c['command'] = command
        return c
    def pod(app, c, claims, uid=65532):
        return {'metadata': {'labels': labels(app)}, 'spec': {'automountServiceAccountToken': False,
            'securityContext': {'runAsNonRoot': True, 'runAsUser': uid, 'runAsGroup': uid, 'fsGroup': uid, 'seccompProfile': {'type': 'RuntimeDefault'}},
            'containers': [c], 'volumes': [{'name': v, 'persistentVolumeClaim': {'claimName': v}} for v in claims]}}
    def deployment(name, template):
        return obj('Deployment', name, spec={'replicas': 1, 'strategy': {'type': 'Recreate'}, 'selector': {'matchLabels': labels(name)}, 'template': template})
    def service(name, port):
        return obj('Service', name, spec={'selector': labels(name), 'ports': [{'port': port, 'targetPort': port}]})
    foundation = [dict(apiVersion='v1', kind='Namespace', metadata={'name': ns, 'labels': {'pod-security.kubernetes.io/enforce': 'restricted'}}),
        *[pvc(n) for n in ['database', 'identity-data', 'trust']],
        obj('Secret', 'database', type='Opaque', stringData={'POSTGRES_PASSWORD': credentials['database']}),
        obj('Secret', 'provision', type='Opaque', stringData={'THINKPIXELAG_DATABASE_URL': url, 'OPS_DATABASE_URL': url}),
        obj('Secret', 'identity', type='Opaque', stringData={'OPS_SINK_TOKEN': credentials['evidence']}),
        obj('Secret', 'api', type='Opaque', stringData={'THINKPIXELAG_DATABASE_URL': url, 'THINKPIXELAG_CURSOR_HMAC_KEY': credentials['cursor'], 'THINKPIXELAG_EVIDENCE_BEARER_TOKEN': credentials['evidence']}),
        obj('ConfigMap', 'runtime', data={'runtime.json': (Path(__file__).parent / 'runtime.json').read_text()}),
        obj('NetworkPolicy', 'isolated-evaluation', spec={'podSelector': {}, 'policyTypes': ['Ingress', 'Egress'],
            'ingress': [{'from': [{'podSelector': {}}]}],
            'egress': [{'to': [{'podSelector': {}}]}, {'to': [{'namespaceSelector': {'matchLabels': {'kubernetes.io/metadata.name': 'kube-system'}}}], 'ports': [{'protocol': 'UDP', 'port': 53}, {'protocol': 'TCP', 'port': 53}]}]})]
    pg = container('postgres', PG_IMAGE, {'POSTGRES_USER': 'thinkpixelag', 'POSTGRES_DB': 'thinkpixelag', 'PGDATA': '/var/lib/postgresql/data/pgdata'}, {'database': '/var/lib/postgresql/data'})
    pg['envFrom'] = [{'secretRef': {'name': 'database'}}]
    pg['readinessProbe'] = {'exec': {'command': ['pg_isready', '-U', 'thinkpixelag', '-d', 'thinkpixelag']}, 'periodSeconds': 2}
    pg['volumeMounts'].append({'name': 'tmp', 'mountPath': '/var/run/postgresql'})
    pgpod = pod('postgres', pg, ['database'], uid=70)
    pgpod['spec']['volumes'].append({'name': 'tmp', 'emptyDir': {}})
    foundation += [deployment('postgres', pgpod), service('postgres', 5432)]
    bootstrap = container('provision', args.support_image, {'OPS_ISSUER': 'https://identity:8443', 'OPS_AUDIENCE': 'thinkpixelag-demo', 'OPS_GATEWAY_COUNT': '1', 'OPS_RETAIN_SIGNING_KEY': '1'}, {'identity-data': '/state', 'trust': '/trust'}, ['/bin/sh', '/provision.sh'])
    bootstrap['envFrom'] = [{'secretRef': {'name': 'provision'}}]
    bootstrap_pod = pod('provision', bootstrap, ['identity-data', 'trust'])
    bootstrap_pod['spec']['restartPolicy'] = 'Never'
    job_name = 'provision-' + args.support_image.split(':')[-1][:12]
    provision = [obj('Job', job_name, spec={'backoffLimit': 3, 'template': bootstrap_pod})]
    identity = container('identity', args.support_image, {'OPS_RECEIPTS_FILE': '/state/receipts.jsonl'}, {'identity-data': '/state'}, ['/bootstrap', 'serve', '/state'])
    identity['envFrom'] = [{'secretRef': {'name': 'identity'}}]
    identity['readinessProbe'] = {'exec': {'command': ['/bootstrap', 'health', '/state']}, 'periodSeconds': 2}
    opa = container('opa', OPA_IMAGE, mounts={'trust': '/trust'}, command=['/opa', 'run', '--server', '--addr=0.0.0.0:8181', '--log-level=error', '/trust/authorization.rego'])
    opa['readinessProbe'] = {'httpGet': {'path': '/health', 'port': 8181}, 'periodSeconds': 2}
    dependencies = [deployment('identity', pod('identity', identity, ['identity-data'])), service('identity', 8443), deployment('opa', pod('opa', opa, ['trust'])), service('opa', 8181)]
    api = container('api', AG_IMAGE, {'THINKPIXELAG_ENVIRONMENT': 'local', 'THINKPIXELAG_HTTP_ADDRESS': ':8080', 'THINKPIXELAG_OPA_URL': 'http://opa:8181', 'THINKPIXELAG_OIDC_ISSUER_URL': 'https://identity:8443', 'THINKPIXELAG_OIDC_AUDIENCE': 'thinkpixelag-demo', 'THINKPIXELAG_OIDC_ROLE_MAPPINGS': 'invoker=agent-invoker,revoker=revocation-admin', 'THINKPIXELAG_RUNTIME_FILE': '/config/runtime.json', 'THINKPIXELAG_EVIDENCE_SINK_ID': 'demo-receiver', 'THINKPIXELAG_EVIDENCE_ENDPOINT': 'https://identity:8443/evidence', 'SSL_CERT_FILE': '/trust/ca.crt'}, {'trust': '/trust'})
    api['envFrom'] = [{'secretRef': {'name': 'api'}}]
    api['volumeMounts'].append({'name': 'runtime', 'mountPath': '/config', 'readOnly': True})
    api['readinessProbe'] = {'httpGet': {'path': '/readyz', 'port': 8080}, 'periodSeconds': 2}
    api['livenessProbe'] = {'httpGet': {'path': '/livez', 'port': 8080}, 'periodSeconds': 10}
    api_pod = pod('api', api, ['trust'])
    api_pod['spec']['volumes'].append({'name': 'runtime', 'configMap': {'name': 'runtime'}})
    stages = {'foundation': foundation, 'provision': provision, 'dependencies': dependencies, 'application': [deployment('api', api_pod), service('api', 8080)]}
    for name, resources in stages.items():
        path = args.output / (name + '.json')
        path.write_text(json.dumps({'apiVersion': 'v1', 'kind': 'List', 'items': resources}, indent=2) + '\n')
        path.chmod(0o600)
    print(f'Private manifests written to {args.output}; existing credentials retained. Namespace: {ns}; Job: {job_name}')

if __name__ == '__main__':
    main()
