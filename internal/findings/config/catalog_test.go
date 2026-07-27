package config

import (
	"strings"
	"testing"
)

// The real coverage config must carry a catalog entry for every accounted item,
// and the kernel spec loader must still parse the same file (the catalog section
// is invisible to it). This is the single-management-surface guarantee.
func TestCatalog_RealConfig_CoversEveryItem(t *testing.T) {
	const path = "../../../configs/coverage/kisa.yaml"

	spec, err := LoadCoverageSpecFile(path)
	if err != nil {
		t.Fatalf("coverage spec load (catalog section must not break the kernel loader): %v", err)
	}
	cat, err := LoadCatalogFile(path)
	if err != nil {
		t.Fatalf("catalog load: %v", err)
	}
	for _, id := range spec.Items {
		m, ok := cat[id]
		if !ok {
			t.Errorf("%s has no catalog entry (title/description)", id)
			continue
		}
		if m.Title == "" || m.Description == "" || m.Category == "" {
			t.Errorf("%s catalog entry incomplete: %+v", id, m)
		}
	}
}

func TestCatalog_MissingSection_IsEmptyNotError(t *testing.T) {
	cat, err := LoadCatalog(strings.NewReader("items: [PS-001]\n"))
	if err != nil {
		t.Fatalf("a file with no catalog section must not error: %v", err)
	}
	if len(cat) != 0 {
		t.Errorf("want empty catalog, got %d", len(cat))
	}
}
