// Package oapi exposes Octo download/convert/catalog helpers for hosts (FinchKit).
package oapi

import (
	"github.com/openfluke/octo/internal/catalog"
	"github.com/openfluke/octo/internal/convert"
	"github.com/openfluke/octo/internal/hub"
	"github.com/openfluke/octo/internal/paths"
	"github.com/openfluke/octo/internal/serve"
)

type Snapshot = catalog.Snapshot
type EntityInfo = catalog.EntityInfo

func ListSnapshots() []Snapshot  { return catalog.ListSnapshots() }
func ListEntities() []EntityInfo { return catalog.ListEntities() }
func DownloadRepo(repo string) (string, error) {
	return hub.DownloadRepo(repo)
}
func PackRepo(repoID string) (string, error) { return convert.PackRepo(repoID) }
func HubRoot() string                         { return paths.HubRoot() }
func EntitiesDir() string                     { return paths.EntitiesDir() }
func OutputsDir() string                      { return paths.OutputsDir() }
func ManualSnapshotDir(hubRoot, repoID string) string {
	return paths.ManualSnapshotDir(hubRoot, repoID)
}

// ServeStart hosts local .entity files on addr (default ":7878").
func ServeStart(addr string) error { return serve.Start(serve.Options{Addr: addr}) }

// ServeStop stops the in-process entity CDN.
func ServeStop() error { return serve.Stop() }

// ServeStatusJSON returns listening / LAN URLs / entity_count.
func ServeStatusJSON() string { return serve.StatusJSON() }
