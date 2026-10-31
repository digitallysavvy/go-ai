#!/usr/bin/env python3
"""Gzips recorder output (one <pkg>.jsonl per TS package) into the committed
form: pkg/providers/testdata/tswire/<pkg>.jsonl.gz. A recording whose gzip
would exceed ~900 KB is split by TS test file into <pkg>__<test-file>.jsonl.gz.

usage: finish.py <recorder out dir> <testdata/tswire dir>
"""
import gzip, json, os, re, sys, collections

src, dst = sys.argv[1], sys.argv[2]
LIMIT = 900_000


def write(path, lines):
    # mtime=0 and no file name keep the bytes reproducible (gzip -9n)
    with open(path, 'wb') as f:
        with gzip.GzipFile(filename='', mode='wb', fileobj=f, compresslevel=9, mtime=0) as z:
            for l in lines:
                z.write(l.encode())


os.makedirs(dst, exist_ok=True)
for name in sorted(os.listdir(src)):
    if not name.endswith('.jsonl'):
        continue
    pkg = name[:-6]
    lines = open(os.path.join(src, name)).read().splitlines(keepends=True)
    out = os.path.join(dst, pkg + '.jsonl.gz')
    write(out, lines)
    if os.path.getsize(out) <= LIMIT:
        continue
    os.remove(out)
    by = collections.OrderedDict()
    for l in lines:
        f = json.loads(l)['file']
        key = re.sub(r'[^A-Za-z0-9]+', '-', os.path.basename(f).replace('.test.ts', '')).strip('-')
        by.setdefault(key, []).append(l)
    for key, ls in by.items():
        write(os.path.join(dst, f'{pkg}__{key}.jsonl.gz'), ls)
