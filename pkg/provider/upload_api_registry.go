package provider

import "fmt"

type NoSuchUploadAPIError struct {
	API string
}

func (e *NoSuchUploadAPIError) Error() string {
	return fmt.Sprintf("provider does not support %s API", e.API)
}

func ResolveFilesAPI(p Provider) (FilesAPI, error) {
	if fp, ok := p.(FilesProvider); ok {
		return fp.Files(), nil
	}
	return nil, &NoSuchUploadAPIError{API: "files"}
}

func ResolveSkillsAPI(p Provider) (SkillsAPI, error) {
	if sp, ok := p.(SkillsProvider); ok {
		return sp.Skills(), nil
	}
	return nil, &NoSuchUploadAPIError{API: "skills"}
}
