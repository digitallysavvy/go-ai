// Package scaledown is the ScaleDown provider (TS @ai-sdk/scaledown,
// c6db718176): typed decisions through the ScaleDown Decisions API (POST
// {baseURL}/v1/scaledown). It exposes only a decision model for now; every
// other model family is a NoSuchModelError. The extraction model (POST
// {baseURL}/v1/chat/completions, model extract-1) lands with the extraction
// model spec.
package scaledown
