// Package config loads chiller's declarative, versioned config into runtime
// tables: the CWE→framework mapping (weakness axis) + ATT&CK attacker axis
// (FR-14/FQ6), the per-tool severity tables (FQ4=B), and the coverage spec
// (item set + delegations, refactor B). Config is data, not code — adding a
// framework or changing the checked items is a file edit (NFR-15).
package config

import (
	"fmt"
	"io"
	"os"

	"github.com/enki-offen/chiller/internal/domain"
	"gopkg.in/yaml.v3"
)

// AttackTechnique is the attacker-axis classification for a no-canonical item.
type AttackTechnique struct {
	Owner  domain.CheckerID
	Labels map[domain.Framework][]string // e.g. ATTACK → [T1584.001]
}

// MappingTable projects CWEs onto framework items (weakness axis) and holds the
// attacker axis for no-canonical items. Report-labeling only — never coverage.
type MappingTable struct {
	version  string
	weakness map[domain.CWE]map[domain.Framework][]string
	attack   map[domain.PSID]AttackTechnique
}

type mappingFile struct {
	Version         string                          `yaml:"version"`
	Map             map[string]map[string][]string  `yaml:"map"`
	AttackTechnique map[string]attackTechniqueEntry `yaml:"attackTechnique"`
}
type attackTechniqueEntry struct {
	Owner  string              `yaml:"owner"`
	Labels map[string][]string `yaml:"labels"`
}

// LoadMapping parses a mapping file.
func LoadMapping(r io.Reader) (*MappingTable, error) {
	var f mappingFile
	if err := yaml.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("mapping: %w", err)
	}
	t := &MappingTable{
		version:  f.Version,
		weakness: make(map[domain.CWE]map[domain.Framework][]string, len(f.Map)),
		attack:   make(map[domain.PSID]AttackTechnique, len(f.AttackTechnique)),
	}
	for cwe, byFw := range f.Map {
		m := make(map[domain.Framework][]string, len(byFw))
		for fw, items := range byFw {
			m[domain.Framework(fw)] = items
		}
		t.weakness[domain.CWE(cwe)] = m
	}
	for psid, e := range f.AttackTechnique {
		labels := make(map[domain.Framework][]string, len(e.Labels))
		for fw, v := range e.Labels {
			labels[domain.Framework(fw)] = v
		}
		t.attack[domain.PSID(psid)] = AttackTechnique{Owner: domain.CheckerID(e.Owner), Labels: labels}
	}
	return t, nil
}

// LoadMappingFile is a path convenience over LoadMapping.
func LoadMappingFile(path string) (*MappingTable, error) {
	// #nosec G304 -- path is an operator-supplied config location (a CLI flag),
	// inside the trust boundary. It is not user input, and constraining it to a
	// fixed directory would break the legitimate case of configs living elsewhere
	// while buying no real defence.
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }() // read-only: a failed close cannot lose data
	return LoadMapping(fh)
}

// Revision returns the mapping version stamped into reports.
func (t *MappingTable) Revision() string { return t.version }

// Project returns the framework items for a set of CWEs (deduped, order-stable).
// Absent CWE/framework ⇒ empty (R10). Totality (that every *owned* CWE has an
// entry) is asserted separately by the boot check, not here.
func (t *MappingTable) Project(cwes []domain.CWE, fw domain.Framework) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cwes {
		for _, item := range t.weakness[c][fw] {
			if !seen[item] {
				seen[item] = true
				out = append(out, item)
			}
		}
	}
	return out
}

// KnownCWE reports whether the CWE has any mapping entry (used by the boot
// totality check: every owned CWE must be known).
func (t *MappingTable) KnownCWE(c domain.CWE) bool {
	_, ok := t.weakness[c]
	return ok
}

// AttackOwners returns the attacker-axis PS-ID → owning checker map (coverage).
func (t *MappingTable) AttackOwners() map[domain.PSID]domain.CheckerID {
	out := make(map[domain.PSID]domain.CheckerID, len(t.attack))
	for id, at := range t.attack {
		out[id] = at.Owner
	}
	return out
}

// AttackLabels returns the framework labels for a no-canonical item (report).
func (t *MappingTable) AttackLabels(id domain.PSID) (AttackTechnique, bool) {
	at, ok := t.attack[id]
	return at, ok
}
