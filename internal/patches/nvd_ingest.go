package patches

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// defaultNVDURL is the NVD API v2.0 CVE endpoint.
const defaultNVDURL = "https://services.nvd.nist.gov/rest/json/cves/2.0"

// NVDIngestConfig configures the NVD CVE ingestion service.
type NVDIngestConfig struct {
	// APIKey is the optional NVD API key for higher rate limits.
	APIKey string
	// BaseURL defaults to https://services.nvd.nist.gov/rest/json/cves/2.0
	BaseURL string
	// FetchTimeout per-request timeout. Default 30s.
	FetchTimeout time.Duration
	// ResultsPerPage from NVD API. Default 100.
	ResultsPerPage int
	// Logger.
	Logger *slog.Logger
}

// NVDIngester fetches CVE records from NVD and upserts them into the
// store. It is a batch processor: call IngestCVEs with a CVE-ID prefix
// or a date range, and it pages through results.
type NVDIngester struct {
	cfg   NVDIngestConfig
	store Store
	http  *http.Client
	log   *slog.Logger
}

// NewNVDIngester builds an NVDIngester.
func NewNVDIngester(cfg NVDIngestConfig, store Store) *NVDIngester {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultNVDURL
	}
	timeout := cfg.FetchTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	rpp := cfg.ResultsPerPage
	if rpp <= 0 {
		rpp = 100
	}
	cfg.BaseURL = base
	cfg.FetchTimeout = timeout
	cfg.ResultsPerPage = rpp
	return &NVDIngester{
		cfg:   cfg,
		store: store,
		http:  &http.Client{Timeout: timeout},
		log:   cfg.Logger,
	}
}

// IngestCVEs fetches CVE records from NVD matching the given options and
// upserts each into the store. It returns the total number of CVE records
// processed. opts may carry a cveId prefix (cveId), a pubStartDate /
// pubEndDate range (RFC3339), or both.
func (n *NVDIngester) IngestCVEs(ctx context.Context, opts NVDIngestOptions) (int, error) {
	processed := 0
	startIndex := 0
	for {
		reqURL, err := n.buildURL(opts, startIndex)
		if err != nil {
			return processed, err
		}
		resp, err := n.doFetch(ctx, reqURL)
		if err != nil {
			return processed, err
		}
		total, results, err := parseNVDResponse(resp)
		if err != nil {
			return processed, err
		}
		for i := range results {
			enr := nvdToEnrichment(&results[i])
			if enr == nil {
				continue
			}
			if err := n.store.UpsertCVEEnrichment(ctx, enr); err != nil {
				n.log.Warn("nvd: upsert failed", "cve", enr.CVEID, "err", err)
				continue
			}
			processed++
		}
		if total == 0 || len(results) == 0 {
			break
		}
		startIndex += len(results)
		if startIndex >= total {
			break
		}
	}
	return processed, nil
}

// NVDIngestOptions carries optional query filters for IngestCVEs.
type NVDIngestOptions struct {
	// CVEID is a CVE-ID prefix filter (e.g. "CVE-2024").
	CVEID string
	// PubStartDate / PubEndDate bound the published date range (RFC3339).
	PubStartDate string
	PubEndDate   string
}

// buildURL constructs the NVD API request URL with query params.
func (n *NVDIngester) buildURL(opts NVDIngestOptions, startIndex int) (string, error) {
	u, err := url.Parse(n.cfg.BaseURL)
	if err != nil {
		return "", fmt.Errorf("patches: parse nvd base url: %w", err)
	}
	q := u.Query()
	if opts.CVEID != "" {
		q.Set("cveId", opts.CVEID)
	}
	if opts.PubStartDate != "" {
		q.Set("pubStartDate", opts.PubStartDate)
	}
	if opts.PubEndDate != "" {
		q.Set("pubEndDate", opts.PubEndDate)
	}
	q.Set("resultsPerPage", strconv.Itoa(n.cfg.ResultsPerPage))
	q.Set("startIndex", strconv.Itoa(startIndex))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// doFetch performs the HTTP GET against NVD with the optional API key.
func (n *NVDIngester) doFetch(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("patches: build nvd request: %w", err)
	}
	if n.cfg.APIKey != "" {
		req.Header.Set("apiKey", n.cfg.APIKey)
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("patches: nvd fetch: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("patches: read nvd body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("patches: nvd http %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// nvdResponse is the subset of the NVD API v2.0 response we parse.
type nvdResponse struct {
	TotalResults int            `json:"totalResults"`
	Vulnerabilities []nvdVuln  `json:"vulnerabilities"`
}

type nvdVuln struct {
	CVE nvdCVE `json:"cve"`
}

type nvdCVE struct {
	ID             string          `json:"id"`
	Published      string          `json:"published"`
	LastModified   string          `json:"lastModified"`
	Descriptions   []nvdDesc       `json:"descriptions"`
	Metrics        json.RawMessage `json:"metrics"`
}

type nvdDesc struct {
	Lang string `json:"lang"`
	Value string `json:"value"`
}

// parseNVDResponse decodes the NVD JSON envelope and returns the total
// count and the vulnerability list.
func parseNVDResponse(body []byte) (int, []nvdVuln, error) {
	var r nvdResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, nil, fmt.Errorf("patches: decode nvd response: %w", err)
	}
	return r.TotalResults, r.Vulnerabilities, nil
}

// nvdToEnrichment maps a single NVD vulnerability into a CVEEnrichment.
func nvdToEnrichment(v *nvdVuln) *models.CVEEnrichment {
	if v == nil || v.CVE.ID == "" {
		return nil
	}
	enr := &models.CVEEnrichment{
		CVEID:     v.CVE.ID,
		Source:    "nvd",
		RawData:   v.CVE.Metrics,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	for _, d := range v.CVE.Descriptions {
		if d.Lang == "en" {
			enr.Description = d.Value
			break
		}
	}
	if enr.Description == "" && len(v.CVE.Descriptions) > 0 {
		enr.Description = v.CVE.Descriptions[0].Value
	}
	if t, err := time.Parse(time.RFC3339, v.CVE.Published); err == nil {
		enr.PublishedDate = &t
	}
	if t, err := time.Parse(time.RFC3339, v.CVE.LastModified); err == nil {
		enr.LastModified = &t
	}
	// Extract CVSS v3.x base score + severity.
	score, severity := extractCVSSV3(v.CVE.Metrics)
	enr.CvssV3Score = score
	enr.CvssV3Severity = severity
	return enr
}

// extractCVSSV3 pulls the CVSS v3.x baseScore and baseSeverity from the
// NVD metrics object. It prefers cvssMetricV31, then cvssMetricV30.
func extractCVSSV3(metrics json.RawMessage) (*float64, string) {
	if len(metrics) == 0 {
		return nil, ""
	}
	type cvssData struct {
		BaseScore  float64 `json:"baseScore"`
		BaseSeverity string `json:"baseSeverity"`
	}
	type metric struct {
		CVSSData cvssData `json:"cvssData"`
	}
	var parsed struct {
		V31 []metric `json:"cvssMetricV31"`
		V30 []metric `json:"cvssMetricV30"`
	}
	if err := json.Unmarshal(metrics, &parsed); err != nil {
		return nil, ""
	}
	pick := func(list []metric) (*float64, string) {
		if len(list) == 0 {
			return nil, ""
		}
		s := list[0].CVSSData.BaseScore
		return &s, list[0].CVSSData.BaseSeverity
	}
	if s, sev := pick(parsed.V31); s != nil {
		return s, sev
	}
	return pick(parsed.V30)
}
