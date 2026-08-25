package patches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// UpsertCVEEnrichment inserts or updates a CVE enrichment record.
// ON CONFLICT(cve_id) DO UPDATE.
func (s *pgPatchStore) UpsertCVEEnrichment(ctx context.Context, cve *models.CVEEnrichment) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if cve == nil || cve.CVEID == "" {
		return errors.New("patches: cve_id is required")
	}

	source := cve.Source
	if source == "" {
		source = "nvd"
	}
	raw := cve.RawData
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var cvssScore, published, modified any
	if cve.CvssV3Score != nil {
		cvssScore = *cve.CvssV3Score
	}
	if cve.PublishedDate != nil {
		published = *cve.PublishedDate
	}
	if cve.LastModified != nil {
		modified = *cve.LastModified
	}

	const upsert = `
		INSERT INTO cve_enrichment
			(id, cve_id, source, cvss_v3_score, cvss_v3_severity, description,
			 published_date, last_modified, raw_data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		ON CONFLICT (cve_id) DO UPDATE
		SET source = EXCLUDED.source,
			cvss_v3_score = EXCLUDED.cvss_v3_score,
			cvss_v3_severity = EXCLUDED.cvss_v3_severity,
			description = EXCLUDED.description,
			published_date = EXCLUDED.published_date,
			last_modified = EXCLUDED.last_modified,
			raw_data = EXCLUDED.raw_data,
			updated_at = now()
	`
	if _, err := s.pool.Exec(ctx, upsert,
		uuid.NewString(), cve.CVEID, source, cvssScore, cve.CvssV3Severity,
		cve.Description, published, modified, raw,
	); err != nil {
		return fmt.Errorf("patches: upsert cve enrichment: %w", err)
	}
	return nil
}

// GetCVEEnrichment fetches a single CVE by its CVE-ID.
func (s *pgPatchStore) GetCVEEnrichment(ctx context.Context, cveID string) (*models.CVEEnrichment, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	if cveID == "" {
		return nil, errors.New("patches: cve_id is required")
	}
	const q = `
		SELECT id, cve_id, source, cvss_v3_score, cvss_v3_severity,
			COALESCE(description, ''), published_date, last_modified, raw_data,
			created_at, updated_at
		FROM cve_enrichment
		WHERE cve_id = $1
		LIMIT 1
	`
	row := s.pool.QueryRow(ctx, q, cveID)
	return scanCVEEnrichment(row)
}

// ListCVEEnrichments returns the most recently modified CVE records.
func (s *pgPatchStore) ListCVEEnrichments(ctx context.Context, limit int) ([]models.CVEEnrichment, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT id, cve_id, source, cvss_v3_score, cvss_v3_severity,
			COALESCE(description, ''), published_date, last_modified, raw_data,
			created_at, updated_at
		FROM cve_enrichment
		ORDER BY last_modified DESC NULLS LAST, created_at DESC
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("patches: list cve enrichments: %w", err)
	}
	defer rows.Close()
	return scanCVEEnrichments(rows)
}

// PatchCatalogUpdateCVEIDs updates the cve_ids JSONB on the matching
// patch_catalog row. Idempotent: sets the array, does not append.
func (s *pgPatchStore) PatchCatalogUpdateCVEIDs(ctx context.Context, orgID, kb string, cveIDs []string) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if orgID == "" || kb == "" {
		return errors.New("patches: org_id and kb are required")
	}
	b, err := json.Marshal(cveIDs)
	if err != nil {
		return fmt.Errorf("patches: marshal cve_ids: %w", err)
	}
	const q = `
		UPDATE patch_catalog
		SET cve_ids = $3, updated_at = now()
		WHERE org_id = $1 AND kb = $2
	`
	if _, err := s.pool.Exec(ctx, q, orgID, kb, b); err != nil {
		return fmt.Errorf("patches: update patch_catalog cve_ids: %w", err)
	}
	return nil
}

// PatchCatalogUpdateCVSS updates the cvss_score on the matching
// patch_catalog row. The score is the max CVSS v3 score across all
// CVEs associated with this KB.
func (s *pgPatchStore) PatchCatalogUpdateCVSS(ctx context.Context, orgID, kb string, cvssScore *float64) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if orgID == "" || kb == "" {
		return errors.New("patches: org_id and kb are required")
	}
	var score any
	if cvssScore != nil {
		score = *cvssScore
	}
	const q = `
		UPDATE patch_catalog
		SET cvss_score = $3, updated_at = now()
		WHERE org_id = $1 AND kb = $2
	`
	if _, err := s.pool.Exec(ctx, q, orgID, kb, score); err != nil {
		return fmt.Errorf("patches: update patch_catalog cvss_score: %w", err)
	}
	return nil
}

// LookupCVEsByKB returns all CVE enrichment records referenced by a
// patch_catalog row's cve_ids array.
func (s *pgPatchStore) LookupCVEsByKB(ctx context.Context, orgID, kb string) ([]models.CVEEnrichment, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	if orgID == "" || kb == "" {
		return nil, errors.New("patches: org_id and kb are required")
	}
	const q = `
		SELECT ce.id, ce.cve_id, ce.source, ce.cvss_v3_score, ce.cvss_v3_severity,
			COALESCE(ce.description, ''), ce.published_date, ce.last_modified, ce.raw_data,
			ce.created_at, ce.updated_at
		FROM cve_enrichment ce
		JOIN patch_catalog pc ON pc.cve_ids @> to_jsonb(ARRAY[ce.cve_id])
		WHERE pc.org_id = $1 AND pc.kb = $2
	`
	rows, err := s.pool.Query(ctx, q, orgID, kb)
	if err != nil {
		return nil, fmt.Errorf("patches: lookup cves by kb: %w", err)
	}
	defer rows.Close()
	return scanCVEEnrichments(rows)
}

// LookupKBsByCVE returns all patch_catalog rows whose cve_ids contain
// the given CVE ID, within the specified org.
func (s *pgPatchStore) LookupKBsByCVE(ctx context.Context, orgID, cveID string) ([]CVEKBMatch, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	if orgID == "" || cveID == "" {
		return nil, errors.New("patches: org_id and cve_id are required")
	}
	const q = `
		SELECT kb, COALESCE(title, ''), COALESCE(severity, ''), cve_ids, cvss_score
		FROM patch_catalog
		WHERE org_id = $1 AND cve_ids @> to_jsonb($2::text)
		ORDER BY cvss_score DESC NULLS LAST, kb ASC
		LIMIT 200
	`
	rows, err := s.pool.Query(ctx, q, orgID, cveID)
	if err != nil {
		return nil, fmt.Errorf("patches: lookup kbs by cve: %w", err)
	}
	defer rows.Close()

	out := make([]CVEKBMatch, 0, 8)
	for rows.Next() {
		var m CVEKBMatch
		var title, severity string
		var cveIDs []string
		var score *float64
		if err := rows.Scan(&m.KB, &title, &severity, &cveIDs, &score); err != nil {
			return nil, fmt.Errorf("patches: scan kb match: %w", err)
		}
		m.Title = title
		m.Severity = severity
		m.CVEIDs = cveIDs
		m.CvssScore = score
		out = append(out, m)
	}
	return out, rows.Err()
}

// scanCVEEnrichment scans a single enrichment row.
func scanCVEEnrichment(row pgx.Row) (*models.CVEEnrichment, error) {
	var r models.CVEEnrichment
	var raw []byte
	if err := row.Scan(
		&r.ID, &r.CVEID, &r.Source, &r.CvssV3Score, &r.CvssV3Severity,
		&r.Description, &r.PublishedDate, &r.LastModified, &raw,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("patches: cve enrichment not found: %w", ErrCVEEnrichmentNotFound)
		}
		return nil, fmt.Errorf("patches: scan cve enrichment: %w", err)
	}
	if len(raw) > 0 {
		r.RawData = raw
	}
	return &r, nil
}

// scanCVEEnrichments scans a result set of enrichment rows.
func scanCVEEnrichments(rows pgx.Rows) ([]models.CVEEnrichment, error) {
	out := make([]models.CVEEnrichment, 0, 8)
	for rows.Next() {
		var r models.CVEEnrichment
		var raw []byte
		if err := rows.Scan(
			&r.ID, &r.CVEID, &r.Source, &r.CvssV3Score, &r.CvssV3Severity,
			&r.Description, &r.PublishedDate, &r.LastModified, &raw,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("patches: scan cve enrichment: %w", err)
		}
		if len(raw) > 0 {
			r.RawData = raw
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrCVEEnrichmentNotFound is returned when a CVE enrichment row does not exist.
var ErrCVEEnrichmentNotFound = errors.New("cve enrichment not found")