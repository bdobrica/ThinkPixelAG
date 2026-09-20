#!/usr/bin/env python3
"""Render private staged Kubernetes manifests for the local-signing team PoC."""
import argparse
import json
import os
from pathlib import Path
import re
from urllib.parse import urlsplit

REPO=Path(__file__).resolve().parents[2]


def render(s, console=False):
    ns=s['namespace']
    if not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,61}[a-z0-9]',ns):raise ValueError('invalid namespace')
    for key in ['ag_image']+(['console_image'] if console else []):
        if not re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}',s[key]):raise ValueError('immutable image required')
    for key in ['ag_origin','issuer']+(['console_origin'] if console else []):
        u=urlsplit(s[key])
        if u.scheme!='https' or not u.hostname or u.username or u.password or u.query or u.fragment or key!='issuer' and u.path:raise ValueError('HTTPS origins required')
    bootstrap=s['bootstrap']
    if bootstrap['issuer']!=s['issuer']:raise ValueError('bootstrap issuer mismatch')
    opa=bootstrap['opa']['endpoint']
    runtime={'policy_channel':bootstrap['channel'],'authority_constraints':s['authority_constraints'],'local_policy_key':'/keys/private/policy.key',
             'role_mappings_mode':'api','integrations_mode':'api','opa_allowed_origins':[opa],'opa_secret_files':{}}
    if bootstrap['opa']['token_reference']:raise ValueError('This minimal template uses private tokenless OPA; mount protected aliases explicitly for other setups')
    def obj(kind,name,**kw):
        version={'Deployment':'apps/v1','Job':'batch/v1','Ingress':'networking.k8s.io/v1','NetworkPolicy':'networking.k8s.io/v1'}.get(kind,'v1')
        return {'apiVersion':version,'kind':kind,'metadata':{'name':name,'namespace':ns},**kw}
    def pod(name,image,env,volumes,mounts,command=None):
        c={'name':name,'image':image,'env':[{'name':k,'value':v} for k,v in env.items()], 'volumeMounts':mounts,
           'securityContext':{'allowPrivilegeEscalation':False,'readOnlyRootFilesystem':True,'capabilities':{'drop':['ALL']}},
           'resources':{'requests':{'cpu':'100m','memory':'128Mi'},'limits':{'cpu':'1','memory':'512Mi'}}}
        if command:c['command']=command
        return {'metadata':{'labels':{'app':name}},'spec':{'automountServiceAccountToken':False,'securityContext':{'runAsNonRoot':True,'runAsUser':65532,'runAsGroup':65532,'fsGroup':65532,'seccompProfile':{'type':'RuntimeDefault'}},'containers':[c],'volumes':volumes}}
    keyvol={'name':'keys','persistentVolumeClaim':{'claimName':'signing-key'}}
    configvol={'name':'config','configMap':{'name':'configuration'}}
    trustvol={'name':'trust','configMap':{'name':'trust'}}
    env={'THINKPIXELAG_ENVIRONMENT':'local','THINKPIXELAG_HTTP_ADDRESS':':8080','THINKPIXELAG_OPA_URL':opa,'THINKPIXELAG_OIDC_ISSUER_URL':s['issuer'],
         'THINKPIXELAG_OIDC_AUDIENCE':s['audience'],'THINKPIXELAG_RUNTIME_FILE':'/config/runtime.json','SSL_CERT_FILE':'/trust/ca.crt'}
    secrets={'THINKPIXELAG_DATABASE_URL':s['database_url'],'THINKPIXELAG_CURSOR_HMAC_KEY':s['cursor_key']}
    if s.get('evidence_endpoint'):
        env.update(THINKPIXELAG_EVIDENCE_ENDPOINT=s['evidence_endpoint'],THINKPIXELAG_EVIDENCE_SINK_ID=s['evidence_sink_id'])
        secrets['THINKPIXELAG_EVIDENCE_BEARER_TOKEN']=s['evidence_token']
    foundation=[{'apiVersion':'v1','kind':'Namespace','metadata':{'name':ns,'labels':{'pod-security.kubernetes.io/enforce':'restricted'}}},
       obj('Secret','ag-runtime',type='Opaque',stringData=secrets),
       obj('ConfigMap','trust',data={'ca.crt':Path(s['ca_file']).read_text()}),
       obj('ConfigMap','configuration',data={'runtime.json':json.dumps(runtime),'bootstrap.json':json.dumps(bootstrap),'authorization.rego':(REPO/'policies/authorization.rego').read_text()}),
       obj('PersistentVolumeClaim','signing-key',spec={'accessModes':['ReadWriteOnce'],'resources':{'requests':{'storage':'1Gi'}},**({'storageClassName':s['storage_class']} if s.get('storage_class') else {})})]
    template=pod('bootstrap',s['ag_image'],env,[keyvol,configvol,trustvol],[{'name':'keys','mountPath':'/keys'},{'name':'config','mountPath':'/config','readOnly':True},{'name':'trust','mountPath':'/trust','readOnly':True}],
        ['/thinkpixelag-operator','bootstrap','--spec','/config/bootstrap.json','--policy','/config/authorization.rego','--key','/keys/private/policy.key','--opa-origin',opa])
    template['spec']['containers'][0]['envFrom']=[{'secretRef':{'name':'ag-runtime'}}]
    migrate=json.loads(json.dumps(template['spec']['containers'][0]));migrate['name']='migrate';migrate['command']=['/thinkpixelag-migrate','--directory','/migrations']
    template['spec']['initContainers']=[migrate];template['spec']['restartPolicy']='Never'
    job=obj('Job','bootstrap-'+s['ag_image'].rsplit(':',1)[1][:12],spec={'backoffLimit':1,'template':template})
    def deploy(name,template):return obj('Deployment',name,spec={'replicas':1,'strategy':{'type':'Recreate'},'selector':{'matchLabels':{'app':name}},'template':template})
    def service(name,port):return obj('Service',name,spec={'selector':{'app':name},'ports':[{'port':port,'targetPort':port}]})
    api=pod('api',s['ag_image'],env,[keyvol,configvol,trustvol],[{'name':'keys','mountPath':'/keys','readOnly':True},{'name':'config','mountPath':'/config','readOnly':True},{'name':'trust','mountPath':'/trust','readOnly':True}])
    api['spec']['containers'][0].update(envFrom=[{'secretRef':{'name':'ag-runtime'}}],readinessProbe={'httpGet':{'path':'/readyz','port':8080},'periodSeconds':5},livenessProbe={'httpGet':{'path':'/livez','port':8080},'periodSeconds':10})
    application=[deploy('api',api),service('api',8080)]
    names=[('api','ag_origin',8080)]
    if console:
        cfg={'AG_CONSOLE_PUBLIC_ORIGIN':s['console_origin'],'AG_CONSOLE_AG_ORIGIN':s['ag_origin'],'AG_CONSOLE_ISSUER':s['issuer'],'AG_CONSOLE_CLIENT_ID':s['client_id'],'AG_CONSOLE_AUDIENCE':s['audience'],'AG_CONSOLE_CA_FILE':'/trust/ca.crt'}
        view=pod('console',s['console_image'],cfg,[trustvol],[{'name':'trust','mountPath':'/trust','readOnly':True}])
        probe={'httpGet':{'path':'/healthz','port':8081,'httpHeaders':[{'name':'Host','value':urlsplit(s['console_origin']).netloc}]},'periodSeconds':10}
        view['spec']['containers'][0].update(readinessProbe=probe,livenessProbe=probe)
        application.extend([deploy('console',view),service('console',8081)]);names.append(('console','console_origin',8081))
    for name,origin_key,port in names:
        host=urlsplit(s[origin_key]).hostname
        application.append(obj('Ingress',name,spec={'ingressClassName':s['ingress_class'],'tls':[{'hosts':[host],'secretName':s['tls_secret']}],
            'rules':[{'host':host,'http':{'paths':[{'path':'/','pathType':'Prefix','backend':{'service':{'name':name,'port':{'number':port}}}}]}}]}))
    dns={'to':[{'namespaceSelector':{'matchLabels':{'kubernetes.io/metadata.name':'kube-system'}}}],'ports':[{'protocol':'UDP','port':53},{'protocol':'TCP','port':53}]}
    for name in ['api','bootstrap']+(['console'] if console else []):
        egress=s['console_egress'] if name=='console' else s['ag_egress']
        rules=[dns]+[{'to':[{'ipBlock':{'cidr':x['cidr']}}],'ports':[{'protocol':'TCP','port':p} for p in x['ports']]} for x in egress]
        foundation.append(obj('NetworkPolicy',name,spec={'podSelector':{'matchLabels':{'app':name}},'policyTypes':['Ingress','Egress'],
          'ingress':[] if name=='bootstrap' else [{'from':[{'namespaceSelector':{'matchLabels':{'kubernetes.io/metadata.name':s['ingress_namespace']}}}],'ports':[{'protocol':'TCP','port':8081 if name=='console' else 8080}]}], 'egress':rules}))
    return {'foundation':foundation,'bootstrap':[job],'application':application}


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--settings',type=Path,required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--console',action='store_true');a=p.parse_args()
    os.umask(0o077);a.output.mkdir(mode=0o700,parents=True,exist_ok=True)
    if a.output.stat().st_mode & 0o077:raise ValueError('private output directory required')
    for name,items in render(json.loads(a.settings.read_text()),a.console).items():
        path=a.output/(name+'.json');path.write_text(json.dumps({'apiVersion':'v1','kind':'List','items':items},indent=2)+'\n');path.chmod(0o600)
    print('Private manifests generated. Review and apply foundation, bootstrap, application in that order.')


if __name__=='__main__':main()
