"""Local-only acceptance issuer. No production login/credential claims."""
import base64,hashlib,json,secrets,time,os
from pathlib import Path
from urllib.parse import parse_qs,urlencode
from fastapi import FastAPI,Request
from fastapi.responses import HTMLResponse,RedirectResponse,Response
from cryptography.hazmat.primitives import serialization
import jwt,httpx
root=Path(os.environ['AG_EVALUATION_STATE'])
settings=json.loads((root/'installation.json').read_text())
spec=json.loads((root/'bootstrap.json').read_text())
key=serialization.load_pem_private_key((root/'issuer.key').read_bytes(),password=None)
jwk=json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(key.public_key()));jwk.update(kid='console-local',alg='RS256',use='sig')
issuer=settings['origin'];client='ag-evaluation-console';redirect=settings['console_origin']+'/callback'
app=FastAPI();pending={};codes={}
@app.get('/.well-known/openid-configuration')
async def discovery():return {'issuer':issuer,'authorization_endpoint':issuer+'/authorize','token_endpoint':issuer+'/token','jwks_uri':issuer+'/jwks','code_challenge_methods_supported':['S256']}
@app.get('/jwks')
async def jwks():return {'keys':[jwk]}
@app.get('/authorize')
async def authorize(r:Request):
 q=dict(r.query_params)
 if q.get('client_id')!=client or q.get('redirect_uri')!=redirect or q.get('code_challenge_method')!='S256':return Response(status_code=400)
 ticket=secrets.token_urlsafe(24);pending[ticket]=(q,time.time()+300)
 return HTMLResponse('<h1>Local development identity provider</h1><p>Local evaluation accounts only. This is not company SSO.</p>'+''.join('<form method="post" action="/choose"><input type="hidden" name="ticket" value="'+ticket+'"><button name="account" value="'+str(i)+'">'+label+'</button></form>' for i,label in enumerate(['Operator one','Operator two','Caller'])))
@app.post('/choose')
async def choose(r:Request):
 f=parse_qs((await r.body()).decode());entry=pending.pop(f['ticket'][0],None)
 if not entry or entry[1]<time.time():return Response(status_code=400)
 q=entry[0];code=secrets.token_urlsafe(24);codes[code]=(q,int(f['account'][0]),time.time()+60)
 return RedirectResponse(redirect+'?'+urlencode({'state':q['state'],'code':code,'iss':issuer}),status_code=303)
@app.post('/token')
async def token(r:Request):
 f=parse_qs((await r.body()).decode());entry=codes.pop(f.get('code',[''])[0],None)
 if not entry or entry[2]<time.time():return Response(status_code=400)
 q,index,_=entry
 challenge=base64.urlsafe_b64encode(hashlib.sha256(f.get('code_verifier',[''])[0].encode()).digest()).rstrip(b'=').decode()
 if challenge!=q['code_challenge'] or f.get('client_id')!=[client] or f.get('redirect_uri')!=[redirect]:return Response(status_code=400)
 now=int(time.time());claims={'iss':issuer,'sub':spec['principals'][index],'iat':now,'exp':now+1800,'aud':client,'nonce':q['nonce']}
 access={**claims,'aud':'thinkpixelag-evaluation','tenant_id':spec['tenant_id'],'roles':['users','operators','registrars'] if index<2 else ['users']}
 return {'token_type':'Bearer','id_token':jwt.encode(claims,key,algorithm='RS256',headers={'kid':'console-local'}),'access_token':jwt.encode(access,key,algorithm='RS256',headers={'kid':'console-local'})}
@app.api_route('/v1/{path:path}',methods=['GET','POST','PUT'])
async def proxy(r:Request,path:str):
 async with httpx.AsyncClient(trust_env=False,timeout=10) as c:
  headers={k:v for k,v in r.headers.items() if k.lower() in ['authorization','content-type','accept','idempotency-key','if-none-match']}
  result=await c.request(r.method,settings['api_internal']+'/v1/'+path,params=r.query_params,headers=headers,content=await r.body())
  return Response(result.content,status_code=result.status_code,headers={k:v for k,v in result.headers.items() if k.lower() in ['content-type','etag','x-ag-capability-revision','x-ag-guidance-expires']})
