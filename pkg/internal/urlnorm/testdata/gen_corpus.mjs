// Generates node_href_corpus.json.gz: [input, href | null] pairs where href
// is Node's `new URL(input).href` and null means `new URL(input)` threw.
//
//   node pkg/internal/urlnorm/testdata/gen_corpus.mjs > /tmp/corpus.json
//   gzip -9n < /tmp/corpus.json > pkg/internal/urlnorm/testdata/node_href_corpus.json.gz
//
// Recorded with Node v24.20.0. The corpus is deterministic (seeded PRNG).

const schemes = ['http', 'HTTP', 'https', 'HtTpS', 'ws', 'wss', 'ftp', 'file', 'FILE',
  'foo', 'S3', 'gs', 'data', 'mailto', 'blob', 'urn', 'a+b-c.d', '1x', '', 'ht tp'];
const seps = ['://', ':/', ':', ':\\\\', ':/\\', ':///', '://///', ':\\/'];
const userinfos = ['', 'u@', 'u:p@', 'u%20:p@', '@', ':@', 'u:p:q@', 'ü:ö@', 'a@b@',
  'u;=[]|@', 'u :p"@'];
const hosts = ['example.com', 'EXAMPLE.Com', 'ex ample.com', 'xn--nxasmq6b.com', 'bücher.de',
  'BÜCHER.de', 'faß.de', '例え.テスト', '127.0.0.1', '0x7f.1', '1.2.3', '256.0.0.1',
  '0177.0.0.1', '4294967295', '4294967296', '0x', '1.2.3.4.5', '[::1]', '[2001:DB8::1]',
  '[::ffff:192.168.0.1]', '[1:2:3:4:5:6:7:8]', '[::]', '[1::2::3]', '[1:0:0:0:1:0:0:0]',
  '[0:0:1:0:0:0:0:0]', '[::1.2.3.4.5]', '[1:2', 'localhost', 'LOCALHOST', 'a_b.c',
  '%41.com', '%zz', '', 'ex%2Ecom', 'a..b', 'exa\u00admple.com', 'xn--', 'a.b.', '-x.com',
  'ab--cd.com', 'x\u200dy.com', 'a.\u0661.com', 'a<b', 'a^b', 'a|b', '%00', 'ß', 'Ⓐ.com',
  'x.com.', 'xn--a.com', '1.2.3.08', '09', '0x1g'];
const ports = ['', ':', ':80', ':443', ':21', ':8080', ':0', ':65535', ':65536', ':08080',
  ':x', ':80x', '::1'];
const paths = ['', '/', '/a/b', '/a/./b/../c', '/a/%2e%2E/b', '/../a', '/a b', '/é',
  '/a^b`{}|', '\\a\\b', '/C|/x', '/C:/../..', '/%7e', '/a//b', '/.', '/..', '/a/..',
  '/a%2Fb', '/"<>', '/a\u0000b', '/\u007f', '/a/.%2E/b', "/'", '/%', '/€', '/😀', '/a/b/'];
const queries = ['', '?', '?a=b c', '?é', '?\'"<>`', '?a=%zz', '?a^{}|', '?a?b', '?\u007f'];
const fragments = ['', '#', '#a b', '#é', '#`<>"', '#a#b', '#^{}|', '#\u0001'];

const extras = [
  ' http://x/ ', '\thttp://x/\n', 'ht\ntp://x/a\tb\rc', '\u0000http://x/\u001f',
  'http://x/a b ', 'a:b c', 'a:b  #x', 'a:b ?x', 'a:b c ', 'mailto:Some One <a@b.c>',
  'data:text/plain,a b', 'data:image/png;base64,AA==', 'DATA:image/png;base64,AA==',
  'data:,', 'data:', 'data:text/html,<p>é</p>', 'urn:isbn:0451450523', 'javascript:alert(1)',
  'blob:https://x/uuid', 'about:blank', 'non-spec:/.//p', 'non-spec:/..//p', 'non-spec:/a/..//p',
  'foo:///x', 'foo://', 'foo://@x', 'foo://x:/y', 'foo://[::1]:8/', 'foo:/x/../../y',
  'file:', 'file:/', 'file:a', 'file:///', 'file://localhost/C:/x', 'file:C|/x',
  'file:///C:/a/../../b', 'file://host/C:/x', 'file:\\\\server\\share', 'file:////x',
  'file://h:8/x', 'file://u@h/x', 'http:', 'http:/', 'http://', 'http:///x', 'http://x:',
  'https://x.com:443/?#', 'http://a:b@c:80/d?e#f', 'http://u@:80/', 'http://\u3000x.com/',
  'http://x.com/\ud83d\ude00?\ud83d\ude00#\ud83d\ude00', 'gs://bucket/obj name.png',
  's3://Bucket/Key With Space', 'https://storage.googleapis.com/b/a b.png',
  'https://www.youtube.com/watch?v=dQw4w9WgXcQ', 'HTTPS://Example.COM:443/Path/../File.PDF?Q=1#F',
  'https://example.com/a%20b', 'https://example.com/a%2520b', 'https://example.com/%E2%82%AC',
  'https://ex.com/a?b=c&d=e f', 'https://ex.com\\a\\b?c\\d#e\\f', 'https://ex.com/a\\..\\b',
  // IDNA cases found by fuzzing (UTS #46 mapping, Punycode labels, Bidi).
  'http://xn--/', 'http://xn--a.com/', 'http://xn--ab$/', 'http://xn--.ü/', 'http://ü.xn--/',
  'http://xn--a.ü/', 'http://xn--abc-.ü/', 'http://xn--zca.ü/', 'http://xn--ü/', 'http://ß.xn--/',
  'http://xn--。/', 'http://xn--zz。com/', 'http://Xn--Ⅻ/', 'http://ｘｎ--Ⅻ.ü/', 'http://xn--xé例-/',
  'http://Xn--ا-/', 'http://1.א/', 'http://א.1/', 'http://a.א/', 'http://1א/', 'http://א1/',
  'http://אa/', 'http://-.א/', 'http://١.a/', 'http://ü.١/', 'http://ü.xn--a-ymc/',
  'http://aا.ü/', 'http://ü.xn--1-zhc/', 'http://ü.xn---1-zhc/', 'http://x\u200dy.ü/',
  'http://ǅ.com/', 'http://İ.com/', 'http://Σ.com/', 'http://ς.com/', 'http://a\u0300.com/',
  'http://\u0300a.com/', 'http://\ufe0fa.com/', 'http://⒈.com/', 'http://a\u3002b\uff0ec\uff61d/',
  'http://%30。א/', 'http://0x。ü/', 'http://ü%2e%30/',
  // IPv6 and IPv4 parser branches.
  ...['1:2:3:4:5:6:7:8:9', '::1:2:3:4:5:6:7:8', '1:2:3:4:5:6:1.2.3.4', '1:2:3:4:5:6:7:1.2.3.4',
    '::1.2.3.04', '::1.2.3.256', '::1.2.3.4.', '::1.2..3', '::1.2.3', '::.1.2.3', '1.2.3.4', ':1', '1:',
    '12345::', '::g', '1:::2', '1:2:3:4:5:6:7::', '::0:0:1:0:0', '1:0:0:2:0:0:0:3', 'a:b:c:d:e:f:1.2.3.4',
    '0:0:0:0:0:0:0:0', '1::', '1:2::3:4', '::ffff:0:0', 'FFFF::1', '::1:', '1:2:3:4:5:6:7:8:',
  ].map(h => 'http://[' + h + ']/'),
  ...['0x', '0X10.0', '00', '08', '0x1.0x2.0x3.0x4', '1.2.3.4.', '1..2', '.1.2', '1.2.3.4..', '0xffffffff',
    '0x100000000', '037777777777', '040000000000', '1.16777215', '1.16777216', '1.2.65535', '1.2.65536',
    '256.0.0', '1.256.0.0', '09.1', '0x0g.1', 'x.0x', 'a.1', '1.a', 'a.08', 'a.0x', '.', '..',
  ].map(h => 'http://' + h + '/'),
];

let seed = 0x5eed1234;
function rand(n) {
  seed |= 0;
  seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return (((t ^ (t >>> 14)) >>> 0) % n);
}
const pick = a => a[rand(a.length)];

const inputs = new Set(extras);
// One-component sweeps around a well-formed base, per scheme.
for (const scheme of schemes) {
  const base = { sep: '://', ui: '', host: 'example.com', port: '', path: '/p', q: '', f: '' };
  const build = o => scheme + o.sep + o.ui + o.host + o.port + o.path + o.q + o.f;
  for (const sep of seps) inputs.add(build({ ...base, sep }));
  for (const ui of userinfos) inputs.add(build({ ...base, ui }));
  for (const host of hosts) inputs.add(build({ ...base, host }));
  for (const port of ports) inputs.add(build({ ...base, port }));
  for (const path of paths) inputs.add(build({ ...base, path }));
  for (const q of queries) inputs.add(build({ ...base, q }));
  for (const f of fragments) inputs.add(build({ ...base, f }));
}
// Random combinations.
for (let i = 0; i < 2500; i++) {
  inputs.add(pick(schemes) + pick(seps) + pick(userinfos) + pick(hosts) + pick(ports) +
    pick(paths) + pick(queries) + pick(fragments));
}

const out = [];
for (const input of inputs) {
  let href = null;
  try { href = new URL(input).href; } catch { href = null; }
  out.push([input, href]);
}
process.stdout.write(JSON.stringify(out));
