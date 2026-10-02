import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { env } from "node:process";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { CallToolRequestSchema, ErrorCode, ListToolsRequestSchema, McpError } from "@modelcontextprotocol/sdk/types.js";
import { Agent, request } from "node:http";
//#region src/v1/bridge/host-tool-mcp-server.ts
const VERSION = "1.0.77";
function createHostToolMCPServer({ tools, revision = 1, invoke, onListTools }) {
	let catalog = createCatalog({
		revision,
		tools
	});
	let catalogAcknowledgmentError;
	const server = new Server({
		name: "@ai-sdk/harness-acp-host-tools",
		version: VERSION
	}, { capabilities: { tools: { listChanged: true } } });
	server.setRequestHandler(ListToolsRequestSchema, async () => {
		if (catalogAcknowledgmentError != null) throw catalogAcknowledgmentError;
		const current = catalog;
		if (onListTools != null) setImmediate(() => {
			onListTools({ revision: current.revision }).catch((error) => {
				catalogAcknowledgmentError = error;
			});
		});
		return { tools: current.tools.map(toMCPTool) };
	});
	server.setRequestHandler(CallToolRequestSchema, async (request) => {
		const current = catalog;
		const tool = current.byName.get(request.params.name);
		if (tool == null) throw new McpError(ErrorCode.InvalidParams, `Unknown host tool: ${request.params.name}`);
		const input = request.params.arguments ?? {};
		return toCallToolResult({ result: await invoke({
			toolName: tool.name,
			input,
			catalogRevision: current.revision
		}) });
	});
	return {
		server,
		updateCatalog: async ({ revision: nextRevision, tools: nextTools }) => {
			catalog = createCatalog({
				revision: nextRevision,
				tools: nextTools
			});
			await server.sendToolListChanged();
		}
	};
}
function createCatalog({ revision, tools }) {
	return {
		revision,
		tools: [...tools],
		byName: new Map(tools.map((tool) => [tool.name, tool]))
	};
}
function toMCPTool(tool) {
	return {
		name: tool.name,
		...tool.description == null ? {} : { description: tool.description },
		inputSchema: requireObjectSchema({
			toolName: tool.name,
			schema: tool.inputSchema ?? { type: "object" }
		})
	};
}
function requireObjectSchema({ toolName, schema }) {
	if (schema == null || typeof schema !== "object" || Array.isArray(schema) || Reflect.get(schema, "type") !== void 0 && Reflect.get(schema, "type") !== "object") throw new Error(`Host tool ${toolName} must use an object JSON Schema for MCP.`);
	return schema;
}
function toCallToolResult({ result }) {
	return {
		content: [{
			type: "text",
			text: stringifyOutput({ output: result.output })
		}],
		...result.isError ? { isError: true } : {},
		_meta: { "ai-sdk-harness-acp-correlation": result.correlationToken }
	};
}
function stringifyOutput({ output }) {
	if (output === void 0) return "null";
	try {
		return JSON.stringify(output);
	} catch {
		return JSON.stringify({ error: "Host tool output could not be serialized as JSON." });
	}
}
//#endregion
//#region src/v1/bridge/host-tool-relay-client.ts
const relayAgent = new Agent({
	keepAlive: true,
	timeout: 0
});
async function postHostToolRelay({ relayUrl, relayCredential, path, body }) {
	const serializedBody = JSON.stringify(body);
	return new Promise((resolve, reject) => {
		const relayRequest = request(new URL(path, relayUrl), {
			agent: relayAgent,
			method: "POST",
			headers: {
				authorization: `Bearer ${relayCredential}`,
				"content-type": "application/json",
				"content-length": Buffer.byteLength(serializedBody)
			}
		}, (response) => {
			const chunks = [];
			response.on("data", (chunk) => chunks.push(chunk));
			response.on("error", reject);
			response.on("end", () => {
				new Response(Buffer.concat(chunks)).json().then((value) => {
					const status = response.statusCode ?? 0;
					resolve({
						status,
						ok: status >= 200 && status < 300,
						value
					});
				}, reject);
			});
		});
		relayRequest.on("error", reject);
		relayRequest.end(serializedBody);
	});
}
//#endregion
//#region src/v1/bridge/host-tool-mcp.ts
const catalogPath = requireEnvironmentVariable({ name: "AI_SDK_ACP_HOST_TOOLS_FILE" });
const relayUrl = requireEnvironmentVariable({ name: "AI_SDK_ACP_HOST_TOOL_RELAY_URL" });
const relayCredential = requireEnvironmentVariable({ name: "AI_SDK_ACP_HOST_TOOL_RELAY_CREDENTIAL" });
const hostToolServer = createHostToolMCPServer({
	tools: await readToolCatalog({ path: catalogPath }),
	invoke: async ({ toolName, input, catalogRevision }) => validateInvocationResult({ value: await postRelay({
		path: "/invoke",
		body: {
			requestId: randomUUID(),
			toolName,
			input,
			catalogRevision
		}
	}) }),
	onListTools: async ({ revision }) => {
		await postRelay({
			path: "/catalog/seen",
			body: { revision }
		});
	}
});
await hostToolServer.server.connect(new StdioServerTransport());
watchCatalog({
	initialRevision: 1,
	updateCatalog: hostToolServer.updateCatalog
}).catch((error) => {
	process.stderr.write(`Host tool catalog synchronization failed: ${error instanceof Error ? error.message : String(error)}\n`);
	hostToolServer.server.close();
	process.exitCode = 1;
});
async function watchCatalog({ initialRevision, updateCatalog }) {
	let revision = initialRevision;
	for (;;) {
		const value = await postRelay({
			path: "/catalog/next",
			body: { afterRevision: revision }
		});
		if (isRecord(value) && value.closed === true) return;
		if (!isRecord(value) || !Number.isSafeInteger(value.revision) || value.revision < revision) throw new Error("Invalid host tool catalog poll response.");
		const nextRevision = value.revision;
		if (nextRevision === revision) continue;
		await updateCatalog({
			revision: nextRevision,
			tools: validateToolCatalog({ value: value.tools })
		});
		revision = nextRevision;
	}
}
async function postRelay({ path, body }) {
	const response = await postHostToolRelay({
		relayUrl,
		relayCredential,
		path,
		body
	});
	const { value } = response;
	if (!response.ok) throw new Error(readErrorMessage({
		value,
		status: response.status
	}));
	return value;
}
function validateToolCatalog({ value }) {
	if (!Array.isArray(value) || !value.every(isTool)) throw new Error("Invalid host tool catalog.");
	return value;
}
async function readToolCatalog({ path }) {
	const text = await readFile(path, "utf8");
	return validateToolCatalog({ value: await new Response(text, { headers: { "content-type": "application/json" } }).json() });
}
function isTool(value) {
	return isRecord(value) && typeof value.name === "string" && (value.description === void 0 || typeof value.description === "string") && (value.inputSchema === void 0 || isRecord(value.inputSchema) && !Array.isArray(value.inputSchema));
}
function validateInvocationResult({ value }) {
	if (!isRecord(value) || typeof value.correlationToken !== "string" || value.isError !== void 0 && typeof value.isError !== "boolean") throw new Error("Invalid host tool relay response.");
	return {
		output: value.output,
		...value.isError ? { isError: true } : {},
		correlationToken: value.correlationToken
	};
}
function readErrorMessage({ value, status }) {
	return isRecord(value) && typeof value.error === "string" ? value.error : `Host tool relay returned HTTP ${status}.`;
}
function requireEnvironmentVariable({ name }) {
	const value = env[name];
	if (value == null || value.length === 0) throw new Error(`Missing ${name}.`);
	return value;
}
function isRecord(value) {
	return value != null && typeof value === "object" && !Array.isArray(value);
}
//#endregion
export {};

//# sourceMappingURL=host-tool-mcp.mjs.map