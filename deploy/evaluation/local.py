#!/usr/bin/env python3
"""Retained Linux local evaluation using the real AG operator bootstrap."""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import time
import ipaddress
import ssl
from urllib.parse import quote

import httpx
import jwt
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

REPO = Path(__file__).resolve().parents[2]
PG = 'postgres:18.4-alpine3.23@sha256:996d0920e4ff9df1fc19dacb904492f3c1ec0ec1cc338f0ad7123be7731c5f5e'
OPA = 'openpolicyagent/opa:1.19.0-debug@sha256:ec3c7a29a21ce96d71231cb4befa2561205fe84e5a2dc3cc46ac7bc8bd21b3a4'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['up', 'restart', 'console-stop', 'token', 'backup', 'status'])
    parser.add_argument('--state', type=Path, default=Path.home()/'.local/state/thinkpixelag-evaluation')
    parser.add_argument('--console', action='store_true')
    parser.add_argument('--port', type=int, default=19455, help='Five consecutive loopback ports, persisted at first setup')
    parser.add_argument('--bin-dir', type=Path, help='Use packaged binaries instead of building this checkout')
    parser.add_argument('--identity', choices=['caller','operator-one','operator-two'], default='caller')
    args = parser.parse_args()
    os.umask(0o077)
    root = args.state.resolve();root.mkdir(mode=0o700, parents=True, exist_ok=True)
    if root.is_symlink() or root.stat().st_uid != os.getuid() or root.stat().st_mode & 0o077:
        parser.error('state must be an owned 0700 directory on a Unix-permission filesystem')
    def save(name, value):
        (root/name).write_text(json.dumps(value, indent=2)+'\n')
    def run(cmd, env=None):
        p = subprocess.run(cmd, env=env, cwd=REPO, capture_output=True, text=True)
        if p.returncode:
            (root/'last-command.log').write_text(p.stdout+p.stderr)
            raise RuntimeError('Command failed; inspect the private last-command.log')
        return p.stdout
    clean = {k:v for k,v in os.environ.items() if not k.startswith(('THINKPIXELAG_','AG_CONSOLE_'))}
    config = root/'installation.json'
    if config.exists():
        settings=json.loads(config.read_text())
    else:
        if args.command!='up' or not 1024 <= args.port <= 65531:
            parser.error('run up first with five available ports')
        settings={'port':args.port,'origin':f'https://127.0.0.1:{args.port}', 'console_origin':f'https://127.0.0.1:{args.port+1}',
                  'api_internal':f'http://127.0.0.1:{args.port+2}','database_password':secrets.token_hex(32),'cursor_key':secrets.token_hex(32)}
        save('installation.json', settings)
    port=settings['port'];prefix='thinkpixelag-eval-'+hashlib.sha256(str(root).encode()).hexdigest()[:10]
    binaries=(args.bin_dir.resolve() if args.bin_dir else root/'bin')
    def stop(name):
        pidfile=root/(name+'.pid')
        if not pidfile.exists():return
        pid=int(pidfile.read_text())
        try:
            command=Path(f'/proc/{pid}/cmdline').read_bytes()
        except FileNotFoundError:return
        if not command:return
        if name=='api' and str(binaries/'thinkpixelag').encode() not in command or name!='api' and b'uvicorn' not in command:
            raise RuntimeError('Recorded PID does not match the evaluation; refusing to signal it')
        os.kill(pid,signal.SIGTERM)
        for _ in range(100):
            proc=Path(f'/proc/{pid}/cmdline')
            if not proc.exists() or not proc.read_bytes():return
            time.sleep(.1)
        raise RuntimeError('Process did not stop; inspect private logs before restarting')
    def mint():
        spec=json.loads((root/'bootstrap.json').read_text());index={'operator-one':0,'operator-two':1,'caller':2}[args.identity]
        now=int(time.time());claims={'iss':settings['origin'],'aud':'thinkpixelag-evaluation','sub':spec['principals'][index],
           'tenant_id':spec['tenant_id'],'roles':['users','operators','registrars'] if index<2 else ['users'],'iat':now,'exp':now+900}
        token=jwt.encode(claims,(root/'issuer.key').read_bytes(),algorithm='RS256',headers={'kid':'console-local'})
        target=root/(args.identity+'.token');target.write_text(token);target.chmod(0o600)
        print(f'Caller assertion written privately to {target}')
    if args.command=='token':mint();return
    if args.command=='console-stop':stop('console');print('Console stopped; AG and durable dependencies retained.');return
    if args.command=='backup':
        target=root/('database-'+str(int(time.time()))+'.dump')
        with target.open('xb') as f:subprocess.run(['docker','exec',prefix+'-postgres','pg_dump','-U','thinkpixelag','-Fc','thinkpixelag'],stdout=f,check=True)
        print(f'Logical database backup: {target}; also protect keys and configuration in {root}');return
    if args.command=='status':
        with httpx.Client(verify=ssl.create_default_context(cafile=str(root/'tls.crt')),trust_env=False) as c:
            print('AG readiness HTTP',c.get(settings['api_internal']+'/readyz').status_code)
        print('AG:',settings['origin'],'Console:',settings['console_origin']);return
    if args.command=='restart':
        for name in ['console','api','identity']:stop(name)
    binaries.mkdir(mode=0o700,exist_ok=True)
    if not args.bin_dir:
        for name in ['thinkpixelag','thinkpixelag-operator','thinkpixelag-migrate']:
            run(['go','build','-trimpath','-o',str(binaries/name),'./cmd/'+name],env=clean)
    if not (root/'tls.key').exists():
        tls=rsa.generate_private_key(public_exponent=65537,key_size=2048);now=dt.datetime.now(dt.timezone.utc)
        name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'ThinkPixelAG local evaluation')])
        cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(tls.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-dt.timedelta(minutes=1)).not_valid_after(now+dt.timedelta(days=14)).add_extension(x509.BasicConstraints(ca=True,path_length=None),True).add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address('127.0.0.1'))]),False).sign(tls,hashes.SHA256())
        (root/'tls.crt').write_bytes(cert.public_bytes(serialization.Encoding.PEM))
        for filename,key in [('tls.key',tls),('issuer.key',rsa.generate_private_key(public_exponent=65537,key_size=2048))]:
            (root/filename).write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.TraditionalOpenSSL,serialization.NoEncryption()))
    for name,image,options in [
        ('postgres',PG,['-e','POSTGRES_USER=thinkpixelag','-e','POSTGRES_DB=thinkpixelag','-e','POSTGRES_PASSWORD','-v',prefix+'-database:/var/lib/postgresql','-p',f'127.0.0.1:{port+3}:5432']),
        ('opa',OPA,['-p',f'127.0.0.1:{port+4}:8181'])]:
        existing=subprocess.run(['docker','inspect',prefix+'-'+name],capture_output=True,text=True)
        if existing.returncode:
            command=['docker','create','--name',prefix+'-'+name,'--label','thinkpixelag.evaluation='+str(root),*options,image]
            if name=='opa':command+=['run','--server','--addr=0.0.0.0:8181','--log-level=error']
            run(command,env={**clean,'POSTGRES_PASSWORD':settings['database_password']})
        else:
            metadata=json.loads(existing.stdout)[0]
            if metadata['Config']['Labels'].get('thinkpixelag.evaluation')!=str(root):raise RuntimeError('Container ownership mismatch')
        run(['docker','start',prefix+'-'+name])
    for _ in range(60):
        if subprocess.run(['docker','exec',prefix+'-postgres','pg_isready','-U','thinkpixelag','-d','thinkpixelag'],capture_output=True).returncode==0:break
        time.sleep(1)
    env={**clean,'THINKPIXELAG_ENVIRONMENT':'local','THINKPIXELAG_DATABASE_URL':f'postgresql://thinkpixelag:{quote(settings["database_password"],safe="")}@127.0.0.1:{port+3}/thinkpixelag?sslmode=disable'}
    opa=f'http://127.0.0.1:{port+4}'
    with httpx.Client(trust_env=False,timeout=2) as client:
        for _ in range(60):
            try:
                if client.get(opa+'/health').status_code==200:break
            except httpx.HTTPError:pass
            time.sleep(1)
        else:raise RuntimeError('OPA did not become ready')
    if not (root/'bootstrap.json').exists():
        ids=[run([str(binaries/'thinkpixelag-operator'),'new-id'],env).strip() for _ in range(5)]
        save('bootstrap.json',{'tenant_id':ids[0],'slug':'local-evaluation','issuer':settings['origin'],'principals':ids[1:4],
             'role_mappings':{'users':'agent-invoker','operators':'policy-admin','registrars':'registry-admin'},'opa':{'endpoint':opa,'token_reference':''},
             'channel':'stable','agent_id':ids[4],'agent_name':'local-evaluation','manifest':{'schema_version':1,'image':'registry.example/evaluation@sha256:'+'a'*64,
             'models':[],'tools':[],'skills':[],'subagents':[],'limits':{'max_execution_time_seconds':300,'max_llm_tokens':1000,'max_tool_calls':10}}})
    run([str(binaries/'thinkpixelag-migrate'),'--directory',str(REPO/'migrations')],env)
    receipt=run([str(binaries/'thinkpixelag-operator'),'bootstrap','--spec',str(root/'bootstrap.json'),'--policy',str(REPO/'policies/authorization.rego'),'--key',str(root/'policy.key'),'--opa-origin',opa],env)
    (root/'provisioned.json').write_text(receipt)
    save('runtime.json',{'policy_channel':'stable','authority_constraints':{'max_execution_time_seconds':300,'max_llm_tokens':1000,'max_tool_calls':10},'local_policy_key':str(root/'policy.key'),'role_mappings_mode':'api','integrations_mode':'api','opa_allowed_origins':[opa],'opa_secret_files':{}})
    env.update(THINKPIXELAG_RUNTIME_FILE=str(root/'runtime.json'),THINKPIXELAG_CURSOR_HMAC_KEY=settings['cursor_key'],THINKPIXELAG_OIDC_ISSUER_URL=settings['origin'],THINKPIXELAG_OIDC_AUDIENCE='thinkpixelag-evaluation',THINKPIXELAG_HTTP_ADDRESS=f'127.0.0.1:{port+2}',THINKPIXELAG_OPA_URL=opa,SSL_CERT_FILE=str(root/'tls.crt'))
    host={**clean,'AG_EVALUATION_STATE':str(root),'PYTHONPATH':str(REPO/'console')+':'+str(REPO/'deploy/evaluation')}
    console={**host,'AG_CONSOLE_PUBLIC_ORIGIN':settings['console_origin'],'AG_CONSOLE_AG_ORIGIN':settings['origin'],'AG_CONSOLE_ISSUER':settings['origin'],'AG_CONSOLE_CLIENT_ID':'ag-evaluation-console','AG_CONSOLE_AUDIENCE':'thinkpixelag-evaluation','AG_CONSOLE_CA_FILE':str(root/'tls.crt')}
    command=[sys.executable,'-m','uvicorn','--host','127.0.0.1','--no-access-log','--no-proxy-headers','--ssl-keyfile',str(root/'tls.key'),'--ssl-certfile',str(root/'tls.crt')]
    processes=[('identity',command+['identity:app','--port',str(port)],host),('api',[str(binaries/'thinkpixelag')],env)]
    if args.console:processes.append(('console',command+['ag_console.app:create_app','--factory','--port',str(port+1)],console))
    for name,cmd,process_env in processes:
        pidfile=root/(name+'.pid')
        if pidfile.exists():
            proc=Path('/proc/'+pidfile.read_text().strip()+'/cmdline')
            if proc.exists() and proc.read_bytes():continue
        with (root/(name+'.log')).open('ab') as log:
            process=subprocess.Popen(cmd,env=process_env,stdout=log,stderr=log,start_new_session=True);pidfile.write_text(str(process.pid))
        if name=='identity':
            with httpx.Client(verify=ssl.create_default_context(cafile=str(root/'tls.crt')),trust_env=False,timeout=2) as client:
                for _ in range(60):
                    try:
                        if client.get(settings['origin']+'/.well-known/openid-configuration').status_code==200:break
                    except httpx.HTTPError:pass
                    time.sleep(1)
                else:raise RuntimeError('Development issuer did not become ready')
    with httpx.Client(trust_env=False,timeout=2) as client:
        for _ in range(90):
            try:
                if client.get(settings['api_internal']+'/readyz').status_code==200:break
            except httpx.HTTPError:pass
            time.sleep(1)
        else:raise RuntimeError('AG not ready; inspect private process logs')
    if args.console:
        with httpx.Client(verify=ssl.create_default_context(cafile=str(root/'tls.crt')),trust_env=False,timeout=2) as client:
            for _ in range(60):
                try:
                    if client.get(settings['console_origin']+'/healthz').status_code==200:break
                except httpx.HTTPError:pass
                time.sleep(1)
            else:raise RuntimeError('Console not ready; inspect private process logs')
    save('harness.json',{'origin':settings['origin'],'token_file':str(root/'caller.token'),'state_dir':str(root/'harness-cache'),'ca_file':str(root/'tls.crt')})
    args.identity='caller';mint()
    print(f'AG ready: {settings["origin"]}; console: {settings["console_origin"] if args.console else "disabled"}')
    print(f'Private state retained at {root}. Development issuer only; no AR execution or production signing claim.')


if __name__=='__main__':
    try:main()
    except (RuntimeError,OSError,ValueError) as error:
        print('Evaluation setup failed. Inspect private state/logs; no reset or cleanup was performed.',file=sys.stderr)
        sys.exit(1)
