package ingest

import "testing"

func TestIngest_OK(t *testing.T) {
	set, err := New().Ingest(ScanRequest{URLs: []string{"https://a.test/x", "http://b.test/"}, Options: ScanOptions{MaxURLs: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.URLs) != 2 || set.URLs[0].Host != "a.test" || set.URLs[0].Scheme != "https" {
		t.Errorf("bad set: %+v", set.URLs)
	}
}

// H3 regression: an unset (zero) RatePerHost must be defaulted to a safe value,
// never left at 0 (which disables per-host throttling entirely).
func TestIngest_DefaultsUnsetOptions(t *testing.T) {
	set, err := New().Ingest(ScanRequest{URLs: []string{"https://a.test/"}}) // no options
	if err != nil {
		t.Fatal(err)
	}
	if set.Options.RatePerHost != defaultRatePerHost {
		t.Errorf("RatePerHost should default to %d, got %d", defaultRatePerHost, set.Options.RatePerHost)
	}
	if set.Options.Burst != defaultRatePerHost {
		t.Errorf("Burst should default to the rate (%d), got %d", defaultRatePerHost, set.Options.Burst)
	}
	if set.Options.MaxURLs != defaultMaxURLs {
		t.Errorf("MaxURLs should default to %d, got %d", defaultMaxURLs, set.Options.MaxURLs)
	}
}

func TestIngest_Rejects(t *testing.T) {
	i := New()
	cases := []ScanRequest{
		{URLs: []string{"://nope"}},      // malformed
		{URLs: []string{"ftp://x.test"}}, // bad scheme
		{URLs: []string{"https://a.test", "https://b.test", "https://c.test"}, Options: ScanOptions{MaxURLs: 2}}, // over bound
		{URLs: []string{"https://a.test"}, Options: ScanOptions{RatePerHost: -1}},                                // bad option
	}
	for idx, c := range cases {
		if _, err := i.Ingest(c); err == nil {
			t.Errorf("case %d: expected rejection", idx)
		}
	}
}

// A request that omits MaxURLs (zero) is still bounded by the default ceiling
// (SR-1) — the default must be applied BEFORE the check, not bypassed.
func TestIngest_DefaultCeilingEnforced(t *testing.T) {
	urls := make([]string, defaultMaxURLs+1)
	for j := range urls {
		urls[j] = "https://a.test/" + string(rune('a'+j%26))
	}
	if _, err := New().Ingest(ScanRequest{URLs: urls}); err == nil {
		t.Fatalf("expected rejection: %d URLs with no MaxURLs must hit the default %d ceiling", len(urls), defaultMaxURLs)
	}
}
