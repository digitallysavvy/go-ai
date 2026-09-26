#!/usr/bin/env node

// src/bridge/host-tool-mcp.ts
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

// src/bridge/json-schema-to-zod.ts
import { z } from "zod/v4";
function jsonSchemaToZodShape(input) {
  const schema = isJsonSchemaObject(input) ? input : {};
  return toZodShape(schema);
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
  if (!Array.isArray(schema.enum) || schema.enum.length === 0 || !schema.enum.every(isJsonLiteral)) {
    return void 0;
  }
  return z.literal(schema.enum);
}
function zodForType(schema) {
  const types = Array.isArray(schema.type) ? schema.type.filter((t) => t !== "null") : [schema.type].filter(Boolean);
  switch (types[0]) {
    case "string":
      return z.string();
    case "number":
      return z.number();
    case "integer":
      return z.number().int();
    case "boolean":
      return z.boolean();
    case "array":
      return z.array(toZodType(schema.items));
    case "object":
      return z.object(toZodShape(schema));
    case "null":
      return z.null();
    default:
      return z.any();
  }
}
function isJsonSchemaObject(input) {
  return input != null && typeof input === "object" && !Array.isArray(input);
}
function isJsonLiteral(value) {
  return value === null || typeof value === "string" || typeof value === "boolean" || typeof value === "number" && Number.isFinite(value);
}

// src/bridge/host-tool-mcp.ts
var schemas = JSON.parse(process.env.TOOL_SCHEMAS || "[]");
var relayUrl = process.env.TOOL_RELAY_URL || "";
if (!schemas.length || !relayUrl) {
  process.stderr.write(
    "[host-tool-mcp] Missing TOOL_SCHEMAS or TOOL_RELAY_URL; exiting\n"
  );
  process.exit(0);
}
var server = new McpServer({ name: "harness-tools", version: "1.0.0" });
for (const schema of schemas) {
  const shape = jsonSchemaToZodShape(schema.inputSchema);
  server.tool(
    schema.name,
    schema.description ?? "",
    shape,
    async (input) => {
      const requestId = crypto.randomUUID();
      try {
        const res = await fetch(relayUrl, {
          method: "POST",
          headers: {
            "Content-Type": "application/json"
          },
          body: JSON.stringify({ requestId, toolName: schema.name, input })
        });
        if (!res.ok) {
          const body = await res.text();
          throw new Error(
            `Tool relay ${schema.name} failed with ${res.status}: ${body.slice(0, 500)}`
          );
        }
        const data = await res.json();
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify(data.result ?? null)
            }
          ]
        };
      } catch (err) {
        return {
          content: [{ type: "text", text: `Error: ${String(err)}` }],
          isError: true
        };
      }
    }
  );
}
var transport = new StdioServerTransport();
await server.connect(transport);
//# sourceMappingURL=host-tool-mcp.mjs.map