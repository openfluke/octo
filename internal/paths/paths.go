package paths

import (
	"os"
	"path/filepath"
)

// HubRoot is the Hugging Face–style hub cache (OCTO_HUB or ./octo_hub).
func HubRoot() string {
	if v := os.Getenv("OCTO_HUB"); v != "" {
		return filepath.Clean(v)
	}
	return filepath.Join(".", "octo_hub")
}

// EntitiesDir holds converted .entity files (OCTO_ENTITIES or ./octo_entities).
func EntitiesDir() string {
	if v := os.Getenv("OCTO_ENTITIES"); v != "" {
		return filepath.Clean(v)
	}
	return filepath.Join(".", "octo_entities")
}

// RepoDirName turns org/name into models--org--name.
func RepoDirName(repoID string) string {
	return "models--" + replaceSlash(repoID)
}

func replaceSlash(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			b = append(b, '-', '-')
			continue
		}
		b = append(b, s[i])
	}
	return string(b)
}

// ManualSnapshotDir is hub/models--…/snapshots/manual-download.
func ManualSnapshotDir(hubRoot, repoID string) string {
	return filepath.Join(hubRoot, RepoDirName(repoID), "snapshots", "manual-download")
}

// EntityPath is entities/<org--name>.entity.
func EntityPath(repoID string) string {
	name := replaceSlash(repoID) + ".entity"
	return filepath.Join(EntitiesDir(), name)
}
