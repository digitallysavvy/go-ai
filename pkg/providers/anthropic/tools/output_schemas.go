package tools

// Code generated from the TypeScript SDK tool modules (packages/anthropic/src/tool/*.ts):
// each outputSchema rendered by zod 4.4.3 toJSONSchema(schema, {target:
// 'draft-7', io: 'input', reused: 'inline'}), as zodSchema does, without the
// $schema key. ValidateUIMessages checks provider tool outputs against them.

import "encoding/json"

var toolOutputSchemaJSON = map[string]string{
	"anthropic.advisor_20260301": `{
		"oneOf": [
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "advisor_result"
					},
					"text": {
						"type": "string"
					},
					"stopReason": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"text"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "advisor_redacted_result"
					},
					"encryptedContent": {
						"type": "string"
					},
					"stopReason": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"encryptedContent"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "advisor_tool_result_error"
					},
					"errorCode": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"errorCode"
				]
			}
		]
	}`,
	"anthropic.code_execution_20250522": `{
		"type": "object",
		"properties": {
			"type": {
				"type": "string",
				"const": "code_execution_result"
			},
			"stdout": {
				"type": "string"
			},
			"stderr": {
				"type": "string"
			},
			"return_code": {
				"type": "number"
			},
			"content": {
				"default": [],
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"type": {
							"type": "string",
							"const": "code_execution_output"
						},
						"file_id": {
							"type": "string"
						}
					},
					"required": [
						"type",
						"file_id"
					]
				}
			}
		},
		"required": [
			"type",
			"stdout",
			"stderr",
			"return_code"
		]
	}`,
	"anthropic.code_execution_20250825": `{
		"oneOf": [
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "code_execution_result"
					},
					"stdout": {
						"type": "string"
					},
					"stderr": {
						"type": "string"
					},
					"return_code": {
						"type": "number"
					},
					"content": {
						"default": [],
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"type": {
									"type": "string",
									"const": "code_execution_output"
								},
								"file_id": {
									"type": "string"
								}
							},
							"required": [
								"type",
								"file_id"
							]
						}
					}
				},
				"required": [
					"type",
					"stdout",
					"stderr",
					"return_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "bash_code_execution_result"
					},
					"content": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"type": {
									"type": "string",
									"const": "bash_code_execution_output"
								},
								"file_id": {
									"type": "string"
								}
							},
							"required": [
								"type",
								"file_id"
							]
						}
					},
					"stdout": {
						"type": "string"
					},
					"stderr": {
						"type": "string"
					},
					"return_code": {
						"type": "number"
					}
				},
				"required": [
					"type",
					"content",
					"stdout",
					"stderr",
					"return_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "bash_code_execution_tool_result_error"
					},
					"error_code": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"error_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_tool_result_error"
					},
					"error_code": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"error_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_view_result"
					},
					"content": {
						"type": "string"
					},
					"file_type": {
						"type": "string"
					},
					"num_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"start_line": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"total_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					}
				},
				"required": [
					"type",
					"content",
					"file_type",
					"num_lines",
					"start_line",
					"total_lines"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_create_result"
					},
					"is_file_update": {
						"type": "boolean"
					}
				},
				"required": [
					"type",
					"is_file_update"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_str_replace_result"
					},
					"lines": {
						"anyOf": [
							{
								"type": "array",
								"items": {
									"type": "string"
								}
							},
							{
								"type": "null"
							}
						]
					},
					"new_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"new_start": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"old_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"old_start": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					}
				},
				"required": [
					"type",
					"lines",
					"new_lines",
					"new_start",
					"old_lines",
					"old_start"
				]
			}
		]
	}`,
	"anthropic.code_execution_20260120": `{
		"oneOf": [
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "code_execution_result"
					},
					"stdout": {
						"type": "string"
					},
					"stderr": {
						"type": "string"
					},
					"return_code": {
						"type": "number"
					},
					"content": {
						"default": [],
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"type": {
									"type": "string",
									"const": "code_execution_output"
								},
								"file_id": {
									"type": "string"
								}
							},
							"required": [
								"type",
								"file_id"
							]
						}
					}
				},
				"required": [
					"type",
					"stdout",
					"stderr",
					"return_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "encrypted_code_execution_result"
					},
					"encrypted_stdout": {
						"type": "string"
					},
					"stderr": {
						"type": "string"
					},
					"return_code": {
						"type": "number"
					},
					"content": {
						"default": [],
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"type": {
									"type": "string",
									"const": "code_execution_output"
								},
								"file_id": {
									"type": "string"
								}
							},
							"required": [
								"type",
								"file_id"
							]
						}
					}
				},
				"required": [
					"type",
					"encrypted_stdout",
					"stderr",
					"return_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "bash_code_execution_result"
					},
					"content": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"type": {
									"type": "string",
									"const": "bash_code_execution_output"
								},
								"file_id": {
									"type": "string"
								}
							},
							"required": [
								"type",
								"file_id"
							]
						}
					},
					"stdout": {
						"type": "string"
					},
					"stderr": {
						"type": "string"
					},
					"return_code": {
						"type": "number"
					}
				},
				"required": [
					"type",
					"content",
					"stdout",
					"stderr",
					"return_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "bash_code_execution_tool_result_error"
					},
					"error_code": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"error_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_tool_result_error"
					},
					"error_code": {
						"type": "string"
					}
				},
				"required": [
					"type",
					"error_code"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_view_result"
					},
					"content": {
						"type": "string"
					},
					"file_type": {
						"type": "string"
					},
					"num_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"start_line": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"total_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					}
				},
				"required": [
					"type",
					"content",
					"file_type",
					"num_lines",
					"start_line",
					"total_lines"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_create_result"
					},
					"is_file_update": {
						"type": "boolean"
					}
				},
				"required": [
					"type",
					"is_file_update"
				]
			},
			{
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "text_editor_code_execution_str_replace_result"
					},
					"lines": {
						"anyOf": [
							{
								"type": "array",
								"items": {
									"type": "string"
								}
							},
							{
								"type": "null"
							}
						]
					},
					"new_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"new_start": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"old_lines": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					},
					"old_start": {
						"anyOf": [
							{
								"type": "number"
							},
							{
								"type": "null"
							}
						]
					}
				},
				"required": [
					"type",
					"lines",
					"new_lines",
					"new_start",
					"old_lines",
					"old_start"
				]
			}
		]
	}`,
	"anthropic.tool_search_bm25_20251119": `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {
				"type": {
					"type": "string",
					"const": "tool_reference"
				},
				"toolName": {
					"type": "string"
				}
			},
			"required": [
				"type",
				"toolName"
			]
		}
	}`,
	"anthropic.tool_search_regex_20251119": `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {
				"type": {
					"type": "string",
					"const": "tool_reference"
				},
				"toolName": {
					"type": "string"
				}
			},
			"required": [
				"type",
				"toolName"
			]
		}
	}`,
	"anthropic.web_fetch_20250910": `{
		"type": "object",
		"properties": {
			"type": {
				"type": "string",
				"const": "web_fetch_result"
			},
			"url": {
				"type": "string"
			},
			"content": {
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "document"
					},
					"title": {
						"anyOf": [
							{
								"type": "string"
							},
							{
								"type": "null"
							}
						]
					},
					"citations": {
						"type": "object",
						"properties": {
							"enabled": {
								"type": "boolean"
							}
						},
						"required": [
							"enabled"
						]
					},
					"source": {
						"anyOf": [
							{
								"type": "object",
								"properties": {
									"type": {
										"type": "string",
										"const": "base64"
									},
									"mediaType": {
										"type": "string",
										"const": "application/pdf"
									},
									"data": {
										"type": "string"
									}
								},
								"required": [
									"type",
									"mediaType",
									"data"
								]
							},
							{
								"type": "object",
								"properties": {
									"type": {
										"type": "string",
										"const": "text"
									},
									"mediaType": {
										"type": "string",
										"const": "text/plain"
									},
									"data": {
										"type": "string"
									}
								},
								"required": [
									"type",
									"mediaType",
									"data"
								]
							}
						]
					}
				},
				"required": [
					"type",
					"title",
					"source"
				]
			},
			"retrievedAt": {
				"anyOf": [
					{
						"type": "string"
					},
					{
						"type": "null"
					}
				]
			}
		},
		"required": [
			"type",
			"url",
			"content",
			"retrievedAt"
		]
	}`,
	"anthropic.web_fetch_20260209": `{
		"type": "object",
		"properties": {
			"type": {
				"type": "string",
				"const": "web_fetch_result"
			},
			"url": {
				"type": "string"
			},
			"content": {
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "document"
					},
					"title": {
						"anyOf": [
							{
								"type": "string"
							},
							{
								"type": "null"
							}
						]
					},
					"citations": {
						"type": "object",
						"properties": {
							"enabled": {
								"type": "boolean"
							}
						},
						"required": [
							"enabled"
						]
					},
					"source": {
						"anyOf": [
							{
								"type": "object",
								"properties": {
									"type": {
										"type": "string",
										"const": "base64"
									},
									"mediaType": {
										"type": "string",
										"const": "application/pdf"
									},
									"data": {
										"type": "string"
									}
								},
								"required": [
									"type",
									"mediaType",
									"data"
								]
							},
							{
								"type": "object",
								"properties": {
									"type": {
										"type": "string",
										"const": "text"
									},
									"mediaType": {
										"type": "string",
										"const": "text/plain"
									},
									"data": {
										"type": "string"
									}
								},
								"required": [
									"type",
									"mediaType",
									"data"
								]
							}
						]
					}
				},
				"required": [
					"type",
					"title",
					"source"
				]
			},
			"retrievedAt": {
				"anyOf": [
					{
						"type": "string"
					},
					{
						"type": "null"
					}
				]
			}
		},
		"required": [
			"type",
			"url",
			"content",
			"retrievedAt"
		]
	}`,
	"anthropic.web_fetch_20260318": `{
		"type": "object",
		"properties": {
			"type": {
				"type": "string",
				"const": "web_fetch_result"
			},
			"url": {
				"type": "string"
			},
			"content": {
				"type": "object",
				"properties": {
					"type": {
						"type": "string",
						"const": "document"
					},
					"title": {
						"anyOf": [
							{
								"type": "string"
							},
							{
								"type": "null"
							}
						]
					},
					"citations": {
						"type": "object",
						"properties": {
							"enabled": {
								"type": "boolean"
							}
						},
						"required": [
							"enabled"
						]
					},
					"source": {
						"anyOf": [
							{
								"type": "object",
								"properties": {
									"type": {
										"type": "string",
										"const": "base64"
									},
									"mediaType": {
										"type": "string",
										"const": "application/pdf"
									},
									"data": {
										"type": "string"
									}
								},
								"required": [
									"type",
									"mediaType",
									"data"
								]
							},
							{
								"type": "object",
								"properties": {
									"type": {
										"type": "string",
										"const": "text"
									},
									"mediaType": {
										"type": "string",
										"const": "text/plain"
									},
									"data": {
										"type": "string"
									}
								},
								"required": [
									"type",
									"mediaType",
									"data"
								]
							}
						]
					}
				},
				"required": [
					"type",
					"title",
					"source"
				]
			},
			"retrievedAt": {
				"anyOf": [
					{
						"type": "string"
					},
					{
						"type": "null"
					}
				]
			}
		},
		"required": [
			"type",
			"url",
			"content",
			"retrievedAt"
		]
	}`,
	"anthropic.web_search_20250305": `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {
				"url": {
					"type": "string"
				},
				"title": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "null"
						}
					]
				},
				"pageAge": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "null"
						}
					]
				},
				"encryptedContent": {
					"type": "string"
				},
				"type": {
					"type": "string",
					"const": "web_search_result"
				}
			},
			"required": [
				"url",
				"title",
				"pageAge",
				"encryptedContent",
				"type"
			]
		}
	}`,
	"anthropic.web_search_20260209": `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {
				"url": {
					"type": "string"
				},
				"title": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "null"
						}
					]
				},
				"pageAge": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "null"
						}
					]
				},
				"encryptedContent": {
					"type": "string"
				},
				"type": {
					"type": "string",
					"const": "web_search_result"
				}
			},
			"required": [
				"url",
				"title",
				"pageAge",
				"encryptedContent",
				"type"
			]
		}
	}`,
	"anthropic.web_search_20260318": `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {
				"url": {
					"type": "string"
				},
				"title": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "null"
						}
					]
				},
				"pageAge": {
					"anyOf": [
						{
							"type": "string"
						},
						{
							"type": "null"
						}
					]
				},
				"encryptedContent": {
					"type": "string"
				},
				"type": {
					"type": "string",
					"const": "web_search_result"
				}
			},
			"required": [
				"url",
				"title",
				"pageAge",
				"encryptedContent",
				"type"
			]
		}
	}`,
}

// toolOutputSchema returns a fresh copy of the TS output schema of the provider
// tool with the given id, or nil when TS declares none.
func toolOutputSchema(id string) map[string]interface{} {
	raw, ok := toolOutputSchemaJSON[id]
	if !ok {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		panic("invalid output schema for " + id + ": " + err.Error())
	}
	return m
}
