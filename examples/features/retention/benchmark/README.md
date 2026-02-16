# Retention Settings - Memory Benchmark

This example benchmarks the memory savings achieved by using retention settings to exclude request/response bodies from results.

## Overview

This benchmark demonstrates the real-world memory impact of retention settings by:
1. Measuring memory usage with retention enabled (default)
2. Measuring memory usage with retention disabled (optimized)
3. Comparing memory usage across multiple requests
4. Quantifying the memory savings

## Running the Benchmark

```bash
export OPENAI_API_KEY=your-api-key
go run main.go
```

## What This Example Measures

### Single Request Benchmark
- Creates a large prompt (simulating image metadata or large context)
- Measures heap memory before and after generation
- Compares memory usage with and without retention
- Calculates memory savings

### Multiple Request Benchmark
- Simulates 10 consecutive requests
- Demonstrates how savings compound over multiple requests
- Shows real-world production scenario

## Expected Results

Typical results for large prompts (100KB+):

```
=== Memory Comparison ===
With retention:    2.5 MB
Without retention: 0.5 MB
Savings:           2.0 MB (80%)
```

For multiple requests:
```
Multi-request savings: 15-20 MB (60-80%)
```

## Memory Measurement Notes

Go's garbage collector can affect memory measurements:
- We call `runtime.GC()` before measurements for accuracy
- Memory usage can vary between runs due to GC timing
- Savings become more apparent with:
  - Larger prompts (images, documents)
  - More requests over time
  - Longer-running processes

## Key Insights

### When Retention Settings Matter Most

1. **Image Processing**
   - Base64-encoded images in prompts can be 1-5 MB each
   - Response bodies can contain large image descriptions
   - Savings: 50-80% per request

2. **Large Context Windows**
   - Documents, code repositories, long conversations
   - Context can be 10-100 KB per request
   - Savings: 40-60% per request

3. **Long-Running Services**
   - Memory accumulates over thousands of requests
   - Can prevent out-of-memory errors
   - Improves GC performance

4. **Privacy-Sensitive Applications**
   - Don't store confidential request data
   - Comply with data retention policies
   - Reduce security audit surface

### What's Not Affected

Retention settings do NOT impact:
- Generated text content
- Token usage tracking
- Finish reasons
- Tool calls and results
- Warnings
- Error handling
- Any functional behavior

## Performance Tips

1. **Enable in Production**: Use retention by default for production workloads
2. **Disable in Development**: Keep retention enabled for debugging
3. **Profile Your App**: Use `pprof` to measure actual memory usage
4. **Monitor GC**: Watch GC pause times with retention enabled/disabled

## Benchmarking Your Own Workload

Modify this example to test your specific use case:

```go
// Test with your actual prompt structure
largePrompt := loadYourPromptData()

// Measure memory over your typical request pattern
for i := 0; i < yourRequestCount; i++ {
    result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
        Model:                 model,
        Prompt:                largePrompt,
        ExperimentalRetention: retention,
    })
    // Process result...
}
```

## Related Documentation

- [Memory Optimization Guide](../../../../docs/06-advanced/11-memory-optimization.md)
- [Basic Retention Example](../basic/)
- [Go Memory Profiling](https://go.dev/blog/pprof)

## Troubleshooting

### Inconsistent Results
If you see inconsistent memory measurements:
- Run the benchmark multiple times
- Increase the number of requests
- Use larger prompts
- Monitor with `runtime.MemStats` over time

### Negative Savings
If savings appear negative:
- This can happen due to GC timing
- Run with `GOGC=off` to disable GC during measurement
- Focus on trends over multiple runs rather than single measurements
