package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	ctx := context.Background()

	fmt.Println("=== Multimodal Audio Understanding ===")
	fmt.Println("Note: This is a pattern example for audio understanding.")
	fmt.Println("OpenAI's current models handle audio through Whisper (speech-to-text)")
	fmt.Println("and can then analyze the transcribed text.")
	fmt.Println()

	// Example 1: Audio transcription + analysis pattern
	fmt.Println("=== Example 1: Audio Analysis Pattern ===")

	// Step 1: Transcribe audio (see speech-to-text example)
	transcribedText := "Hello, this is a sample audio message about artificial intelligence."

	// Step 2: Analyze the transcribed text
	result1, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: fmt.Sprintf(`Analyze this audio transcription:

"%s"

Provide:
1. Sentiment analysis
2. Key topics
3. Speaker intent
4. Summary`, transcribedText),
	})

	fmt.Printf("Analysis:\n%s\n\n", result1.Text)

	// Example 2: Audio classification pattern
	fmt.Println("=== Example 2: Audio Classification Pattern ===")

	audioDescription := "A recording of someone speaking quickly with excitement in their voice, discussing a new product launch."

	result2, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: fmt.Sprintf(`Classify this audio description:

"%s"

Categories:
- Type: (speech/music/ambient/mixed)
- Emotion: (happy/sad/neutral/excited/angry)
- Context: (business/personal/entertainment/educational)
- Urgency: (low/medium/high)`, audioDescription),
	})

	fmt.Printf("Classification:\n%s\n\n", result2.Text)

	// Example 3: Multi-audio comparison pattern
	fmt.Println("=== Example 3: Multi-Audio Comparison ===")

	audio1 := "Calm, professional voicemail: 'Hello, this is regarding your appointment tomorrow.'"
	audio2 := "Urgent, stressed voice: 'We need to meet ASAP, this is critical!'"

	result3, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: fmt.Sprintf(`Compare these two audio messages:

Audio 1: %s
Audio 2: %s

Compare:
1. Tone and emotion
2. Urgency level
3. Appropriate response
4. Priority`, audio1, audio2),
	})

	fmt.Printf("Comparison:\n%s\n\n", result3.Text)

	// Example 4: Audio content extraction
	fmt.Println("=== Example 4: Information Extraction ===")

	voicemail := `Voicemail transcription: "Hi, this is Sarah Johnson from ABC Company.
	I'm calling about order number 12345. We need to reschedule delivery to next Friday,
	December 15th, at 2 PM. Please call me back at 555-0123. Thanks!"`

	result4, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: fmt.Sprintf(`Extract structured information from this voicemail:

%s

Extract:
- Caller name
- Company
- Order number
- Requested date/time
- Callback number
- Action required

Format as JSON.`, voicemail),
	})

	fmt.Printf("Extracted Information:\n%s\n\n", result4.Text)

	// Example 5: Audio quality assessment pattern
	fmt.Println("=== Example 5: Audio Quality Assessment ===")

	audioQualityDescription := `Audio characteristics:
- Background noise: Moderate traffic sounds
- Clarity: Speaker sometimes muffled
- Volume: Inconsistent, varies between loud and quiet
- Duration: 3 minutes 42 seconds`

	result5, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: fmt.Sprintf(`Assess this audio recording quality:

%s

Provide:
1. Overall quality score (1-10)
2. Usability for different purposes (transcription, broadcast, archival)
3. Recommended improvements
4. Suitable post-processing steps`, audioQualityDescription),
	})

	fmt.Printf("Quality Assessment:\n%s\n\n", result5.Text)

	fmt.Println("=== Audio Understanding Workflow ===")
	workflow := `
	1. Audio Input → 2. Speech-to-Text → 3. Text Analysis → 4. Insights

	Step 1: Capture or load audio file
	Step 2: Use Whisper API for transcription (see speech-to-text example)
	Step 3: Analyze transcription with GPT-4
	Step 4: Extract insights, sentiment, actions, etc.
	`
	fmt.Println(workflow)

	fmt.Println("=== Supported Audio Analysis ===")
	features := []string{
		"✓ Speech transcription (via Whisper)",
		"✓ Sentiment analysis",
		"✓ Intent detection",
		"✓ Information extraction",
		"✓ Speaker identification (from transcription)",
		"✓ Language detection",
		"✓ Content summarization",
		"✓ Action item extraction",
	}

	for _, feature := range features {
		fmt.Println("  " + feature)
	}

	fmt.Println("\n=== Use Cases ===")
	useCases := []string{
		"• Call center analysis",
		"• Meeting insights",
		"• Podcast content extraction",
		"• Voice assistant interactions",
		"• Customer feedback analysis",
		"• Medical dictation analysis",
		"• Interview transcription & analysis",
	}

	for _, useCase := range useCases {
		fmt.Println("  " + useCase)
	}

	fmt.Println("\n=== Future: Native Audio Understanding ===")
	fmt.Println("When native audio understanding becomes available:")
	fmt.Println("  - Direct audio input (no transcription needed)")
	fmt.Println("  - Tone and emotion from audio directly")
	fmt.Println("  - Music understanding")
	fmt.Println("  - Environmental sound analysis")
	fmt.Println("  - Multi-speaker identification")
}
