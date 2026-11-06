# Merges a fresh extraction (extract.mjs at the target TS commit) into a
# committed case file, keeping the committed cases: each case (matched by test
# title and occurrence) gains the fresh assertions' `js` programs, own-call
# arguments and aliases it lacks. An assertion whose expression text alone
# changed (newer cast handling) takes the fresh one; a case whose assertions
# changed upstream is merged by key and reported. The committed JSON style is
# kept byte for byte, so `gzip -9n` of the output regenerates unchanged cases
# identically.
#
#   python3 -I merge_cases.py <committed.json.gz> <fresh.json> <out.json>
#   gzip -9n < out.json > <committed.json.gz>
import json, sys, gzip, collections
committed, fresh, out = sys.argv[1], sys.argv[2], sys.argv[3]
raw = gzip.open(committed).read().decode()
old = json.loads(raw, object_pairs_hook=collections.OrderedDict)
style = None
for seps in ((',', ':'), (', ', ': ')):
    for asc in (False, True):
        if json.dumps(old, separators=seps, ensure_ascii=asc) == raw.rstrip('\n'):
            style = (seps, asc)
if style is None:
    sys.exit("committed file does not round-trip in any known style: " + committed)
new = json.load(open(fresh), object_pairs_hook=collections.OrderedDict)
idx = collections.defaultdict(list)
for c in new:
    idx[c['test']].append(c)
seen = collections.Counter()
stats = collections.Counter()
key = lambda a: json.dumps([a['expr'], a['matcher'], a['not'], a['arg']], sort_keys=True)
semkey = lambda a: json.dumps([a['matcher'], a['not'], a['arg']], sort_keys=True)
for c in old:
    n = seen[c['test']]; seen[c['test']] += 1
    cands = idx.get(c['test'], [])
    if n >= len(cands) or 'asserts' not in c:
        stats['nomatch'] += 1; continue
    f = cands[n]
    fas = f.get('asserts', [])
    if [key(a) for a in c['asserts']] != [key(a) for a in fas]:
        if [semkey(a) for a in c['asserts']] == [semkey(a) for a in fas]:
            # only the expression text changed (newer cast handling): take the fresh assertion
            for i, a in enumerate(fas):
                if key(c['asserts'][i]) != key(a):
                    print("expr rewritten:", c['test'], '|', c['asserts'][i]['expr'].replace('\n', ' ')[:70], '->', a['expr'][:70], file=sys.stderr)
                    c['asserts'][i] = a; stats['expr-rewritten'] += 1
        else:
            stats['changed-asserts'] += 1
            print("changed upstream (merged by key):", c['test'], file=sys.stderr)
    fa = {}
    for a in fas:
        fa.setdefault(key(a), []).append(a)
    used = collections.Counter()
    for a in c['asserts']:
        k = key(a)
        lst = fa.get(k)
        if not lst: continue
        src = lst[min(used[k], len(lst)-1)]; used[k] += 1
        for fld in ('callArgs', 'callSuffix'):
            if fld in src and fld not in a:
                a[fld] = src[fld]; stats['own-call'] += 1
        if 'js' in src and a.get('js') != src['js']:
            a['js'] = src['js']; stats['js'] += 1
    al = c.get('aliases')
    if al is not None:
        for k2, v in f.get('aliases', {}).items():
            if k2 not in al:
                al[k2] = v; stats['alias'] += 1
with open(out, 'w') as g:
    g.write(json.dumps(old, separators=style[0], ensure_ascii=style[1]) + ('\n' if raw.endswith('\n') else ''))
print(dict(stats), file=sys.stderr)
