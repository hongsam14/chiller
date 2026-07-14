package config

import (
	"fmt"
	"io"
	"os"

	"github.com/hongsam14/chiller/internal/domain"
	"gopkg.in/yaml.v3"
)

type coverageFile struct {
	Items       []string          `yaml:"items"`
	Delegations map[string]string `yaml:"delegations"`
}

// LoadCoverageSpec parses the coverage config (item set + delegations) into a
// domain.CoverageSpec. Refactor B: the item set lives here, not the kernel, so
// changing the checked items is a config edit.
func LoadCoverageSpec(r io.Reader) (domain.CoverageSpec, error) {
	var f coverageFile
	if err := yaml.NewDecoder(r).Decode(&f); err != nil {
		return domain.CoverageSpec{}, fmt.Errorf("coverage spec: %w", err)
	}
	if len(f.Items) == 0 {
		return domain.CoverageSpec{}, fmt.Errorf("coverage spec: empty item set")
	}
	items := make([]domain.PSID, len(f.Items))
	for i, id := range f.Items {
		items[i] = domain.PSID(id)
	}
	dels := make(map[domain.PSID]domain.DelegateTarget, len(f.Delegations))
	for id, target := range f.Delegations {
		dels[domain.PSID(id)] = domain.DelegateTarget(target)
	}
	return domain.CoverageSpec{Items: items, Delegations: dels}, nil
}

// LoadCoverageSpecFile is a path convenience over LoadCoverageSpec.
func LoadCoverageSpecFile(path string) (domain.CoverageSpec, error) {
	// #nosec G304 -- path is an operator-supplied config location (a CLI flag),
	// inside the trust boundary. It is not user input, and constraining it to a
	// fixed directory would break the legitimate case of configs living elsewhere
	// while buying no real defence.
	fh, err := os.Open(path)
	if err != nil {
		return domain.CoverageSpec{}, err
	}
	defer func() { _ = fh.Close() }() // read-only: a failed close cannot lose data
	return LoadCoverageSpec(fh)
}
