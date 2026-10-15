import fs from 'fs';
import vm from 'vm';
const src = fs.readFileSync(process.argv[2], 'utf8').replace(/export const/g, 'var');
const ctx = vm.createContext({});
vm.runInContext(src, ctx);
const out = {};
for (const k of ['LANGGRAPH_RESPONSE_1','LANGGRAPH_RESPONSE_2','REACT_AGENT_TOOL_CALLING']) out[k] = JSON.parse(JSON.stringify(vm.runInContext(k, ctx)));
process.stdout.write(JSON.stringify(out));
