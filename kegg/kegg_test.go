package kegg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.Rate = 0 // no pacing in tests
	c.BaseURL = srv.URL
	return c
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestFindEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/find/compound/glucose" {
			http.NotFound(w, r)
			return
		}
		// KEGG TSV response for /find/compound/glucose
		_, _ = w.Write([]byte("C00031\tD-Glucose; Grape sugar; Dextrose\nC00293\tD-Glucosamine\n"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	entries, err := c.FindEntries(context.Background(), "compound", "glucose")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].ID != "C00031" {
		t.Errorf("entries[0].ID = %q, want C00031", entries[0].ID)
	}
	if len(entries[0].Names) != 3 {
		t.Errorf("entries[0].Names = %v, want 3 names", entries[0].Names)
	}
	if entries[0].Names[0] != "D-Glucose" {
		t.Errorf("entries[0].Names[0] = %q, want D-Glucose", entries[0].Names[0])
	}
}

func TestListPathways(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/list/pathway/hsa" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("path:hsa00010\tGlycolysis / Gluconeogenesis - Homo sapiens (human)\npath:hsa00020\tCitrate cycle (TCA cycle) - Homo sapiens (human)\n"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	pathways, err := c.ListPathways(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pathways) != 2 {
		t.Fatalf("got %d pathways, want 2", len(pathways))
	}
	if pathways[0].ID != "path:hsa00010" {
		t.Errorf("pathways[0].ID = %q, want path:hsa00010", pathways[0].ID)
	}
	if pathways[0].Name != "Glycolysis / Gluconeogenesis - Homo sapiens (human)" {
		t.Errorf("pathways[0].Name = %q", pathways[0].Name)
	}
}

func TestListCompounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/list/compound" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("cpd:C00001\tH2O; Water\ncpd:C00002\tATP; Adenosine 5'-triphosphate\n"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	compounds, err := c.ListCompounds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(compounds) != 2 {
		t.Fatalf("got %d compounds, want 2", len(compounds))
	}
	if compounds[0].ID != "cpd:C00001" {
		t.Errorf("compounds[0].ID = %q, want cpd:C00001", compounds[0].ID)
	}
	if compounds[0].Name != "H2O" {
		t.Errorf("compounds[0].Name = %q, want H2O", compounds[0].Name)
	}
}

func TestGetCompound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get/C00031" {
			http.NotFound(w, r)
			return
		}
		// Minimal KEGG flat-file for D-Glucose
		_, _ = w.Write([]byte("ENTRY       C00031                      Compound\nNAME        D-Glucose;\n            Grape sugar;\n            Dextrose\nFORMULA     C6H12O6\n///\n"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	cmp, err := c.GetCompound(context.Background(), "C00031")
	if err != nil {
		t.Fatal(err)
	}
	if cmp.ID != "C00031" {
		t.Errorf("ID = %q, want C00031", cmp.ID)
	}
	if cmp.Name != "D-Glucose" {
		t.Errorf("Name = %q, want D-Glucose", cmp.Name)
	}
}

func TestParseLines(t *testing.T) {
	body := []byte("C00031\tD-Glucose; Grape sugar\nC00002\tATP\n\n")
	rows := parseLines(body)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0][0] != "C00031" {
		t.Errorf("rows[0][0] = %q, want C00031", rows[0][0])
	}
	if rows[0][1] != "D-Glucose; Grape sugar" {
		t.Errorf("rows[0][1] = %q", rows[0][1])
	}
}
