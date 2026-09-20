import json
import unittest
import httpx
import test_workflows as workflows


class ConfigurationTests(unittest.TestCase):
    def setUp(self):
        workflows.WorkflowTests.setUp(self)
        self.mode = 'api'
        self.revision = 1
        self.unavailable = False
        def handler(r):
            self.calls.append(r)
            if r.method != 'GET':
                return httpx.Response(self.status, json={'revision': 2, 'mode': self.mode})
            if r.url.path.endswith('/status'):
                if self.unavailable:
                    return httpx.Response(503, json={'detail': 'SECRET_OR_PATH'})
                return httpx.Response(200, json={'id': 'opa', 'revision': 1, 'state': 'ready'})
            data = {'mode': self.mode, 'revision': self.revision}
            if r.url.path.endswith('/role-mappings'):
                data.update(issuer='https://identity.test', mappings={'operators': 'policy-admin'})
            else:
                data.update(id='opa', connection={'endpoint': 'https://opa.test', 'token_reference': 'protected-alias'})
            return httpx.Response(200, json=data)
        self.app.state.console.transport.adapter = httpx.MockTransport(handler)

    post = workflows.WorkflowTests.post

    def test_file_mode_hides_and_rejects_writes(self):
        self.mode = 'file'
        for path in ('/mappings', '/integrations'):
            r = self.client.get(path)
            self.assertEqual(r.status_code, 200)
            self.assertIn('File-managed', r.text)
            self.assertNotIn('action="/prepare/', r.text)
        r = self.post('/prepare/opa', {'expected_revision': '1', 'endpoint': 'https://opa.test', 'token_reference': ''})
        self.assertEqual(r.status_code, 403)
        self.assertFalse(any(r.method != 'GET' for r in self.calls))

    def test_revision_conflict_does_not_write(self):
        self.revision = 2
        r = self.post('/prepare/mappings', {'expected_revision': '1', 'mappings': '{"operators":"policy-admin"}', 'approval_reference': 'not-required'})
        self.assertEqual(r.status_code, 409)
        self.assertFalse(any(r.method != 'GET' for r in self.calls))

    def test_closed_roles_duplicate_names_and_secret_paths_rejected(self):
        for value in ('{"external":"trusted-workload"}', '{"external":"policy-admin","external":"agent-invoker"}', '{"external":"custom-role"}'):
            self.assertEqual(self.post('/prepare/mappings', {'expected_revision': '1', 'mappings': value, 'approval_reference': 'not-required'}).status_code, 400)
        self.assertEqual(self.post('/prepare/opa', {'expected_revision': '1', 'endpoint': 'https://opa.test', 'token_reference': '/private/token'}).status_code, 400)
        self.assertEqual(self.post('/prepare/opa', {'expected_revision': '1', 'endpoint': 'https://secret@opa.test', 'token_reference': ''}).status_code, 400)

    def test_alias_only_and_unavailable_status_redacted(self):
        self.unavailable = True
        r = self.client.get('/integrations')
        self.assertIn('protected-alias', r.text)
        self.assertIn('unavailable', r.text)
        self.assertIn('Unsupported', r.text)
        self.assertNotIn('SECRET_OR_PATH', r.text)
        result = self.post('/prepare/opa', {'expected_revision': '1', 'endpoint': 'https://opa.test', 'token_reference': 'protected-alias'})
        self.assertEqual(result.status_code, 303)
        self.assertEqual(self.post(result.headers['location'], {}).status_code, 303)
        self.assertEqual(json.loads(self.calls[-1].content)['connection']['token_reference'], 'protected-alias')
        self.assertEqual(self.calls[-1].url.host, 'ag.test')

    def test_mapping_approval_is_separate_from_apply(self):
        body = {'expected_revision': '1', 'mappings': '{"operators":"policy-admin","extra":"policy-admin"}', 'approval_reference': 'not-required'}
        result = self.post('/prepare/mapping_approval', body)
        self.assertEqual(result.status_code, 303)
        self.assertEqual(self.post(result.headers['location'], {}).status_code, 303)
        self.assertTrue(self.calls[-1].url.path.endswith('/role-mappings/approvals'))
        self.assertEqual(self.calls[-1].method, 'POST')
        self.assertEqual(json.loads(self.calls[-1].content)['approval_reference'], 'not-required')


if __name__ == '__main__':
    unittest.main()
