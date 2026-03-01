package klingai

// KlingAIVideoModelID is the type for KlingAI video model identifiers.
// The model ID suffix determines the generation mode:
//   - -t2v: text-to-video
//   - -i2v: image-to-video
//   - -motion-control: motion control video generation
type KlingAIVideoModelID = string

const (
	// Text-to-Video models

	// KlingV1T2V is the KlingAI v1 text-to-video model.
	KlingV1T2V KlingAIVideoModelID = "kling-v1-t2v"

	// KlingV1_6T2V is the KlingAI v1.6 text-to-video model.
	KlingV1_6T2V KlingAIVideoModelID = "kling-v1.6-t2v"

	// KlingV2MasterT2V is the KlingAI v2-master text-to-video model.
	KlingV2MasterT2V KlingAIVideoModelID = "kling-v2-master-t2v"

	// KlingV2_1MasterT2V is the KlingAI v2.1-master text-to-video model.
	KlingV2_1MasterT2V KlingAIVideoModelID = "kling-v2.1-master-t2v"

	// KlingV2_5TurboT2V is the KlingAI v2.5-turbo text-to-video model.
	KlingV2_5TurboT2V KlingAIVideoModelID = "kling-v2.5-turbo-t2v"

	// KlingV2_6T2V is the KlingAI v2.6 text-to-video model.
	KlingV2_6T2V KlingAIVideoModelID = "kling-v2.6-t2v"

	// KlingV3T2V is the KlingAI v3.0 text-to-video model.
	// Offers improved visual quality, coherence, and prompt following vs v2.x.
	KlingV3T2V KlingAIVideoModelID = "kling-v3.0-t2v"

	// Image-to-Video models

	// KlingV1I2V is the KlingAI v1 image-to-video model.
	KlingV1I2V KlingAIVideoModelID = "kling-v1-i2v"

	// KlingV1_5I2V is the KlingAI v1.5 image-to-video model.
	KlingV1_5I2V KlingAIVideoModelID = "kling-v1.5-i2v"

	// KlingV1_6I2V is the KlingAI v1.6 image-to-video model.
	KlingV1_6I2V KlingAIVideoModelID = "kling-v1.6-i2v"

	// KlingV2MasterI2V is the KlingAI v2-master image-to-video model.
	KlingV2MasterI2V KlingAIVideoModelID = "kling-v2-master-i2v"

	// KlingV2_1I2V is the KlingAI v2.1 image-to-video model.
	KlingV2_1I2V KlingAIVideoModelID = "kling-v2.1-i2v"

	// KlingV2_1MasterI2V is the KlingAI v2.1-master image-to-video model.
	KlingV2_1MasterI2V KlingAIVideoModelID = "kling-v2.1-master-i2v"

	// KlingV2_5TurboI2V is the KlingAI v2.5-turbo image-to-video model.
	KlingV2_5TurboI2V KlingAIVideoModelID = "kling-v2.5-turbo-i2v"

	// KlingV2_6I2V is the KlingAI v2.6 image-to-video model.
	KlingV2_6I2V KlingAIVideoModelID = "kling-v2.6-i2v"

	// KlingV3I2V is the KlingAI v3.0 image-to-video model.
	// Offers improved visual quality, coherence, and prompt following vs v2.x.
	KlingV3I2V KlingAIVideoModelID = "kling-v3.0-i2v"

	// Motion Control models

	// KlingV2_6MotionControl is the KlingAI v2.6 motion-control model.
	KlingV2_6MotionControl KlingAIVideoModelID = "kling-v2.6-motion-control"
)
