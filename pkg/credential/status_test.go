package credential

import "testing"

func TestCombineStatus(t *testing.T) {
	cases := []struct {
		name    string
		primary Status
		pat     Status
		want    Status
	}{
		// Primary-dominant failures.
		{"primary error beats all", StatusError, StatusOK, StatusError},
		{"primary offline beats all", StatusOffline, StatusOK, StatusOffline},

		// "Primary alone is enough for API" case (GitHub GCM, token accounts).
		// Before the fix this returned Warning when PAT was missing — that's
		// the bug the user surfaced. Primary=OK must win regardless of PAT.
		{"GitHub GCM no PAT", StatusOK, StatusNone, StatusOK},
		{"GitHub GCM broken PAT", StatusOK, StatusWarning, StatusOK},
		{"Token OK, no PAT column", StatusOK, StatusOK, StatusOK},

		// "Primary doesn't work, PAT covers the gap" case (Forgejo GCM + PAT).
		// Before the fix this returned Warning. Discovery still works via the
		// PAT fallback in ResolveAPIToken, so overall must be OK.
		{"Forgejo GCM with PAT", StatusWarning, StatusOK, StatusOK},

		// Neither credential can reach the API — warn.
		{"Forgejo GCM no PAT", StatusWarning, StatusNone, StatusWarning},
		{"Forgejo GCM broken PAT", StatusWarning, StatusWarning, StatusWarning},

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
