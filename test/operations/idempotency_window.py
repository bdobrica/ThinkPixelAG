#!/usr/bin/env python3
"""Opt-in failure-window qualification against an isolated operations fixture.

Requires SSH/kubectl administrative access. Never run alongside other traffic
for the fixture tenant. The key-specific fault trigger is retained disabled.
Only aggregate results are printed; keys, tokens and response bodies are not.
"""
import argparse
import http.client
import json
from pathlib import Path
import shlex
import subprocess
import sys
import time
from urllib.parse import urlsplit
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True)
    parser.add_argument('--identities', type=Path, required=True)
    parser.add_argument('--ssh-host', required=True)
    parser.add_argument('--namespace', required=True)
    parser.add_argument('--database-deployment', default='postgres')
    parser.add_argument('--database-user', default='thinkpixelag')
    parser.add_argument('--database-name', default='thinkpixelag')
    parser.add_argument('--allow-fault-injection', action='store_true', required=True)
    args = parser.parse_args()
    base = urlsplit(args.base)
    if base.scheme not in ('http', 'https') or not base.hostname or base.username or base.password or base.query or base.fragment or base.path not in ('', '/'):
        parser.error('base must be an HTTP(S) origin without credentials')
    identity = json.loads((args.identities / 'identity.json').read_text())
    tenant = str(uuid.UUID(identity['Tenant']))
    agent = str(uuid.UUID(identity['Agent']))
    token = (args.identities / 'caller.token').read_text().strip()
    key = 'ops-window-' + str(uuid.uuid4())
    body = json.dumps({'objective': 'synthetic failure-window test', 'constraints': {'max_execution_time_seconds': 3600, 'max_llm_tokens': 100}})

    def sql(statement):
        command = ['sudo', 'kubectl', '-n', args.namespace, 'exec', '-i', 'deployment/' + args.database_deployment, '--', 'psql', '-U', args.database_user, '-d', args.database_name, '-At', '-v', 'ON_ERROR_STOP=1']
        result = subprocess.run(['ssh', args.ssh_host, shlex.join(command)], input=statement, text=True, capture_output=True, timeout=120)
        if result.returncode:
            raise RuntimeError('database fault/check command failed; details withheld')
        return result.stdout.strip()

    def count():
        return int(sql("SELECT count(*) FROM runs WHERE tenant_id='" + tenant + "';"))

    def admit():
        kind = http.client.HTTPSConnection if base.scheme == 'https' else http.client.HTTPConnection
        connection = kind(base.hostname, base.port, timeout=25)
        try:
            connection.request('POST', '/v1/agents/' + agent + '/runs', body, {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json', 'Idempotency-Key': key})
            response = connection.getresponse()
            response.read()
            return response.status
        except OSError:
            return 0
        finally:
            connection.close()

    result = {'scenario': 'admission completion failure followed by lease-expiry retry', 'runs_before': count()}
    # UUID-generated key and parsed UUID tenant are the only SQL interpolations.
    sql("""CREATE OR REPLACE FUNCTION ops010_delay_idempotency() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
IF NEW.idempotency_key='""" + key + """' AND NEW.state='COMPLETED' THEN
  PERFORM pg_sleep(20);
END IF;
RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER ops010_delay_idempotency BEFORE UPDATE ON idempotency_records
FOR EACH ROW EXECUTE FUNCTION ops010_delay_idempotency();
ALTER TABLE idempotency_records ENABLE TRIGGER ops010_delay_idempotency;
""")
    try:
        started = time.monotonic()
        result['first_status'] = admit()
        result['first_seconds'] = round(time.monotonic() - started, 3)
        result['runs_after_failure'] = count()
    finally:
        sql('ALTER TABLE idempotency_records DISABLE TRIGGER ops010_delay_idempotency;')
    # The documented governed runtime uses a one-minute acquisition lease.
    time.sleep(65)
    result['retry_status'] = admit()
    result['runs_after_retry'] = count()
    result['new_runs_for_one_key'] = result['runs_after_retry'] - result['runs_before']
    injected_failure = result['first_status'] == 0 or result['first_status'] >= 500
    result['passed'] = injected_failure and result['retry_status'] == 201 and result['new_runs_for_one_key'] == 1
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        print('Failure-window test could not complete (' + type(error).__name__ + '); verify the test trigger is disabled.', file=sys.stderr)
        sys.exit(2)
