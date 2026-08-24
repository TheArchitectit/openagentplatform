package reports

import (
	"testing"
	"time"
)

func TestDownloadTokenRoundTrip(t *testing.T) {
	secret := []byte("test-secret")
	orgID, reportID := "org-1", "run-abc"
	expiry := time.Now().Add(time.Hour).Unix()

	token := SignDownloadToken(secret, orgID, reportID, expiry)
	gotOrg, gotReport, err := VerifyDownloadToken(secret, token)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if gotOrg != orgID || gotReport != reportID {
		t.Errorf("got org=%q report=%q, want %q/%q", gotOrg, gotReport, orgID, reportID)
	}
}

func TestDownloadTokenWrongSecret(t *testing.T) {
	token := SignDownloadToken([]byte("secret-a"), "o", "r", time.Now().Add(time.Hour).Unix())
	if _, _, err := VerifyDownloadToken([]byte("secret-b"), token); err == nil {
		t.Error("wrong secret should fail verification")
	}
}

func TestDownloadTokenExpired(t *testing.T) {
	token := SignDownloadToken([]byte("s"), "o", "r", time.Now().Add(-time.Minute).Unix())
	if _, _, err := VerifyDownloadToken([]byte("s"), token); err == nil {
		t.Error("expired token should fail")
	}
}

func TestDownloadTokenTampered(t *testing.T) {
	secret := []byte("s")
	orgID, reportID := "org-1", "run-1"
	token := SignDownloadToken(secret, orgID, reportID, time.Now().Add(time.Hour).Unix())

	// Deterministic tamper: swap the first two payload characters. If the
	// result happens to be invalid base64 that also fails verification.
	b := []byte(token)
	b[0], b[1] = b[1], b[0]
	if _, _, err := VerifyDownloadToken(secret, string(b)); err == nil {
		t.Error("tampered token should fail")
	}

	// A token signed for a different report must not validate for this one.
	other := SignDownloadToken(secret, orgID, "run-other", time.Now().Add(time.Hour).Unix())
	if _, gotReport, err := VerifyDownloadToken(secret, other); err != nil || gotReport != "run-other" {
		t.Errorf("cross-report token mixup: report=%q err=%v", gotReport, err)
	}
}

func TestPresignedURLShape(t *testing.T) {
	d := &DefaultDeliverer{BaseURL: "https://example.com"}
	rpt := &Report{OrgID: "org-9", ID: "run-7"}
	url := d.PresignedURL(rpt, time.Hour)
	if url == "" || !contains(url, "https://example.com/api/v1/reports/runs/run-7/download?token=") {
		t.Errorf("unexpected URL: %q", url)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
