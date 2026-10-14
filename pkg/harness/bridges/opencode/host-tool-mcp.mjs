#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { readFileSync } from "node:fs";
import "ws";
import { z } from "zod/v4";
//#region ../harness/dist/bridge/index.js
const name = "AI_HarnessBridgeCapabilityUnsupportedError";
/**
* Signals an unsupported capability discovered inside a sandbox bridge.
* `isInstance` also recognizes the serialized error shape received by the
* host, where it can be translated to `HarnessCapabilityUnsupportedError`.
*/
var HarnessBridgeCapabilityUnsupportedError = class extends Error {
	constructor({ message, harnessId, cause }) {
		super(message);
		Object.defineProperty(this, "name", { value: name });
		this.harnessId = harnessId;
		this.cause = cause;
	}
	static isInstance(error) {
		return error != null && typeof error === "object" && "name" in error && error.name === name && "message" in error && typeof error.message === "string";
	}
};
function getNormalizedInlineImage(part) {
	if (part.type === "file" && part.mediaType.startsWith("image/") && part.mediaType !== "image/*" && part.data.type === "data" && typeof part.data.data === "string") return {
		data: part.data.data,
		mediaType: part.mediaType
	};
}
function convertHarnessToolModelOutput({ output }) {
	switch (output.type) {
		case "text":
		case "error-text": return {
			content: [{
				type: "text",
				text: output.value
			}],
			isError: output.type === "error-text"
		};
		case "json":
		case "error-json": return {
			content: [{
				type: "text",
				text: JSON.stringify(output.value)
			}],
			isError: output.type === "error-json"
		};
		case "execution-denied": return {
			content: [{
				type: "text",
				text: output.reason ?? "Tool execution denied."
			}],
			isError: true
		};
		case "content": return {
			content: output.value.map((part) => {
				if (part.type === "text") return {
					type: "text",
					text: part.text
				};
				const image = getNormalizedInlineImage(part);
				if (image != null) return {
					type: "image",
					...image
				};
				throw new HarnessBridgeCapabilityUnsupportedError({ message: `Harnesses support only text and inline images in tool model output; unsupported content '${part.type}'.` });
			}),
			isError: false
		};
	}
}
//#endregion
//#region src/bridge/host-tool-schemas.ts
function readHostToolSchemasFromEnvironment(environment) {
	const serializedSchemas = environment.TOOL_SCHEMAS_PATH ? readFileSync(environment.TOOL_SCHEMAS_PATH, "utf8") : environment.TOOL_SCHEMAS ?? "[]";
	return JSON.parse(serializedSchemas);
}
//#endregion
//#region src/bridge/json-schema-to-zod.ts
function jsonSchemaToZodShape(input) {
	return toZodShape(isJsonSchemaObject(input) ? input : {});
}
function toZodShape(schema) {
	if (!schema?.properties) return {};
	const required = new Set(schema.required);
	const shape = {};
	for (const [key, propSchema] of Object.entries(schema.properties)) {
		const propType = toZodType(propSchema);
		shape[key] = required.has(key) ? propType : propType.optional();
	}
	return shape;
}
function toZodType(schema) {
	if (!schema) return z.any();
	let zType = zodForEnum(schema) ?? zodForType(schema);
	if (schema.description) zType = zType.describe(schema.description);
	if (schema.nullable) zType = zType.nullable();
	if ("default" in schema) zType = zType.meta({ default: schema.default });
	return zType;
}
function zodForEnum(schema) {
	if (!Array.isArray(schema.enum) || schema.enum.length === 0 || !schema.enum.every(isJsonLiteral)) return;
	return z.literal(schema.enum);
}
function zodForType(schema) {
	switch ((Array.isArray(schema.type) ? schema.type.filter((t) => t !== "null") : [schema.type].filter(Boolean))[0]) {
		case "string": return z.string();
		case "number": return z.number();
		case "integer": return z.number().int();
		case "boolean": return z.boolean();
		case "array": return z.array(toZodType(schema.items));
		case "object": return z.object(toZodShape(schema));
		case "null": return z.null();
		default: return z.any();
	}
}
function isJsonSchemaObject(input) {
	return input != null && typeof input === "object" && !Array.isArray(input);
}
function isJsonLiteral(value) {
	return value === null || typeof value === "string" || typeof value === "boolean" || typeof value === "number" && Number.isFinite(value);
}
//#endregion
//#region src/bridge/host-tool-mcp.ts
const schemas = readHostToolSchemasFromEnvironment(process.env);
const relayUrl = process.env.TOOL_RELAY_URL || "";
if (!schemas.length || !relayUrl) {
	process.stderr.write("[host-tool-mcp] Missing TOOL_SCHEMAS_PATH/TOOL_SCHEMAS or TOOL_RELAY_URL; exiting\n");
	process.exit(0);
}
const server = new McpServer({
	name: "harness-tools",
	version: "1.0.0"
});
for (const schema of schemas) {
	const shape = jsonSchemaToZodShape(schema.inputSchema);
	server.tool(schema.name, schema.description ?? "", shape, async (input) => {
		const requestId = crypto.randomUUID();
		try {
			const res = await fetch(relayUrl, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					requestId,
					toolName: schema.name,
					input
				})
			});
			if (!res.ok) {
				const body = await res.text();
				throw new Error(`Tool relay ${schema.name} failed with ${res.status}: ${body.slice(0, 500)}`);
			}
			const data = await res.json();
			if (data.toolResult != null) {
				const converted = convertHarnessToolModelOutput({ output: data.toolResult.output });
				return {
					content: converted.content.map((part) => part.type === "text" ? part : {
						type: "image",
						data: part.data,
						mimeType: part.mediaType
					}),
					isError: data.isError === true || converted.isError
				};
			}
			return {
				content: [{
					type: "text",
					text: JSON.stringify(data.result ?? null)
				}],
				...data.isError === true ? { isError: true } : {}
			};
		} catch (err) {
			return {
				content: [{
					type: "text",
					text: `Error: ${String(err)}`
				}],
				isError: true
			};
		}
	});
}
const transport = new StdioServerTransport();
await server.connect(transport);
//#endregion
export {};

//# sourceMappingURL=host-tool-mcp.mjs.map