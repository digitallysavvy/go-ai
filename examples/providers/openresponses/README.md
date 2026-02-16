# Open Responses Provider Examples

This directory contains examples demonstrating how to use the Open Responses provider with local LLMs like LMStudio, Ollama, and other OpenAI-compatible services.

## Prerequisites

Before running these examples, you need a local LLM service running:

### Option 1: LMStudio (Recommended)

1. **Download LMStudio** from https://lmstudio.ai/
2. **Download a model**:
   - Open LMStudio
   - Go to "Discover" tab
   - Download a model (recommendations below)
3. **Start the local server**:
   - Click "Local Server" in the left sidebar
   - Select your downloaded model
   - Click "Start Server"
   - Default endpoint: `http://localhost:1234/v1`

### Option 2: Ollama

1. **Install Ollama** from https://ollama.ai/
2. **Pull a model**: `ollama pull mistral`
3. **Start OpenAI-compatible server**: `ollama serve`
4. **API endpoint**: `http://localhost:11434/v1`

## Recommended Models

### For Text Generation (all examples except vision.go)
- **Mistral-7B-Instruct** - Good general-purpose model, supports tool calling
- **Llama-2-7B-Chat** - Fast and reliable for conversations
- **Llama-3-8B** - Latest Llama model with improved capabilities
- **CodeLlama-7B** - Specialized for code-related tasks

### For Vision (vision.go example only)
- **LLaVA v1.5 7B** - Popular vision model
- **BakLLaVA** - Alternative vision model
- **LLaVA v1.6 (various sizes)** - Improved vision understanding

### For Tool Calling (tool-calling.go)
- **Mistral-7B-Instruct** - Native tool calling support
- **Llama-3-8B** - Good tool calling capabilities
- Note: Not all models support tool calling. Check model documentation.

## Examples

### 1. basic-chat.go
Simple text generation with a local model.

**Features demonstrated**:
- Basic text generation
- Using system messages
- Custom generation parameters (temperature, max tokens)
- Token usage tracking

**Run**:
```bash
cd basic-chat
go run main.go
```

**What to expect**:
- Three examples showing different ways to generate text
- Response times depend on your hardware (GPU recommended)

---

### 2. chatbot.go
Interactive multi-turn conversation chatbot.

**Features demonstrated**:
- Maintaining conversation history
- Multi-turn dialogue
- Interactive user input
- Context preservation across turns

**Run**:
```bash
cd chatbot
go run main.go
```

**Usage**:
- Type your messages and press Enter
- The bot remembers previous messages
- Type `exit` or `quit` to end the conversation

**Tips**:
- Start with simple questions
- The bot's personality is defined by the system prompt
- Longer conversations use more tokens

---

### 3. streaming.go
Real-time streaming text generation.

**Features demonstrated**:
- Streaming responses (tokens appear as they're generated)
- Chunk-by-chunk processing
- Token-per-second calculation
- Performance metrics

**Run**:
```bash
cd streaming
go run main.go
```

**What to expect**:
- Text appears progressively (like ChatGPT)
- Shows streaming performance stats
- Demonstrates different streaming scenarios

**Use case**: Better user experience for long responses

---

### 4. tool-calling.go
Using tools/functions with local models.

**Features demonstrated**:
- Defining tools (functions) for the model to use
- Weather, calculator, and time/date tools
- Parallel tool usage
- Tool execution and result handling

**Run**:
```bash
cd tool-calling
go run main.go
```

**Requirements**:
- Model must support tool calling (e.g., Mistral-7B-Instruct)
- Not all models support this feature

**What happens**:
1. Model receives a question
2. Model decides to call a tool
3. Tool executes and returns result
4. Model uses result to answer question

**Tips**:
- Use clear tool descriptions
- Provide well-defined parameters
- Some models are better at tool calling than others

---

### 5. vision.go
Image understanding with vision models.

**Features demonstrated**:
- Analyzing images from URLs
- Analyzing local image files
- Comparing multiple images
- OCR (text extraction from images)

**Run**:
```bash
cd vision
go run main.go
```

**Requirements**:
- Vision model loaded in LMStudio (e.g., LLaVA)
- Internet connection for URL-based examples
- Optional: `example.jpg` in the same directory for local file example

**What to expect**:
- Detailed image descriptions
- Image comparison analysis
- Text extraction from images
- Slower than text-only generation (vision models are larger)

**Supported image formats**:
- JPEG
- PNG
- Base64-encoded images
- Remote URLs

---

## Configuration

All examples use the default LMStudio configuration:

```go
provider := openresponses.New(openresponses.Config{
    BaseURL: "http://localhost:1234/v1",
})
```

### For Ollama

Change the base URL:

```go
provider := openresponses.New(openresponses.Config{
    BaseURL: "http://localhost:11434/v1",
})
```

### For Other Services

Adjust the base URL and add authentication if needed:

```go
provider := openresponses.New(openresponses.Config{
    BaseURL: "https://my-service.com/v1",
    APIKey:  "your-api-key", // If required
    Headers: map[string]string{
        "X-Custom-Header": "value",
    },
})
```

## Troubleshooting

### "Connection refused" error
**Problem**: Cannot connect to the local service

**Solutions**:
1. Verify service is running: `curl http://localhost:1234/v1/models`
2. Check the port number matches your service
3. Ensure LMStudio server is started

### "Model not found" error
**Problem**: Model isn't loaded

**Solutions**:
1. Load a model in LMStudio (click "Load Model" in Local Server tab)
2. Check the model name matches what's loaded
3. Use "local-model" as a generic model ID

### Tool calling doesn't work
**Problem**: Model ignores tools or doesn't call them

**Solutions**:
1. Verify your model supports tool calling
2. Use a model known to support tools (Mistral-7B-Instruct)
3. Make tool descriptions very clear
4. Try a more explicit prompt

### Vision example doesn't work
**Problem**: Model can't process images

**Solutions**:
1. Ensure you loaded a vision model (LLaVA, not a text-only model)
2. Check image format is supported
3. Try a smaller image size
4. Verify the model is fully loaded

### Slow performance
**Problem**: Generation takes too long

**Solutions**:
1. Use GPU acceleration (NVIDIA GPU recommended)
2. Use smaller models (7B instead of 13B)
3. Use quantized models (Q4, Q5)
4. Reduce max_tokens parameter
5. Check CPU/GPU usage in LMStudio

### Out of memory errors
**Problem**: Model won't load or crashes

**Solutions**:
1. Use smaller model size (7B instead of 13B)
2. Use more aggressive quantization (Q4_K_M instead of Q8)
3. Close other applications
4. Check VRAM availability (for GPU)
5. Use CPU inference if VRAM insufficient

## Performance Tips

### Hardware Recommendations
- **CPU**: Modern multi-core processor (8+ cores recommended)
- **RAM**: 16GB minimum, 32GB recommended
- **GPU**: NVIDIA GPU with 8GB+ VRAM (e.g., RTX 3060 or better)
- **Storage**: SSD recommended for model loading

### Model Selection
- **7B models**: Fast, good quality, run on most hardware
- **13B models**: Better quality, need more resources
- **Quantized models**: Faster with minimal quality loss
  - Q8: Best quality, slower
  - Q5_K_M: Good balance
  - Q4_K_M: Fastest, acceptable quality

### Optimization
1. Enable GPU acceleration in LMStudio (Settings → Hardware)
2. Use smaller context windows (fewer tokens = faster)
3. Reduce max_tokens for shorter responses
4. Use streaming for better perceived performance
5. Close unnecessary applications to free up resources

## Additional Resources

- [Open Responses Provider Documentation](../../../pkg/providers/openresponses/README.md)
- [LMStudio Documentation](https://lmstudio.ai/docs)
- [Ollama Documentation](https://ollama.ai/docs)
- [Model Comparison](https://huggingface.co/spaces/lmsys/chatbot-arena-leaderboard)

## Support

For issues or questions:
- Check the main [Go-AI SDK documentation](../../../README.md)
- Review the [provider README](../../../pkg/providers/openresponses/README.md)
- Open an issue on GitHub

## Next Steps

After running these examples:
1. Experiment with different models
2. Adjust generation parameters (temperature, max_tokens)
3. Create your own tools for custom functionality
4. Build a full application using the provider
5. Explore other providers in the SDK
