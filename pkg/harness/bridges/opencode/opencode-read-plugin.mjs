import { open, readdir } from "node:fs/promises";
import path from "node:path";
import { createInterface } from "node:readline";
import { z } from "zod/v4";
//#region src/bridge/opencode-read-plugin.ts
const MAX_BYTES = 51200;
const MAX_LINES = 2e3;
const MAX_LINE_LENGTH = 2e3;
const argsSchema = z.object({
	filePath: z.string(),
	offset: z.number().int().nonnegative().optional(),
	limit: z.number().int().nonnegative().optional()
});
async function harnessReadPlugin() {
	return { tool: {
		harness_read_guard: {
			description: "Internal Harness read-plugin readiness marker.",
			args: {},
			async execute() {
				throw new Error("This internal tool cannot be executed.");
			}
		},
		read: {
			description: "Read a text file, image, PDF, or list a directory. filePath is absolute or relative to the working directory. offset is 1-based; limit defaults to 2000 lines. Does not load nearby instruction files.",
			args: argsSchema.shape,
			async execute(input, ctx) {
				const args = argsSchema.parse(input);
				const filePath = path.resolve(ctx.directory, args.filePath);
				await ctx.ask({
					permission: "read",
					patterns: [filePath],
					always: ["*"],
					metadata: { filepath: filePath }
				});
				ctx.abort.throwIfAborted();
				const offset = Math.max(1, args.offset ?? 1);
				const limit = Math.min(MAX_LINES, args.limit ?? MAX_LINES);
				const file = await open(filePath, "r");
				try {
					const stat = await file.stat();
					if (stat.isDirectory()) {
						const entries = (await readdir(filePath, { withFileTypes: true })).map((entry) => entry.name + (entry.isDirectory() ? "/" : "")).sort();
						const selected = entries.slice(offset - 1, offset - 1 + limit);
						return {
							title: path.relative(ctx.worktree, filePath),
							output: `<path>${filePath}</path>\n<type>directory</type>\n<entries>\n${selected.join("\n")}\n</entries>`,
							metadata: {
								loaded: [],
								truncated: offset - 1 + limit < entries.length
							}
						};
					}
					if (!stat.isFile()) throw new Error("Only regular files and directories can be read.");
					const sample = Buffer.alloc(4096);
					const { bytesRead } = await file.read(sample, 0, sample.length, 0);
					const header = sample.subarray(0, bytesRead);
					const mime = attachmentMime(header);
					if (mime) {
						const bytes = await file.readFile();
						return {
							title: path.relative(ctx.worktree, filePath),
							output: mime === "application/pdf" ? "PDF read successfully" : "Image read successfully",
							metadata: {
								loaded: [],
								truncated: false
							},
							attachments: [{
								type: "file",
								mime,
								url: `data:${mime};base64,${bytes.toString("base64")}`
							}]
						};
					}
					if (header.includes(0)) throw new Error("Binary files are not supported by the restrictive-mode read tool.");
					const lines = [];
					let lineNumber = 0;
					let bytes = 0;
					let truncated = false;
					const stream = file.createReadStream({
						start: 0,
						autoClose: false,
						signal: ctx.abort
					});
					try {
						const reader = createInterface({
							input: stream,
							crlfDelay: Infinity
						});
						try {
							for await (const line of reader) {
								lineNumber++;
								if (lineNumber < offset) continue;
								const text = line.length > MAX_LINE_LENGTH ? `${line.slice(0, MAX_LINE_LENGTH)}... (line truncated)` : line;
								const rendered = `${lineNumber}: ${text}`;
								if (lines.length >= limit || bytes + Buffer.byteLength(rendered) + 1 > MAX_BYTES) {
									truncated = true;
									break;
								}
								lines.push(rendered);
								bytes += Buffer.byteLength(rendered) + 1;
							}
						} finally {
							reader.close();
						}
					} finally {
						stream.destroy();
					}
					return {
						title: path.relative(ctx.worktree, filePath),
						output: `<path>${filePath}</path>\n<type>file</type>\n<content>\n${lines.join("\n")}\n</content>${truncated ? "\n(Output truncated; use offset to read more.)" : ""}`,
						metadata: {
							loaded: [],
							truncated
						}
					};
				} finally {
					await file.close();
				}
			}
		}
	} };
}
function attachmentMime(header) {
	if (header.subarray(0, 5).toString() === "%PDF-") return "application/pdf";
	if (header.subarray(0, 8).equals(Buffer.from([
		137,
		80,
		78,
		71,
		13,
		10,
		26,
		10
	]))) return "image/png";
	if (header.subarray(0, 3).equals(Buffer.from([
		255,
		216,
		255
	]))) return "image/jpeg";
	if (["GIF87a", "GIF89a"].includes(header.subarray(0, 6).toString())) return "image/gif";
	if (header.subarray(0, 4).toString() === "RIFF" && header.subarray(8, 12).toString() === "WEBP") return "image/webp";
}
//#endregion
export { harnessReadPlugin as default };

//# sourceMappingURL=opencode-read-plugin.mjs.map