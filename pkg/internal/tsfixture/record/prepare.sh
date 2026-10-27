#!/bin/bash
# Prepares a scratch copy of the TS AI SDK for recording (see README.md).
#
# usage: prepare.sh <ai repo> <scratch dir>
#
# Copies the provider packages and their workspace dependencies (no build
# needed: vitest.config.mjs resolves @ai-sdk/* to src), trims the workspace,
# installs the dependencies, installs the test-server hook, truncates large
# test fixtures and links this directory in as <scratch>/rec.
set -euo pipefail
AI=$(cd "$1" && pwd)
W=$2
HERE=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$W/packages" "$W/tools" "$W/rec"
cp "$AI/pnpm-lock.yaml" "$W/" 2>/dev/null || true
for p in provider provider-utils test-server anthropic anthropic-aws amazon-bedrock google google-vertex openai openai-compatible azure xai groq mistral deepseek moonshotai gateway cohere elevenlabs black-forest-labs heygen typesafe-ai alibaba cartesia voyage prodia assemblyai baseten bytedance cerebras deepgram deepinfra fal fireworks fish-audio gladia gmicloud huggingface hume klingai luma minimax open-responses perplexity quiverai replicate revai togetherai topaz zai; do
  [ -d "$AI/packages/$p" ] && rsync -a --exclude node_modules --exclude dist "$AI/packages/$p" "$W/packages/"
done
rsync -a --exclude node_modules "$AI/tools/tsconfig" "$W/tools/"
# tools/tsdown-config is a workspace package (@vercel/ai-tsdown-config) since
# TS e64ed82f27; older checkouts have a single tools/tsdown-config.mts file.
TSDOWN_WS=""
if [ -d "$AI/tools/tsdown-config" ]; then
  rsync -a --exclude node_modules "$AI/tools/tsdown-config" "$W/tools/"
  TSDOWN_WS="  - 'tools/tsdown-config'"
elif [ -f "$AI/tools/tsdown-config.mts" ]; then
  cp "$AI/tools/tsdown-config.mts" "$W/tools/"
fi
# the packages import 'ai' only in a few tests; drop the workspace dependency
perl -ni -e 'print unless /"ai": "workspace:\*"/' "$W"/packages/*/package.json
{
  echo "packages:"
  echo "  - 'packages/*'"
  echo "  - 'tools/tsconfig'"
  [ -n "$TSDOWN_WS" ] && echo "$TSDOWN_WS"
  echo "allowBuilds: {}"
} > "$W/pnpm-workspace.yaml"
cat > "$W/package.json" <<'JSON'
{ "name": "ai-repo", "private": true, "type": "module", "devDependencies": { "vitest": "4.1.11", "typescript": "6.0.3" } }
JSON
cp "$HERE/setup.mjs" "$HERE/vitest.config.mjs" "$W/rec/"
# hook the test server: record every request and the response it served
mkdir -p "$W/rec/test-server"
cp "$W"/packages/test-server/src/*.ts "$W/rec/test-server/"
rm -f "$W/rec/test-server/with-vitest.test.ts"
python3 "$HERE/hook_test_server.py" "$W/rec/test-server/create-test-server.ts"
(cd "$W" && CI=true pnpm install --no-frozen-lockfile --ignore-scripts --config.manage-package-manager-versions=false)
ln -sfn "$W/packages/test-server/node_modules" "$W/rec/node_modules"
# truncate the audio fixtures and large opaque strings so recordings stay small
for f in $(find "$W/packages" -name 'transcript-test.mp3' -o -name 'transcription-test.mp3' | grep -v node_modules); do head -c 64 "$f" > "$f.tmp" && mv "$f.tmp" "$f"; done
(cd "$W" && python3 "$HERE/trunc_fixtures.py")
cat > "$W/rec/run.sh" <<'RUN'
#!/bin/bash
# usage: run.sh pkg...   (records packages/<pkg> into rec/out/<pkg>.jsonl)
W=$(cd "$(dirname "$0")/.." && pwd)
for n in "$@"; do
  rm -f "$W/rec/out/$n.jsonl"
  (cd "$W/packages/$n" && REC_OUT="$W/rec/out" timeout 900 npx vitest run --config ../../rec/vitest.config.mjs > "$W/rec/log-$n.txt" 2>&1)
  echo "$n $(grep -E 'Tests ' "$W/rec/log-$n.txt" | tail -1)"
done
RUN
chmod +x "$W/rec/run.sh"
