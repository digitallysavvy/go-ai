package tencent

// TokenHub chat model IDs (TS TencentChatModelId; catalog:
// https://www.tencentcloud.com/document/product/1300/78934). Any other
// TokenHub model ID string is accepted.
const (
	ModelHy4               = "hy4"
	ModelHy4Preview        = "hy4-preview"
	ModelHy3               = "hy3"
	ModelHyMT2Pro          = "hy-mt2-pro"
	ModelHyMT2Plus         = "hy-mt2-plus"
	ModelHyMT2Lite         = "hy-mt2-lite"
	ModelDeepSeekV4Pro0813 = "deepseek-v4-pro-0813"
	ModelDeepSeekV41Flash  = "deepseek-v4.1-flash"
	ModelGLM53             = "glm-5.3"
	ModelGLM53Flash        = "glm-5.3-flash"
	ModelGLM52             = "glm-5.2"
	ModelMimoV26Pro        = "mimo-v2.6-pro"
	ModelMimoV26Flash      = "mimo-v2.6-flash"
	ModelStep5Preview      = "step-5-preview"
)

// Video model IDs (TS TencentVideoModelId). TokenHub Kling models use their
// TokenHub ID; VOD AIGC models use "vod/<ModelName>/<ModelVersion>". Any
// other ID of either shape is accepted.
const (
	// TokenHub Kling
	VideoModelKlingV4               = "kling-video-v4"
	VideoModelKlingV3               = "kling-video-v3"
	VideoModelKlingV3Turbo          = "kling-video-v3-turbo"
	VideoModelKlingV3Omni           = "kling-video-v3-omni"
	VideoModelKlingMotionControlV30 = "kling-video-motion-control-v3.0"
	VideoModelKlingMotionControlV26 = "kling-video-motion-control-v2.6"

	// VOD AIGC
	VideoModelVodVS25         = "vod/VS/2.5"
	VideoModelVodVS20         = "vod/VS/2.0"
	VideoModelVodVS20Fast     = "vod/VS/2.0-fast"
	VideoModelVodVS20Mini     = "vod/VS/2.0-mini"
	VideoModelVodKling30      = "vod/Kling/3.0"
	VideoModelVodKling30Omni  = "vod/Kling/3.0-Omni"
	VideoModelVodKling30Turbo = "vod/Kling/3.0-turbo"
	VideoModelVodKling26      = "vod/Kling/2.6"
	VideoModelVodHailuo23     = "vod/Hailuo/2.3"
	VideoModelVodViduQ3Pro    = "vod/Vidu/q3-pro"
)
