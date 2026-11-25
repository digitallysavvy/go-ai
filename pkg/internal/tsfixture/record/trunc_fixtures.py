import json,sys,os,glob
MAX=3000; KEEP=300
def tr(v):
    if isinstance(v,str):
        return v[:KEEP] if len(v)>MAX else v
    if isinstance(v,list): return [tr(x) for x in v]
    if isinstance(v,dict): return {k:tr(x) for k,x in v.items()}
    return v
n=0
for f in glob.glob('packages/*/src/**/__fixtures__/*',recursive=True):
    if os.path.getsize(f)<20000: continue
    raw=open(f).read()
    out=[]
    changed=False
    if f.endswith('.json'):
        try:
            d=json.loads(raw); new=json.dumps(tr(d)); out=[new]; changed=True
        except Exception as e: continue
    else:
        for line in raw.split('\n'):
            try:
                d=json.loads(line); out.append(json.dumps(tr(d),separators=(',',':')) if True else line); changed=True
            except Exception: out.append(line)
    if changed:
        open(f,'w').write('\n'.join(out)+('\n' if not f.endswith('.json') else ''))
        print(f, os.path.getsize(f)); n+=1
print(n)
