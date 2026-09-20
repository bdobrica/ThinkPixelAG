import tempfile
import unittest
from pathlib import Path
from team import render

class TeamRenderTests(unittest.TestCase):
    def test_optional_console_has_no_governance_credentials_or_keys(self):
        with tempfile.TemporaryDirectory() as d:
            ca=Path(d)/'ca.pem';ca.write_text('test public certificate')
            s={'namespace':'ag-test','ag_image':'quay.io/example/ag@sha256:'+'a'*64,'console_image':'quay.io/example/console@sha256:'+'b'*64,
               'ag_origin':'https://ag.test','console_origin':'https://console.test','issuer':'https://id.test','audience':'ag','client_id':'console',
               'database_url':'postgresql://example','cursor_key':'x'*32,'ca_file':str(ca),'bootstrap':{'issuer':'https://id.test','channel':'stable','opa':{'endpoint':'https://opa.test','token_reference':''}},
               'authority_constraints':{},'ingress_class':'test','ingress_namespace':'ingress','tls_secret':'tls','ag_egress':[],'console_egress':[]}
            without=render(s)
            self.assertFalse(any(x['metadata']['name']=='console' for x in without['application']))
            result=render(s,True)
            console=next(x for x in result['application'] if x['kind']=='Deployment' and x['metadata']['name']=='console')['spec']['template']['spec']
            self.assertEqual([v['name'] for v in console['volumes']],['trust'])
            self.assertNotIn('envFrom',console['containers'][0])
            self.assertTrue(all(e['name'].startswith('AG_CONSOLE_') for e in console['containers'][0]['env']))
            job=result['bootstrap'][0]['spec']['template']['spec']
            self.assertEqual(job['initContainers'][0]['command'][0],'/thinkpixelag-migrate')
            self.assertEqual(job['containers'][0]['command'][0],'/thinkpixelag-operator')
            s['ag_image']='quay.io/example/ag:latest'
            with self.assertRaises(ValueError):render(s)

if __name__=='__main__':unittest.main()
