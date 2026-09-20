import json
import time
import unittest
import httpx
from fastapi.testclient import TestClient
from ag_console.app import COOKIE, create_app
from ag_console.security import Session, Settings, Transport
from test_console import SETTINGS, SUB, TENANT

DIGEST = 'sha256:' + 'a' * 64


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.status = 201
        def handler(r):
            self.calls.append(r)
            if r.method == 'GET':
                return httpx.Response(200, json={'id': SUB, 'revision': 1, 'digest': DIGEST, 'source': '</textarea><script>bad()</script>', 'created_at': '2026-09-20T00:00:00Z'})
            return httpx.Response(self.status, json={'id': SUB, 'revision': 2, 'digest': DIGEST, 'detail': 'PRIVATE_ERROR'})
        self.app = create_app(SETTINGS, Transport(SETTINGS, httpx.MockTransport(handler)))
        self.session = Session('private-token', SUB, TENANT, time.time() + 300)
        handle = self.app.state.console.sessions.add(self.session)
        self.client = TestClient(self.app, base_url=SETTINGS.public_origin, follow_redirects=False)
        self.client.cookies.set(COOKIE, handle)
        self.addCleanup(self.client.close)

    def post(self, path, data):
        return self.client.post(path, data={'csrf': self.session.csrf, **data}, headers={'Origin': SETTINGS.public_origin})

    def prepare(self):
        r = self.post('/prepare/save', {'source': 'package authorization\n', 'expected_revision': '0', 'draft_id': ''})
        self.assertEqual(r.status_code, 303)
        return r.headers['location']

    def test_review_does_not_write_and_apply_replays_exact_request(self):
        path = self.prepare()
        self.assertEqual(self.calls, [])
        review = self.client.get(path)
        self.assertIn('package authorization', review.text)
        self.assertEqual(self.post(path, {}).status_code, 303)
        self.assertEqual(len(self.calls), 1)
        sent = self.calls[0]
        self.assertEqual(json.loads(sent.content), {'source': 'package authorization\n', 'expected_revision': 0})
        self.assertTrue(sent.headers['idempotency-key'].startswith('console-'))
        self.assertEqual(self.post(path, {}).status_code, 303)
        self.assertEqual(len(self.calls), 1)

    def test_conflict_denial_and_retry_keep_key_and_body(self):
        path = self.prepare()
        self.status = 409
        r = self.post(path, {})
        self.assertEqual(r.status_code, 409)
        self.assertNotIn('PRIVATE_ERROR', r.text)
        first = self.calls[-1]
        self.status = 403
        self.assertEqual(self.post(path, {}).status_code, 403)
        self.assertEqual(self.calls[-1].content, first.content)
        self.assertEqual(self.calls[-1].headers['idempotency-key'], first.headers['idempotency-key'])

    def test_review_bound_to_session_and_expiry(self):
        path = self.prepare()
        self.session.intents[path.rsplit('/', 1)[1]]['expires'] = time.time() - 1
        self.assertEqual(self.post(path, {}).status_code, 404)
        path = self.prepare()
        other = Session('other-token', TENANT, TENANT, time.time()+300)
        self.client.cookies.set(COOKIE, self.app.state.console.sessions.add(other))
        self.assertEqual(self.client.get(path).status_code, 404)

    def test_source_rendering_and_revision_preserved(self):
        r = self.client.get('/drafts/' + SUB)
        self.assertEqual(r.status_code, 200)
        self.assertIn('&lt;/textarea&gt;&lt;script&gt;', r.text)
        self.assertNotIn('<script>bad()', r.text)
        r = self.post('/prepare/save', {'source': 'new source', 'expected_revision': '1', 'draft_id': SUB})
        self.assertEqual(r.status_code, 303)
        path = r.headers['location']
        self.assertEqual(self.post(path, {'source': 'tampered'}).status_code, 400)
        self.assertEqual(self.post(path, {}).status_code, 303)
        self.assertEqual(json.loads(self.calls[-1].content)['expected_revision'], 1)

    def test_validation_and_approval_failure_remain_separate(self):
        for action, values in [('validate', {'draft_id': SUB, 'revision': '1'}), ('decide', {'approval_id': SUB, 'approved': 'true'})]:
            r = self.post('/prepare/' + action, values)
            self.assertEqual(r.status_code, 303)
            self.status = 400 if action == 'validate' else 403
            self.assertEqual(self.post(r.headers['location'], {}).status_code, self.status)
        self.assertFalse(any('/activations' in str(r.url) for r in self.calls))


if __name__ == '__main__':
    unittest.main()
