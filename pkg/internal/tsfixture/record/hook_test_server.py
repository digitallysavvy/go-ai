#!/usr/bin/env python3
"""Adds the recording hooks to a copy of @ai-sdk/test-server's create-test-server.ts."""
import sys

p = sys.argv[1]
s = open(p).read()
a = ("      return http.all(url, ({ request }) => {\n        const callNumber = calls.length;\n\n"
     "        calls.push(new TestServerCall(request));\n")
b = ("      return http.all(url, async ({ request }) => {\n        const callNumber = calls.length;\n"
     "        const __rec: any = (globalThis as any).__recHttpStart?.(url, request);\n"
     "        if (__rec) { try { __rec.bodyB64 = Buffer.from(await request.clone().arrayBuffer()).toString('base64'); } catch (e) {} }\n\n"
     "        calls.push(new TestServerCall(request));\n")
assert a in s, "create-test-server.ts changed: update hook_test_server.py"
s = s.replace(a, b)
c = "        const handlerType = response.type;\n"
assert c in s
s = s.replace(c, c + "        (globalThis as any).__recHttpResponse?.(__rec, response);\n")
open(p, 'w').write(s)
