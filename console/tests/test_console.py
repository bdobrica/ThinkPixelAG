import base64
import hashlib
import json
import time
import unittest
from urllib.parse import parse_qs, urlsplit

from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient
import httpx
import jwt

from ag_console.app import COOKIE, LOGIN_COOKIE, create_app
from ag_console.security import Failure, Session, Settings, Transport

SUB = '01990000-0000-7000-8000-000000000001'
TENANT = '01990000-0000-7000-8000-000000000002'
SETTINGS = Settings('https://console.test', 'https://ag.test', 'https://id.test', 'console', 'ag')


class Provider:
    def __init__(self):
        self.key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        self.jwk = json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(self.key.public_key()))
        self.jwk.update(kid='test', alg='RS256', use='sig')
        self.nonce = ''
        self.bad = None
        self.calls = []
        self.token = ''
        self.status = 200

    def handle(self, request):
        self.calls.append(request)
        path = request.url.path
        if request.url.host == 'ag.test':
            if self.status != 200:
                return httpx.Response(self.status, json={'detail': 'SECRET_REMOTE_BODY'})
            return httpx.Response(200, json={'items': [{'id': SUB, 'state': 'ACTIVE', 'name': '<script>bad()</script>'}], 'next_cursor': ''})
        if path.endswith('openid-configuration'):
            return httpx.Response(200, json={'issuer': SETTINGS.issuer, 'authorization_endpoint': SETTINGS.issuer + '/authorize',
                'token_endpoint': SETTINGS.issuer + '/token', 'jwks_uri': SETTINGS.issuer + '/jwks', 'code_challenge_methods_supported': ['S256']})
        if path == '/jwks':
            return httpx.Response(200, json={'keys': [self.jwk]})
        if path == '/token':
            form = parse_qs(request.content.decode())
            assert form['code_verifier'][0]
            now = int(time.time())
            identity = {'iss': SETTINGS.issuer, 'sub': SUB, 'aud': SETTINGS.client_id, 'iat': now, 'exp': now + 900, 'nonce': self.nonce}
            access = {**identity, 'aud': SETTINGS.audience, 'tenant_id': TENANT}
            if self.bad == 'nonce': identity['nonce'] = 'wrong'
            if self.bad == 'audience': access['aud'] = 'different'
            if self.bad == 'expired': access['exp'] = now - 1
            if self.bad == 'subject': access['sub'] = TENANT
            if self.bad == 'tenant': access['tenant_id'] = 'not-a-tenant'
            key = rsa.generate_private_key(public_exponent=65537, key_size=2048) if self.bad == 'signature' else self.key
            self.token = jwt.encode(access, key, algorithm='RS256', headers={'kid': 'test'})
            return httpx.Response(200, json={'token_type': 'Bearer', 'id_token': jwt.encode(identity, key, algorithm='RS256', headers={'kid': 'test'}), 'access_token': self.token})
        raise AssertionError(path)


class ConsoleTests(unittest.TestCase):
    def setUp(self):
        self.provider = Provider()
        self.app = create_app(SETTINGS, Transport(SETTINGS, httpx.MockTransport(self.provider.handle)))
        self.client = TestClient(self.app, base_url=SETTINGS.public_origin, follow_redirects=False)
        self.addCleanup(self.client.close)

    def begin(self):
        r = self.client.get('/login')
        self.assertEqual(r.status_code, 303)
        query = parse_qs(urlsplit(r.headers['location']).query)
        self.provider.nonce = query['nonce'][0]
        pending = self.app.state.console.sessions.logins[self.client.cookies.get(LOGIN_COOKIE)]
        challenge = base64.urlsafe_b64encode(hashlib.sha256(pending['verifier'].encode()).digest()).rstrip(b'=').decode()
        self.assertEqual(query['code_challenge'], [challenge])
        self.assertIn('Secure', r.headers['set-cookie'])
        return query['state'][0]

    def login(self):
        return self.client.get('/callback', params={'state': self.begin(), 'code': 'test-code'})

    def test_login_forwards_exact_caller_and_escapes_output(self):
        result = self.login()
        self.assertEqual(result.status_code, 303)
        self.assertNotIn(self.provider.token, result.text + str(result.headers))
        r = self.client.get('/agents')
        self.assertEqual(r.status_code, 200)
        self.assertIn('&lt;script&gt;', r.text)
        self.assertNotIn('<script>', r.text)
        self.assertEqual(self.provider.calls[-1].headers['authorization'], 'Bearer ' + self.provider.token)
        self.assertNotIn('x-tenant-id', self.provider.calls[-1].headers)
        self.assertEqual(r.headers['cache-control'], 'no-store')
        self.assertEqual(r.headers['referrer-policy'], 'strict-origin')
        self.assertIn("frame-ancestors 'none'", r.headers['content-security-policy'])

    def test_callback_state_cookie_and_replay(self):
        state = self.begin()
        self.assertEqual(self.client.get('/callback', params={'state': 'wrong', 'code': 'code'}).status_code, 401)
        self.assertEqual(self.client.get('/callback', params={'state': state, 'code': 'code'}).status_code, 401)
        state = self.begin()
        self.client.cookies.delete(LOGIN_COOKIE)
        self.assertEqual(self.client.get('/callback', params={'state': state, 'code': 'code'}).status_code, 401)

    def test_identity_failures(self):
        for bad in ('nonce', 'audience', 'expired', 'subject', 'tenant', 'signature'):
            with self.subTest(bad=bad):
                self.provider.bad = bad
                self.assertIn(self.login().status_code, (400, 401))
                self.assertNotIn(COOKIE, self.client.cookies)

    def test_csrf_origin_logout_and_expiry(self):
        self.login()
        s = self.app.state.console.sessions.active[self.client.cookies.get(COOKIE)]
        self.assertEqual(self.client.post('/logout', data={'csrf': s.csrf}).status_code, 403)
        self.assertEqual(self.client.post('/logout', headers={'Origin': SETTINGS.public_origin}, data={'csrf': 'wrong'}).status_code, 403)
        self.assertEqual(self.client.post('/logout', headers={'Origin': SETTINGS.public_origin}, data={'csrf': s.csrf}).status_code, 303)
        self.assertEqual(self.client.get('/agents').status_code, 401)
        self.login()
        self.app.state.console.sessions.active[self.client.cookies.get(COOKIE)].expires = time.time() - 1
        self.assertEqual(self.client.get('/agents').status_code, 401)

    def test_denial_redacted_and_restart_loses_session(self):
        self.login()
        self.provider.status = 403
        r = self.client.get('/runs')
        self.assertEqual(r.status_code, 403)
        self.assertNotIn('SECRET_REMOTE_BODY', r.text)
        self.app.state.console.sessions.active.clear()
        self.assertEqual(self.client.get('/runs').status_code, 401)

    def test_host_body_and_route_bounds(self):
        self.assertEqual(self.client.get('/', headers={'Host': 'evil.test'}).status_code, 400)
        self.assertEqual(self.client.post('/logout', headers={'Origin': SETTINGS.public_origin}, content=b'x' * (1048576 + 1)).status_code, 413)
        self.assertEqual(self.client.get('/login?return_to=https://evil.test').status_code, 400)
        self.login()
        self.assertEqual(self.client.get('/agents/not-an-id').status_code, 400)

    def test_redirects_and_oversized_upstreams_rejected(self):
        import asyncio
        for status, content in ((302, b''), (200, b' ' * (2 * 1048576 + 1))):
            calls = []
            def respond(request):
                calls.append(request)
                return httpx.Response(status, content=content, headers={'Content-Type': 'application/json', 'Location': 'https://evil.test'})
            t = Transport(SETTINGS, httpx.MockTransport(respond))
            with self.assertRaises(Failure):
                asyncio.run(t.json('https://ag.test/v1/agents', token='private'))
            self.assertEqual(len(calls), 1)

    def test_origin_validation(self):
        for bad in ('http://ag.test', 'https://user:pass@ag.test', 'https://ag.test/path', 'https://ag.test?x=1'):
            with self.assertRaises(ValueError):
                Settings(SETTINGS.public_origin, bad, SETTINGS.issuer, 'console', 'ag')


if __name__ == '__main__':
    unittest.main()
