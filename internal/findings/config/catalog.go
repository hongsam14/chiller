package config

import (
	"fmt"
	"io"
	"os"

	"github.com/hongsam14/chiller/internal/domain"
	"gopkg.in/yaml.v3"
)

// ItemMeta is human-facing metadata for one compliance item — the title,
// one-line description, and category shown in the customer compliance report.
// It is PRESENTATION metadata, deliberately separate from the accounting kernel
// (domain.CoverageSpec): the kernel decides which items exist and how they are
// judged; this only decides how they are labelled. ActiveHint marks items a
// passive engine structurally cannot fully judge (JS/SPA login, credential
// testing), so the report can honestly suggest the active/AI stage instead of
// implying a clean pass.
type ItemMeta struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Category    string `yaml:"category"`
	ActiveHint  bool   `yaml:"activeHint"`
}

// catalogFile is the `catalog:` section of the coverage config. It shares the
// file with the item set + delegations (a single management surface) but is read
// by a separate loader — LoadCoverageSpec ignores it, LoadCatalog reads only it.
type catalogFile struct {
	Catalog map[string]ItemMeta `yaml:"catalog"`
}

// LoadCatalog parses the `catalog:` section into a PS-ID → ItemMeta map. A file
// with no catalog section is not an error — it yields an empty map, and the
// report view falls back to bare PS-IDs.
func LoadCatalog(r io.Reader) (map[domain.PSID]ItemMeta, error) {
	var f catalogFile
	if err := yaml.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	out := make(map[domain.PSID]ItemMeta, len(f.Catalog))
	for id, m := range f.Catalog {
		out[domain.PSID(id)] = m
	}
	return out, nil
}

// LoadCatalogFile is a path convenience over LoadCatalog.
func LoadCatalogFile(path string) (map[domain.PSID]ItemMeta, error) {
	// #nosec G304 -- path is an operator-supplied config location (a CLI flag),
	// inside the trust boundary, same as the coverage/mapping loaders.
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }() // read-only: a failed close cannot lose data
	return LoadCatalog(fh)
}
