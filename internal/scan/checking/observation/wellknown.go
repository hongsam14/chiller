package observation

import "github.com/hongsam14/chiller/internal/domain"

// WellKnownPath is a benign path the engine proactively GETs on each approved host
// so the OWNING signature receives its artifact, plus the CWE of that owning
// signature. r16 (FR-6): the executor runs ONLY the owning signature on a
// synthesized item — not the whole observation set — so a path-agnostic scanner
// (PS-009 comment, PS-023 captcha, cookies) no longer fires ~14× per host on the
// sweep. The submitted URLs still exercise every signature.
type WellKnownPath struct {
	Path string
	CWE  domain.CWE
}

// WellKnownPaths returns the fixed, benign well-known paths + their owning CWE.
// Not crawling/enumeration (FR-01): small, fixed, benign (SEC-W2). One sweep per
// distinct approved host (deduped in fan-out), through the normal URL-scoped
// pipeline (approval gate + per-host rate budget), but each item runs only the
// owning signature.
func WellKnownPaths() []WellKnownPath {
	return []WellKnownPath{
		{"/robots.txt", cweRobots},           // PS-008
		{"/.env", cweEnv},                    // PS-011
		{"/.git/config", cweGit},             // PS-010
		{"/.git/HEAD", cweGit},               // PS-010
		{"/crossdomain.xml", cweCrossdomain}, // PS-022
		// PS-012 backup — backup-name heuristic, not any archive.
		{"/backup.zip", cweBackup}, {"/www.zip", cweBackup}, {"/db.sql", cweBackup},
		{"/index.php.bak", cweBackup}, {"/.env.bak", cweBackup},
		// PS-013 admin — common unauthenticated admin surfaces.
		{"/admin", cweAdmin}, {"/administrator", cweAdmin}, {"/manager/html", cweAdmin},
		{"/wp-admin", cweAdmin}, {"/phpmyadmin", cweAdmin}, {"/dashboard", cweAdmin},
		// PS-013 (FR-9, r16) — exposed API-doc surfaces, proactively probed so the
		// api-doc branch of adminPageSig can catch them without crawling (human #10).
		{"/v3/api-docs", cweAdmin}, {"/v2/api-docs", cweAdmin}, {"/api-docs", cweAdmin},
		{"/swagger.json", cweAdmin}, {"/openapi.json", cweAdmin}, {"/swagger-ui.html", cweAdmin},
		{"/actuator", cweAdmin},
		// PS-013 (r21) — product-exposure surfaces, so the exposures branch of
		// adminPageSig can catch them proactively (marker-confirmed, no crawling).
		// /actuator/health is intentionally omitted: a healthy `{"status":"UP"}` body
		// carries no distilled marker, so its owning signature could never fire.
		{"/actuator/env", cweAdmin},
		{"/phpinfo.php", cweAdmin}, {"/server-status", cweAdmin}, {"/server-info", cweAdmin},
		// PS-010 (r21) — .DS_Store directory-index artifact (folded into CWE-527).
		{"/.DS_Store", cweGit},
	}
}
