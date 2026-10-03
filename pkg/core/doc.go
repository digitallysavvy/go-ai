// Package core holds helpers that sit between package ai and the providers:
// UploadFile and UploadSkill, which send a file or a skill to a provider that
// supports uploads, and EvaluateStopConditions, which the agent loop uses to
// decide when to stop. Most applications call these through package ai.
//
// UploadFile returns ErrFilesAPINotSupported, and UploadSkill returns
// ErrSkillsAPINotSupported, when the provider does not offer that API.
//
// Guide: https://goaisdk.com/docs/ai-sdk-core/upload-file-and-skill.
package core
