package convert

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/welvet/entity"
	"github.com/openfluke/welvet/quant"
)

// PackSafetensors converts a local HF snapshot to a Welvet .entity at outPath.
func PackSafetensors(snapDir, repoID, outPath string, format quant.Format, quiet bool) error {
	cfg := filepath.Join(snapDir, "config.json")
	if _, err := os.Stat(cfg); err != nil {
		return fmt.Errorf("need config.json in %s", snapDir)
	}
	sts, _ := filepath.Glob(filepath.Join(snapDir, "*.safetensors"))
	if len(sts) == 0 {
		return fmt.Errorf("no *.safetensors in %s", snapDir)
	}
	if format != quant.FormatNone && !quant.Supported(format) {
		return fmt.Errorf("unsupported pack format %s", format.String())
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	logf := func(block, total int, detail string) {
		if quiet {
			return
		}
		if block == 0 || block == total || block%5 == 0 || block == 1 {
			fmt.Printf("    [%d/%d] %s\n", block, total, detail)
		}
	}
	return entity.PackFromHF(snapDir, outPath, entity.PackOptions{
		Repo:     repoID,
		Format:   format,
		Progress: logf,
	})
}

// EnsureEntity packs snap → entity if missing. When force is false, returns existing path.
func EnsureEntity(snap catalog.Snapshot, format quant.Format, force bool, quiet bool) (string, error) {
	out := paths.EntityPathForFormat(snap.RepoID, format.String())
	if !force {
		if st, err := os.Stat(out); err == nil && st.Size() > 0 {
			return out, nil
		}
		if format == quant.FormatNone {
			legacy := paths.EntityPath(snap.RepoID)
			if st, err := os.Stat(legacy); err == nil && st.Size() > 0 {
				return legacy, nil
			}
		}
		if format == quant.FormatQ4_0 {
			legacy := paths.EntityPathLegacyQ4(snap.RepoID)
			if st, err := os.Stat(legacy); err == nil && st.Size() > 0 {
				return legacy, nil
			}
		}
	}
	if err := PackSafetensors(snap.Dir, snap.RepoID, out, format, quiet); err != nil {
		return "", err
	}
	return out, nil
}
