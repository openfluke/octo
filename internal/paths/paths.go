package paths

import (
	"os"
	"path/filepath"
	"strings"
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

// EntityPath is entities/<org--name>.entity (FormatNone / FP32).
func EntityPath(repoID string) string {
	name := replaceSlash(repoID) + ".entity"
	return filepath.Join(EntitiesDir(), name)
}

// EntityPathForFormat is entities/<org--name>[--format].entity.
// FormatNone uses EntityPath (no suffix).
func EntityPathForFormat(repoID string, format string) string {
	base := replaceSlash(repoID)
	if format == "" || format == "none" {
		return EntityPath(repoID)
	}
	return filepath.Join(EntitiesDir(), base+"--"+strings.ToLower(format)+".entity")
}

// EntityPathLegacyQ4 is the pre-template naming for Q4_0 packs (-q4.entity).
func EntityPathLegacyQ4(repoID string) string {
	return filepath.Join(EntitiesDir(), replaceSlash(repoID)+"-q4.entity")
}

// LogsDir is OCTO_LOGS or ./logs (relative to octo working directory).
func LogsDir() string {
	if v := os.Getenv("OCTO_LOGS"); v != "" {
		return filepath.Clean(v)
	}
	return filepath.Join(".", "logs")
}
