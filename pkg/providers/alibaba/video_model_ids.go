package alibaba

// Video model ID constants for Alibaba Wan video models.
// Use these constants instead of raw strings to avoid typos and get IDE
// support. Any model ID is accepted by Provider.VideoModel; this list
// mirrors the TypeScript SDK's AlibabaVideoModelId documentation set.
// See https://www.alibabacloud.com/help/en/model-studio/use-video-generation
const (
	// ─── Text-to-Video ─────────────────────────────────────────────────────

	// VideoModelWan2_6T2V — wan2.6 text-to-video
	VideoModelWan2_6T2V = "wan2.6-t2v"
	// VideoModelWan2_5T2VPreview — wan2.5 text-to-video (preview)
	VideoModelWan2_5T2VPreview = "wan2.5-t2v-preview"
	// VideoModelWan2_7T2V — wan2.7 text-to-video
	VideoModelWan2_7T2V = "wan2.7-t2v"
	// VideoModelWan2_7T2V20260612 — wan2.7 text-to-video (2026-06-12)
	VideoModelWan2_7T2V20260612 = "wan2.7-t2v-2026-06-12"

	// ─── Image-to-Video (first frame) ──────────────────────────────────────

	// VideoModelWan2_6I2V — wan2.6 image-to-video
	VideoModelWan2_6I2V = "wan2.6-i2v"
	// VideoModelWan2_6I2VFlash — wan2.6 image-to-video (flash)
	VideoModelWan2_6I2VFlash = "wan2.6-i2v-flash"

	// ─── Reference-to-Video ─────────────────────────────────────────────────

	// VideoModelWan2_6R2V — wan2.6 reference-to-video
	VideoModelWan2_6R2V = "wan2.6-r2v"
	// VideoModelWan2_6R2VFlash — wan2.6 reference-to-video (flash)
	VideoModelWan2_6R2VFlash = "wan2.6-r2v-flash"
	// VideoModelWan2_7R2V — wan2.7 reference-to-video
	VideoModelWan2_7R2V = "wan2.7-r2v"
	// VideoModelWan2_7R2V20260612 — wan2.7 reference-to-video (2026-06-12)
	VideoModelWan2_7R2V20260612 = "wan2.7-r2v-2026-06-12"

	// ─── All-in-One (one id serves text-, image-, and reference-to-video) ───

	// VideoModelWan3Video — wan3.0 all-in-one video model
	VideoModelWan3Video = "wan3.0-video"
)
