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
		files := fp.Files()
		if files != nil {
			return files, nil
		}
	}
	return nil, &NoSuchUploadAPIError{API: "files"}
}

func ResolveSkillsAPI(p Provider) (SkillsAPI, error) {
	if sp, ok := p.(SkillsProvider); ok {
		skills := sp.Skills()
		if skills != nil {
			return skills, nil
		}
	}
	return nil, &NoSuchUploadAPIError{API: "skills"}
}
