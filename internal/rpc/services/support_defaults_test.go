package services

import "testing"

// TestSupportBundleSelection covers the documented "default to true" behaviour
// (MINE-151). A no-flag request used to produce an empty 29-byte archive.
func TestSupportBundleSelection(t *testing.T) {
	tests := []struct {
		name                       string
		logs, configs, system      bool
		wantLogs, wantCfg, wantSys bool
	}{
		{"all unspecified defaults to everything", false, false, false, true, true, true},
		{"explicit single selection is preserved", true, false, false, true, false, false},
		{"explicit combination is preserved", false, true, true, false, true, true},
		{"all explicit stays explicit", true, true, true, true, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logs, cfg, sys := supportBundleSelection(tc.logs, tc.configs, tc.system)
			if logs != tc.wantLogs || cfg != tc.wantCfg || sys != tc.wantSys {
				t.Errorf("supportBundleSelection(%v,%v,%v) = (%v,%v,%v), want (%v,%v,%v)",
					tc.logs, tc.configs, tc.system, logs, cfg, sys, tc.wantLogs, tc.wantCfg, tc.wantSys)
			}
		})
	}
}

// TestSupportUploadURLRequiresConfig asserts the hard-coded third-party
// endpoint is gone: without SUPPORT_BASE_URL there is no upload target.
func TestSupportUploadURLRequiresConfig(t *testing.T) {
	t.Setenv("SUPPORT_BASE_URL", "")
	svc := &SupportService{}
	if got := svc.getSupportUrl(); got != "" {
		t.Errorf("getSupportUrl() = %q, want empty when unset", got)
	}
	if got := svc.getUploadSupportUrl(); got != "" {
		t.Errorf("getUploadSupportUrl() = %q, want empty when unset", got)
	}

	t.Setenv("SUPPORT_BASE_URL", "https://support.example.test/")
	if got := svc.getUploadSupportUrl(); got != "https://support.example.test/api/v1/uploads" {
		t.Errorf("getUploadSupportUrl() = %q, want trailing-slash-normalised URL", got)
	}
}
