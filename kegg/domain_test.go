package kegg

import (
	"testing"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the domain wiring (Info, Classify, Locate). Client HTTP behaviour is
// covered in kegg_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "kegg" {
		t.Errorf("Scheme = %q, want kegg", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "kegg" {
		t.Errorf("Identity.Binary = %q, want kegg", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in      string
		wantTyp string
		wantID  string
		wantErr bool
	}{
		{"C00031", "compound", "C00031", false},
		{"C00001", "compound", "C00001", false},
		{"https://www.kegg.jp/entry/C00031", "compound", "C00031", false},
		{"glucose", "", "", true},   // name without C##### prefix → error
		{"", "", "", true},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("Classify(%q): want error, got (%q, %q, nil)", tc.in, typ, id)
			}
			continue
		}
		if err != nil || typ != tc.wantTyp || id != tc.wantID {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.wantTyp, tc.wantID)
		}
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("compound", "C00031")
	want := "https://www.kegg.jp/entry/C00031"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}

	_, err = Domain{}.Locate("page", "foo")
	if err == nil {
		t.Error("Locate(page,...): want error for unknown type")
	}
}

func TestCompoundIDRE(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"C00031", true},
		{"C00001", true},
		{"C99999", true},
		{"glucose", false},
		{"C0003", false},  // too short
		{"C000310", false}, // too long
		{"hsa:672", false},
		{"", false},
	}
	for _, tc := range cases {
		got := compoundIDRE.MatchString(tc.s)
		if got != tc.want {
			t.Errorf("compoundIDRE.MatchString(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}
