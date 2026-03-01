package openai

// Language model ID constants for OpenAI chat models.
// Use these constants instead of raw strings to avoid typos and get IDE support.
// See https://platform.openai.com/docs/models for the full list.
const (
	// O1 reasoning models
	ModelO1                = "o1"
	ModelO1_2024_12_17     = "o1-2024-12-17"
	ModelO3Mini            = "o3-mini"
	ModelO3Mini_2025_01_31 = "o3-mini-2025-01-31"
	ModelO3                = "o3"
	ModelO3_2025_04_16     = "o3-2025-04-16"
	ModelO4Mini            = "o4-mini"
	ModelO4Mini_2025_04_16 = "o4-mini-2025-04-16"

	// GPT-4.1 series
	ModelGPT41                 = "gpt-4.1"
	ModelGPT41_2025_04_14      = "gpt-4.1-2025-04-14"
	ModelGPT41Mini             = "gpt-4.1-mini"
	ModelGPT41Mini_2025_04_14  = "gpt-4.1-mini-2025-04-14"
	ModelGPT41Nano             = "gpt-4.1-nano"
	ModelGPT41Nano_2025_04_14  = "gpt-4.1-nano-2025-04-14"

	// GPT-4o series
	ModelGPT4o                             = "gpt-4o"
	ModelGPT4o_2024_05_13                  = "gpt-4o-2024-05-13"
	ModelGPT4o_2024_08_06                  = "gpt-4o-2024-08-06"
	ModelGPT4o_2024_11_20                  = "gpt-4o-2024-11-20"
	ModelGPT4oAudioPreview                 = "gpt-4o-audio-preview"
	ModelGPT4oAudioPreview_2024_12_17      = "gpt-4o-audio-preview-2024-12-17"
	ModelGPT4oAudioPreview_2025_06_03      = "gpt-4o-audio-preview-2025-06-03"
	ModelGPT4oMini                         = "gpt-4o-mini"
	ModelGPT4oMini_2024_07_18              = "gpt-4o-mini-2024-07-18"
	ModelGPT4oMiniAudioPreview             = "gpt-4o-mini-audio-preview"
	ModelGPT4oMiniAudioPreview_2024_12_17  = "gpt-4o-mini-audio-preview-2024-12-17"
	ModelGPT4oSearchPreview                = "gpt-4o-search-preview"
	ModelGPT4oSearchPreview_2025_03_11     = "gpt-4o-search-preview-2025-03-11"
	ModelGPT4oMiniSearchPreview            = "gpt-4o-mini-search-preview"
	ModelGPT4oMiniSearchPreview_2025_03_11 = "gpt-4o-mini-search-preview-2025-03-11"

	// GPT-4 series
	ModelGPT4Turbo         = "gpt-4-turbo"
	ModelGPT4Turbo_2024_04_09 = "gpt-4-turbo-2024-04-09"
	ModelGPT4              = "gpt-4"
	ModelGPT4_0613         = "gpt-4-0613"

	// GPT-3.5 series
	ModelGPT35Turbo_0125 = "gpt-3.5-turbo-0125"
	ModelGPT35Turbo      = "gpt-3.5-turbo"
	ModelGPT35Turbo_1106 = "gpt-3.5-turbo-1106"
	ModelGPT35Turbo16k   = "gpt-3.5-turbo-16k"

	// ChatGPT aliases
	ModelChatGPT4oLatest = "chatgpt-4o-latest"

	// GPT-5 series
	ModelGPT5                = "gpt-5"
	ModelGPT5_2025_08_07     = "gpt-5-2025-08-07"
	ModelGPT5Mini            = "gpt-5-mini"
	ModelGPT5Mini_2025_08_07 = "gpt-5-mini-2025-08-07"
	ModelGPT5Nano            = "gpt-5-nano"
	ModelGPT5Nano_2025_08_07 = "gpt-5-nano-2025-08-07"
	ModelGPT5ChatLatest      = "gpt-5-chat-latest"

	// GPT-5.1 series
	ModelGPT51             = "gpt-5.1"
	ModelGPT51_2025_11_13  = "gpt-5.1-2025-11-13"
	ModelGPT51ChatLatest   = "gpt-5.1-chat-latest"

	// GPT-5.2 series
	ModelGPT52             = "gpt-5.2"
	ModelGPT52_2025_12_11  = "gpt-5.2-2025-12-11"
	ModelGPT52ChatLatest   = "gpt-5.2-chat-latest"
	ModelGPT52Pro          = "gpt-5.2-pro"
	ModelGPT52Pro_2025_12_11 = "gpt-5.2-pro-2025-12-11"

	// GPT-5.3 Codex — coding-focused model added in #12814
	ModelGPT53Codex = "gpt-5.3-codex"
)

// Image model ID constants for OpenAI image generation models.
const (
	ModelDallE3           = "dall-e-3"
	ModelDallE2           = "dall-e-2"
	ModelGPTImage1        = "gpt-image-1"
	ModelGPTImage1Mini    = "gpt-image-1-mini"
	ModelGPTImage15       = "gpt-image-1.5"
	ModelChatGPTImageLatest = "chatgpt-image-latest"
)

// Embedding model ID constants for OpenAI embedding models.
const (
	ModelTextEmbedding3Small = "text-embedding-3-small"
	ModelTextEmbedding3Large = "text-embedding-3-large"
	ModelTextEmbeddingAda002 = "text-embedding-ada-002"
)

// Speech model ID constants for OpenAI text-to-speech models.
const (
	ModelTTS1                       = "tts-1"
	ModelTTS1_1106                  = "tts-1-1106"
	ModelTTS1HD                     = "tts-1-hd"
	ModelTTS1HD_1106                = "tts-1-hd-1106"
	ModelGPT4oMiniTTS               = "gpt-4o-mini-tts"
	ModelGPT4oMiniTTS_2025_03_20    = "gpt-4o-mini-tts-2025-03-20"
	ModelGPT4oMiniTTS_2025_12_15    = "gpt-4o-mini-tts-2025-12-15"
)

// Transcription model ID constants for OpenAI speech-to-text models.
const (
	ModelWhisper1                          = "whisper-1"
	ModelGPT4oTranscribe                   = "gpt-4o-transcribe"
	ModelGPT4oTranscribeDiarize            = "gpt-4o-transcribe-diarize"
	ModelGPT4oMiniTranscribe               = "gpt-4o-mini-transcribe"
	ModelGPT4oMiniTranscribe_2025_03_20    = "gpt-4o-mini-transcribe-2025-03-20"
	ModelGPT4oMiniTranscribe_2025_12_15    = "gpt-4o-mini-transcribe-2025-12-15"
)
