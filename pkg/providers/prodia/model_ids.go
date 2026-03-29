package prodia

// Language model IDs supported by the Prodia provider.
const (
	// LanguageModelNanoBananaImgToImgV2 is the Prodia img2img language model.
	// It accepts a text prompt and an optional input image and returns a
	// transformed image along with an optional text description.
	LanguageModelNanoBananaImgToImgV2 = "inference.nano-banana.img2img.v2"
)

// Video model IDs supported by the Prodia provider.
const (
	// VideoModelWan22LightningTxt2Vid is the Wan 2.2 Lightning text-to-video model.
	VideoModelWan22LightningTxt2Vid = "inference.wan2-2.lightning.txt2vid.v0"

	// VideoModelWan22LightningImg2Vid is the Wan 2.2 Lightning image-to-video model.
	VideoModelWan22LightningImg2Vid = "inference.wan2-2.lightning.img2vid.v0"
)

// validAspectRatios is the set of aspect ratio values accepted by Prodia
// language and video models.
var validAspectRatios = map[string]bool{
	"1:1":  true,
	"2:3":  true,
	"3:2":  true,
	"4:5":  true,
	"5:4":  true,
	"4:7":  true,
	"7:4":  true,
	"9:16": true,
	"16:9": true,
	"9:21": true,
	"21:9": true,
}
