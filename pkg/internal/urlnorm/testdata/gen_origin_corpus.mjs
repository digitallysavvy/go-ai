// Generates node_origin_corpus.json.gz: for every input of
// node_href_corpus.json.gz, in order, TS getRequestOrigin(input)
// (ai/src/telemetry/get-request-origin.ts, 934100cad4), null when undefined.
//
//   node pkg/internal/urlnorm/testdata/gen_origin_corpus.mjs \
//     pkg/internal/urlnorm/testdata/node_href_corpus.json.gz > /tmp/origins.json
//   gzip -9n < /tmp/origins.json > pkg/internal/urlnorm/testdata/node_origin_corpus.json.gz
//
// Recorded with Node v24.20.0.
import { readFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';

function getRequestOrigin(url) {
  if (url == null) return undefined;
  try {
    const endpoint = new URL(url);
    return endpoint.protocol === 'http:' || endpoint.protocol === 'https:'
      ? endpoint.origin
      : undefined;
  } catch {
    return undefined;
  }
}

const corpus = JSON.parse(gunzipSync(readFileSync(process.argv[2])).toString());
process.stdout.write(JSON.stringify(corpus.map(([input]) => getRequestOrigin(input) ?? null)));
