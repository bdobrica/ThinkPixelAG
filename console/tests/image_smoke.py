"""Verify optional image custody and read-only startup without AG dependencies."""
import subprocess,json
config=subprocess.check_output(['docker','image','inspect','thinkpixelag-console:dev'],text=True)
x=json.loads(config)[0]['Config'];assert x['User']=='65532:65532';assert '--no-access-log' in x['Cmd'];assert '--workers' in x['Cmd']
p=subprocess.run(['docker','run','--rm','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true','--entrypoint','python','thinkpixelag-console:dev','-c','from ag_console.app import create_app; from ag_console.security import Settings; a=create_app(Settings("https://console.test","https://ag.test","https://id.test","console","ag")); print("non-root read-only console factory: passed")'],capture_output=True,text=True)
print(p.stdout);assert p.returncode==0
