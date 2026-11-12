// Package tencent implements the Tencent Cloud provider: TokenHub chat
// models over the OpenAI-compatible protocol, TokenHub Kling video models,
// and Video on Demand (VOD) AIGC video models signed with Tencent Cloud API
// 3.0 (TC3-HMAC-SHA256). It mirrors @ai-sdk/tencent (TS 1c7a311d53).
//
// TokenHub models authenticate with an API key (TENCENT_TOKENHUB_API_KEY);
// VOD models with a SecretId / SecretKey pair and a VOD application ID
// (TENCENTCLOUD_SECRET_ID, TENCENTCLOUD_SECRET_KEY,
// TENCENTCLOUD_VOD_SUB_APP_ID), resolved only when a VOD request is made.
package tencent
