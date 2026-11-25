package ai

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

const invalidDecisionStateMessage = "must be a string, JSON object, or array of text, file, or json parts"

// validateDecisionState mirrors the state half of TypeScript's
// validateDecisionInput. State is a string, a JSON object
// (map[string]interface{}), or an ordered []provider.DecisionStatePart.
//
// For backward compatibility with the pre-multimodal API, a []interface{}
// that is not made of DecisionStatePart values is accepted as a JSON array
// and sent to the model as a single json part.
func validateDecisionState(state interface{}) error {
	switch v := state.(type) {
	case string:
		return nil
	case map[string]interface{}:
		if isJSONCompatible(v) {
			return nil
		}
	case []interface{}:
		if isJSONCompatible(v) {
			return nil
		}
	case []provider.DecisionStatePart:
		for _, part := range v {
			switch part.Type {
			case "text":
			case "json":
				if !isJSONCompatible(part.Value) {
					return invalidDecisionInput("state", state, invalidDecisionStateMessage)
				}
			case "file":
				if part.File == nil {
					return invalidDecisionInput("state", state, invalidDecisionStateMessage)
				}
			default:
				return invalidDecisionInput("state", state, invalidDecisionStateMessage)
			}
		}
		return nil
	}
	return invalidDecisionInput("state", state, invalidDecisionStateMessage)
}

// requireExplicitDownload is the download function a state without an
// ExperimentalDownload hook gets (TS 79d2004947): URL-backed file state is
// never fetched silently, since URL and literal-IP validation cannot protect
// against private DNS answers. A state without remote files never calls it.
func requireExplicitDownload(_ context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
	if len(requests) > 0 {
		return nil, &providererrors.DownloadError{
			URL:     requests[0].URL,
			Message: "URL file state requires experimental_download. For untrusted URLs, use createDownload({ fetch }) with a transport that validates DNS at connection time.",
		}
	}
	return []*DownloadResult{}, nil
}

// prepareDecisionState converts a validated state into ordered parts and
// resolves file URLs to inline data (remote files are downloaded with the
// call's download function, like prompt files in TypeScript's
// convertToLanguageModelPrompt with no supported URLs; without one, URL file
// state fails with a DownloadError). Mirrors TypeScript's
// prepareDecisionState.
func prepareDecisionState(ctx context.Context, state interface{}, download DownloadFunction) ([]provider.DecisionStatePart, error) {
	switch v := state.(type) {
	case string:
		return []provider.DecisionStatePart{provider.DecisionTextPart(v)}, nil
	case []provider.DecisionStatePart:
		return prepareDecisionStateParts(ctx, v, download)
	default:
		return []provider.DecisionStatePart{provider.DecisionJSONPart(v)}, nil
	}
}

func prepareDecisionStateParts(ctx context.Context, parts []provider.DecisionStatePart, download DownloadFunction) ([]provider.DecisionStatePart, error) {
	var files []types.ContentPart
	for _, part := range parts {
		if part.Type == "file" {
			files = append(files, *part.File)
		}
	}
	if len(files) == 0 {
		return append([]provider.DecisionStatePart(nil), parts...), nil
	}

	if download == nil {
		download = requireExplicitDownload
	}
	prompt, err := promptutils.NormalizePromptWithDownloadSupport(ctx, types.Prompt{
		Messages: []types.Message{{Role: types.RoleUser, Content: files}},
	}, true, download, nil)
	if err != nil {
		return nil, err
	}
	if len(prompt.Messages) != 1 || len(prompt.Messages[0].Content) != len(files) {
		return nil, fmt.Errorf("expected one user message with %d file parts", len(files))
	}

	out := make([]provider.DecisionStatePart, len(parts))
	fileIndex := 0
	for i, part := range parts {
		if part.Type != "file" {
			out[i] = part
			continue
		}
		var file types.FileContent
		switch c := prompt.Messages[0].Content[fileIndex].(type) {
		case types.FileContent:
			file = c
		case *types.FileContent:
			file = *c
		default:
			return nil, &providererrors.InvalidArgumentError{Field: "state", Message: "file part could not be converted to file content"}
		}
		fileIndex++
		out[i] = provider.DecisionFilePart(file)
	}
	return out, nil
}
