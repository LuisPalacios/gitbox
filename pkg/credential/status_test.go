package credential

import "testing"

func TestCombineStatus(t *testing.T) {
	// Post-refactor, GCM's Primary only reports credential presence
	// (OK or Error), and the API state rides on the PAT column. The
	// Warning-primary cases are still exercised because token / SSH
	// accounts keep the older semantics where the primary credential
	// can fail the API test.
	cases := []struct {
		name    string
		primary Status
		pat     Status
		want    Status
	}{
		// Primary-dominant failures.
		{"primary error beats all", StatusError, StatusOK, StatusError},
		{"primary offline beats all", StatusOffline, StatusOK, StatusOffline},

		// GCM path: Primary=OK always means "credential cached"; the PAT
		// column tells the API story. These are the cases users actually
		// hit in the Change-credential panel.
		{"GCM credential present, GCM also reaches API", StatusOK, StatusOK, StatusOK},
		{"GCM credential present, GCM fails API, no PAT (Forgejo pwd)", StatusOK, StatusNone, StatusWarning},
		{"GCM credential present, companion PAT covers API", StatusOK, StatusOK, StatusOK},
		{"GCM credential present, PAT stored but broken", StatusOK, StatusWarning, StatusWarning},
		{"GCM credential present, API offline", StatusOK, StatusOffline, StatusOffline},

		// Token / SSH paths: Primary=Warning means "credential present but
		// API rejected it". PAT=OK still wins — the API IS reachable.
		{"Forgejo SSH + PAT", StatusWarning, StatusOK, StatusOK},
		{"Forgejo SSH no PAT", StatusWarning, StatusNone, StatusWarning},
		{"Forgejo SSH broken PAT", StatusWarning, StatusWarning, StatusWarning},

		// PAT is offline and primary can't cover — surface as offline so the
		// user knows to retry later rather than reconfigure.
		{"primary warn, pat offline", StatusWarning, StatusOffline, StatusOffline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := combineStatus(tc.primary, tc.pat)
			if got != tc.want {
				t.Fatalf("combineStatus(%v, %v) = %v, want %v", tc.primary, tc.pat, got, tc.want)
			}
		})
	}
}
