package kegg

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes KEGG as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/kegg-cli/kegg"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// kegg:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone kegg binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the KEGG driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "kegg",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "kegg",
			Short:  "A command line for the KEGG REST API.",
			Long: `A command line for the KEGG REST API.

kegg reads public KEGG data over plain HTTPS, shapes it into clean records,
and prints output that pipes into the rest of your tools. No API key required
for academic and research use.

KEGG covers 19k compounds, 586 pathways, 67M gene entries across 11k genomes,
and 12k drugs.`,
			Site: Host,
			Repo: "https://github.com/tamnd/kegg-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search: /find/<db>/<query> — generic search across databases
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		Summary: "Search KEGG entries by name or keyword",
		Args:    []kit.Arg{{Name: "query", Help: "search term"}},
	}, searchEntries)

	// pathways: /list/pathway/hsa — list human pathways
	kit.Handle(app, kit.OpMeta{
		Name:    "pathways",
		Group:   "read",
		List:    true,
		Summary: "List human pathways (hsa)",
	}, listPathways)

	// compounds: list or search compounds
	kit.Handle(app, kit.OpMeta{
		Name:    "compounds",
		Group:   "read",
		List:    true,
		Summary: "List or search KEGG compounds",
		Args:    []kit.Arg{{Name: "query", Help: "search term (omit to list all)", Optional: true}},
	}, listCompounds)

	// compound: fetch a single compound by id or name
	kit.Handle(app, kit.OpMeta{
		Name:    "compound",
		Group:   "read",
		Single:  true,
		Summary: "Fetch a compound by KEGG id or name",
		URIType: "compound",
		Resolver: true,
		Args:    []kit.Arg{{Name: "ref", Help: "compound id (e.g. C00031) or name (e.g. glucose)"}},
	}, getCompound)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type searchIn struct {
	Query  string  `kit:"arg" help:"search term"`
	DB     string  `kit:"flag" help:"database to search (compound, drug, pathway, genes, disease)" default:"compound"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type pathwaysIn struct {
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type compoundsIn struct {
	Query  string  `kit:"arg" help:"search term (omit to list all)"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type compoundIn struct {
	Ref    string  `kit:"arg" help:"compound id (e.g. C00031) or name (e.g. glucose)"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func searchEntries(ctx context.Context, in searchIn, emit func(*Entry) error) error {
	db := in.DB
	if db == "" {
		db = "compound"
	}
	entries, err := in.Client.FindEntries(ctx, db, in.Query)
	if err != nil {
		return mapErr(err)
	}
	limit := in.Limit
	for i, e := range entries {
		if limit > 0 && i >= limit {
			break
		}
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

func listPathways(ctx context.Context, in pathwaysIn, emit func(*Pathway) error) error {
	pathways, err := in.Client.ListPathways(ctx)
	if err != nil {
		return mapErr(err)
	}
	limit := in.Limit
	for i, p := range pathways {
		if limit > 0 && i >= limit {
			break
		}
		if err := emit(p); err != nil {
			return err
		}
	}
	return nil
}

func listCompounds(ctx context.Context, in compoundsIn, emit func(*Compound) error) error {
	var compounds []*Compound
	var err error
	if in.Query != "" {
		entries, ferr := in.Client.FindEntries(ctx, "compound", in.Query)
		if ferr != nil {
			return mapErr(ferr)
		}
		for _, e := range entries {
			name := ""
			if len(e.Names) > 0 {
				name = e.Names[0]
			}
			compounds = append(compounds, &Compound{ID: e.ID, Name: name})
		}
	} else {
		compounds, err = in.Client.ListCompounds(ctx)
		if err != nil {
			return mapErr(err)
		}
	}
	limit := in.Limit
	for i, c := range compounds {
		if limit > 0 && i >= limit {
			break
		}
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

var compoundIDRE = regexp.MustCompile(`^C\d{5}$`)

func getCompound(ctx context.Context, in compoundIn, emit func(*Compound) error) error {
	ref := strings.TrimSpace(in.Ref)
	var cmp *Compound
	if compoundIDRE.MatchString(ref) {
		// looks like a KEGG compound id — fetch directly
		c, err := in.Client.GetCompound(ctx, ref)
		if err != nil {
			return mapErr(err)
		}
		cmp = c
	} else {
		// treat as a name search; use the first result
		entries, err := in.Client.FindEntries(ctx, "compound", ref)
		if err != nil {
			return mapErr(err)
		}
		if len(entries) == 0 {
			return errs.NotFound("no compound found for %q", ref)
		}
		e := entries[0]
		name := ""
		if len(e.Names) > 0 {
			name = e.Names[0]
		}
		cmp = &Compound{ID: e.ID, Name: name}
	}
	return emit(cmp)
}

// --- Resolver: pure string functions, no network ---

// Classify turns a compound id or KEGG URL into (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	// strip URL prefix if needed
	if strings.Contains(input, "kegg.jp/entry/") {
		parts := strings.SplitN(input, "/entry/", 2)
		input = parts[1]
	}
	input = strings.Trim(input, "/")
	if input == "" {
		return "", "", errs.Usage("unrecognized KEGG reference: %q", input)
	}
	if compoundIDRE.MatchString(input) {
		return "compound", input, nil
	}
	return "", "", errs.Usage("unrecognized KEGG reference: %q", fmt.Sprintf("C##### expected, got %q", input))
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "compound":
		return "https://www.kegg.jp/entry/" + id, nil
	default:
		return "", errs.Usage("kegg has no resource type %q", uriType)
	}
}

// mapErr converts a library error into the kit error kind that carries the
// right exit code.
func mapErr(err error) error {
	return err
}
