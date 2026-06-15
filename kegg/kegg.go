// Package kegg is the library behind the kegg command line:
// the HTTP client, request shaping, and the typed data models for the
// KEGG REST API (rest.kegg.jp).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
// KEGG returns plain TSV text, not JSON; all parsing converts tab-separated
// lines into the typed records below.
package kegg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to KEGG. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "kegg-cli/dev (+https://github.com/tamnd/kegg-cli)"

// Host is the canonical homepage used for URI driver host matching.
const Host = "rest.kegg.jp"

// BaseURL is the root every request is built from.
const BaseURL = "https://rest.kegg.jp"

// Entry is the generic record returned by the /find endpoint:
// an id and zero or more semicolon-separated names.
type Entry struct {
	ID    string   `json:"id"`
	Names []string `json:"names"`
}

// Pathway is a KEGG pathway record returned by /list/pathway.
type Pathway struct {
	ID   string `json:"id" kit:"id"`
	Name string `json:"name"`
}

// Compound is a KEGG compound record (small molecule / metabolite).
type Compound struct {
	ID   string `json:"id" kit:"id"`
	Name string `json:"name"` // first alias
}

// Gene is a KEGG gene entry from /find/genes.
type Gene struct {
	ID   string `json:"id"`   // e.g. "hsa:672"
	Name string `json:"name"` // gene symbol(s)
	Desc string `json:"desc"` // description after the semicolon block
}

// Config holds the tunable knobs for the client. Values of zero mean
// "use the default".
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration // minimum gap between requests
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns the recommended defaults for the KEGG free API:
// 300 ms rate limit (polite), 15 s timeout, 3 retries.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Timeout:   15 * time.Second,
		Retries:   3,
	}
}

// Client talks to the KEGG REST API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with the recommended defaults.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// parseLines splits a TSV response body into rows. Each row is a slice of
// at most two fields: the id and the rest of the line.
func parseLines(body []byte) [][]string {
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	result := make([][]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		result = append(result, fields)
	}
	return result
}

// FindEntries calls /find/<db>/<query> and returns the raw Entry records.
// db is one of: compound, drug, pathway, genes, disease.
func (c *Client) FindEntries(ctx context.Context, db, query string) ([]*Entry, error) {
	url := c.BaseURL + "/find/" + db + "/" + query
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var out []*Entry
	for _, row := range parseLines(body) {
		e := &Entry{ID: row[0]}
		if len(row) > 1 {
			e.Names = strings.Split(row[1], "; ")
		}
		out = append(out, e)
	}
	return out, nil
}

// ListPathways calls /list/pathway/hsa and returns human pathway records.
func (c *Client) ListPathways(ctx context.Context) ([]*Pathway, error) {
	url := c.BaseURL + "/list/pathway/hsa"
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var out []*Pathway
	for _, row := range parseLines(body) {
		p := &Pathway{ID: row[0]}
		if len(row) > 1 {
			p.Name = row[1]
		}
		out = append(out, p)
	}
	return out, nil
}

// ListCompounds calls /list/compound and returns all compound stubs.
func (c *Client) ListCompounds(ctx context.Context) ([]*Compound, error) {
	url := c.BaseURL + "/list/compound"
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	var out []*Compound
	for _, row := range parseLines(body) {
		cmp := &Compound{ID: row[0]}
		if len(row) > 1 {
			names := strings.SplitN(row[1], "; ", 2)
			cmp.Name = names[0]
		}
		out = append(out, cmp)
	}
	return out, nil
}

// GetEntry calls /get/<id> and returns the raw flat-file text. KEGG flat
// files are structured text (ENTRY / NAME / FORMULA / etc sections), not JSON.
// The caller can parse the sections it needs; we return the whole body so
// callers are not limited by what we pre-parse.
func (c *Client) GetEntry(ctx context.Context, id string) ([]byte, error) {
	url := c.BaseURL + "/get/" + id
	return c.Get(ctx, url)
}

// GetCompound calls /get/<id> and parses the ENTRY, NAME, and FORMULA
// sections into a Compound.
func (c *Client) GetCompound(ctx context.Context, id string) (*Compound, error) {
	body, err := c.GetEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	cmp := &Compound{ID: id}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "NAME") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "NAME"))
			name = strings.TrimSuffix(name, ";")
			if cmp.Name == "" {
				cmp.Name = name
			}
		}
	}
	return cmp, nil
}
