package tools

// gatewayToolErrorSchemaJSON is the TS gatewayToolErrorSchema
// (packages/gateway/src/tool/gateway-tool-error.ts, TS 2099d455e4) as zod's
// toJSONSchema renders it: a passthrough object, so unknown keys are kept and
// additionalProperties is {}.
const gatewayToolErrorSchemaJSON = `{
			"type": "object",
			"properties": {
				"error": {
					"type": "string",
					"enum": [
						"api_error",
						"configuration_error",
						"execution_error",
						"gateway_rate_limit",
						"invalid_input",
						"rate_limit",
						"timeout",
						"unknown",
						"unknown_tool"
					]
				},
				"message": {
					"type": "string"
				},
				"statusCode": {
					"type": "number"
				},
				"retryAfter": {
					"type": "string"
				},
				"statusText": {
					"type": "string"
				}
			},
			"required": [
				"error",
				"message"
			],
			"additionalProperties": {}
		}`
