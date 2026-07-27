package observation

// WellKnownPaths returns the fixed set of benign well-known request paths the
// engine GETs on each approved host, so the owning URL-scoped signatures actually
// receive their artifact instead of only ever seeing the submitted URL's response
// (FR-03/FR-05, r10). Without this the robots/.env/.git/crossdomain/backup/admin
// items report false-clean or N/A on an ordinary scan.
//
// This is NOT crawling or enumeration (FR-01): the list is small, fixed, and
// benign (SEC-W2). The orchestrator expands each relative path to a full
// scheme://host+path Target per distinct approved host (host-scoped dedup, one
// sweep per host) and lets the existing URL-scoped pipeline (approval gate +
// per-host rate budget + signatures) judge each response.
//
// The owning signature for each path keys off the response body and/or the
// Target.URL path (see signatures.go), so this list carries no CWE mapping — it
// just decides what gets fetched.
func WellKnownPaths() []string {
	return []string{
		"/robots.txt",      // PS-008 robots
		"/.env",            // PS-011 env
		"/.git/config",     // PS-010 git (config)
		"/.git/HEAD",       // PS-010 git (HEAD)
		"/crossdomain.xml", // PS-022 crossdomain
		// PS-012 backup — names that match the backup-name heuristic, not any archive.
		"/backup.zip", "/www.zip", "/db.sql", "/index.php.bak", "/.env.bak",
		// PS-013 admin — common unauthenticated admin surfaces.
		"/admin", "/administrator", "/manager/html", "/wp-admin", "/phpmyadmin", "/dashboard",
	}
}
