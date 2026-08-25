package patches

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/pashagolub/pgxmock/v4"
)

// TestUpsertCVEEnrichment_Insert verifies a new CVE enrichment is inserted
// with the expected columns and source default.
func TestUpsertCVEEnrichment_Insert(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	score := 9.8
	now := time.Now()
	cve := &models.CVEEnrichment{
		CVEID:          "CVE-2024-12345",
		Source:         "nvd",
		CvssV3Score:    &score,
		CvssV3Severity: "CRITICAL",
		Description:    "A critical vuln",
		PublishedDate:  &now,
		LastModified:   &now,
	}
	// The upsert uses ON CONFLICT (cve_id) DO UPDATE. pgxmock cannot
	// distinguish insert vs update at the SQL string level; match the
	// prefix. The store dereferences *float64 to float64 for the
	// cvss_v3_score arg, so use pgxmock.AnyArg() for the score.
	pool.ExpectExec(`INSERT INTO cve_enrichment`).
		WithArgs(
			pgxmock.AnyArg(), "CVE-2024-12345", "nvd", pgxmock.AnyArg(), "CRITICAL",
			"A critical vuln", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	if err := s.UpsertCVEEnrichment(context.Background(), cve); err != nil {
		t.Fatalf("UpsertCVEEnrichment: %v", err)
	}
}

// TestUpsertCVEEnrichment_Idempotent verifies upserting the same CVE twice
// does not error and issues no duplicate row (the ON CONFLICT clause
// handles dedup server-side; the store simply re-issues the statement).
func TestUpsertCVEEnrichment_Idempotent(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	cve := &models.CVEEnrichment{CVEID: "CVE-2024-12345", Source: "nvd"}
	pool.ExpectExec(`INSERT INTO cve_enrichment`).
		WithArgs(pgxmock.AnyArg(), "CVE-2024-12345", "nvd", nil, "", "", nil, nil, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := s.UpsertCVEEnrichment(context.Background(), cve); err != nil {
		t.Fatalf("first UpsertCVEEnrichment: %v", err)
	}
	// Second upsert: same cve_id, conflict path updates instead of inserting.
	pool.ExpectExec(`INSERT INTO cve_enrichment`).
		WithArgs(pgxmock.AnyArg(), "CVE-2024-12345", "nvd", nil, "", "", nil, nil, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := s.UpsertCVEEnrichment(context.Background(), cve); err != nil {
		t.Fatalf("second UpsertCVEEnrichment: %v", err)
	}
}

// TestLookupCVEsByKB_OrgScoped verifies the join query filters by org_id
// and kb and returns the matching enrichment rows.
func TestLookupCVEsByKB_OrgScoped(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	score := 7.5
	rows := pgxmock.NewRows([]string{
		"id", "cve_id", "source", "cvss_v3_score", "cvss_v3_severity",
		"description", "published_date", "last_modified", "raw_data",
		"created_at", "updated_at",
	}).
		AddRow(uuid.NewString(), "CVE-2024-12345", "nvd", &score, "HIGH",
			"desc", nil, nil, []byte("{}"), time.Now(), time.Now())
	// The join uses patch_catalog.cve_ids @> to_jsonb(ARRAY[ce.cve_id])
	// with WHERE pc.org_id = $1 AND pc.kb = $2.
	pool.ExpectQuery(`FROM cve_enrichment ce JOIN patch_catalog pc`).
		WithArgs(orgID, "KB5001234").
		WillReturnRows(rows)

	got, err := s.LookupCVEsByKB(context.Background(), orgID, "KB5001234")
	if err != nil {
		t.Fatalf("LookupCVEsByKB: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LookupCVEsByKB: got %d rows, want 1", len(got))
	}
	if got[0].CVEID != "CVE-2024-12345" {
		t.Errorf("unexpected cve_id %q", got[0].CVEID)
	}
	if got[0].CvssV3Score == nil || *got[0].CvssV3Score != 7.5 {
		t.Errorf("unexpected cvss score %v", got[0].CvssV3Score)
	}
}

// TestLookupKBsByCVE_OrgScoped verifies the query filters by org_id and
// cve_ids containment, returning CVEKBMatch rows.
func TestLookupKBsByCVE_OrgScoped(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	score := 9.1
	rows := pgxmock.NewRows([]string{"kb", "title", "severity", "cve_ids", "cvss_score"}).
		AddRow("KB5001234", "Security Update", "critical", []string{"CVE-2024-12345"}, &score)
	// WHERE org_id = $1 AND cve_ids @> to_jsonb($2::text)
	pool.ExpectQuery(`SELECT kb, COALESCE\(title, ''\), COALESCE\(severity, ''\), cve_ids, cvss_score FROM patch_catalog WHERE org_id = \$1 AND cve_ids @> to_jsonb\(\$2::text\)`).
		WithArgs(orgID, "CVE-2024-12345").
		WillReturnRows(rows)

	got, err := s.LookupKBsByCVE(context.Background(), orgID, "CVE-2024-12345")
	if err != nil {
		t.Fatalf("LookupKBsByCVE: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LookupKBsByCVE: got %d rows, want 1", len(got))
	}
	if got[0].KB != "KB5001234" || got[0].Severity != "critical" {
		t.Errorf("unexpected match %+v", got[0])
	}
	if got[0].CvssScore == nil || *got[0].CvssScore != 9.1 {
		t.Errorf("unexpected cvss score %v", got[0].CvssScore)
	}
}

// TestPatchCatalogUpdateCVEIDs verifies the cve_ids JSONB is set on the
// matching patch_catalog row scoped by org_id and kb.
func TestPatchCatalogUpdateCVEIDs(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	pool.ExpectExec(`UPDATE patch_catalog SET cve_ids = \$3, updated_at = now\(\) WHERE org_id = \$1 AND kb = \$2`).
		WithArgs(orgID, "KB5001234", []byte(`["CVE-2024-12345","CVE-2024-99999"]`)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := s.PatchCatalogUpdateCVEIDs(context.Background(), orgID, "KB5001234",
		[]string{"CVE-2024-12345", "CVE-2024-99999"}); err != nil {
		t.Fatalf("PatchCatalogUpdateCVEIDs: %v", err)
	}
}
