package detect

import (
	"os"
	"path/filepath"
)

type DetectResult struct {
	Framework Framework
	Matched   bool
}

func DetectFramework(dir string) DetectResult {
	for _, fw := range Frameworks {
		if matchesFramework(dir, fw) {
			return DetectResult{Framework: fw, Matched: true}
		}
	}
	return DetectResult{}
}

func matchesFramework(dir string, fw Framework) bool {
	if len(fw.Files) == 0 {
		return false
	}
	for _, file := range fw.Files {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func ListDetectedFrameworks(dir string) []Framework {
	var detected []Framework
	for _, fw := range Frameworks {
		if matchesFramework(dir, fw) {
			detected = append(detected, fw)
		}
	}
	return detected
}
