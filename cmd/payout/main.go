// Command payout produces the distribution list for a solved campaign.
//
//	payout -db pool.db -campaign puzzle-71 -prize-btc 7.1
//
// It reads the ticket ledger, applies the campaign's split, and prints one line
// per payee with the address and the exact amount in satoshis. It signs nothing
// and broadcasts nothing: the output is a plan for a human to review and execute
// with their own wallet.
//
// That separation is deliberate. A tool that could move the prize by itself
// would be the single most valuable thing to compromise in the whole project,
// and it would have to hold a key to do it. This one holds nothing.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"

	"github.com/0xmvercosa/puzzlebtc/internal/payout"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// satoshisPerBTC is exact: Bitcoin amounts are integers of satoshi and every
// figure here stays integral. Parsing a BTC amount goes through big.Rat rather
// than float64, which cannot represent every satoshi value above 2^53.
const satoshisPerBTC = 100_000_000

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dbPath     = flag.String("db", "puzzlebtc.db", "coordinator database")
		campaignID = flag.String("campaign", "", "campaign id (required)")
		prizeBTC   = flag.String("prize-btc", "", "prize in BTC, e.g. 7.1 (required)")
		format     = flag.String("format", "text", "output: text, csv or json")
		finderID   = flag.String("finder", "", "override the finder; defaults to the recorded solution")
		finderPct  = flag.Int64("finder-pct", 50, "finder's share, percent")
		platPct    = flag.Int64("platform-pct", 30, "platform's share, percent")
		helperPct  = flag.Int64("helper-pct", 20, "share split across all tickets, percent")
	)
	flag.Parse()

	if *campaignID == "" || *prizeBTC == "" {
		flag.Usage()
		return fmt.Errorf("-campaign and -prize-btc are both required")
	}

	prizeSat, err := btcToSatoshis(*prizeBTC)
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	finder := *finderID
	solution, err := db.GetSolution(ctx, *campaignID)
	if err != nil {
		return err
	}
	switch {
	case solution != nil && finder == "":
		finder = solution.WorkerID
	case solution == nil && finder == "":
		return fmt.Errorf("campaign %q has no recorded solution; pass -finder to model a hypothetical payout", *campaignID)
	}

	holders, err := db.TicketHolders(ctx, *campaignID)
	if err != nil {
		return err
	}
	if len(holders) == 0 {
		return fmt.Errorf("campaign %q has no ticket holders", *campaignID)
	}

	ph := make([]payout.Holder, len(holders))
	blocksBy := map[string]int64{}
	for i, h := range holders {
		ph[i] = payout.Holder{WorkerID: h.WorkerID, Tickets: h.Weight}
		blocksBy[h.WorkerID] = h.Blocks
	}

	split := payout.Split{
		FinderPct:          *finderPct,
		PlatformPct:        *platPct,
		HelperPct:          *helperPct,
		FinderKeepsTickets: true,
	}
	dist, err := payout.Compute(prizeSat, split, finder, ph)
	if err != nil {
		return err
	}

	// The one invariant worth failing loudly on: every satoshi is accounted for.
	if got := dist.Total(); got.Cmp(prizeSat) != 0 {
		return fmt.Errorf("distribution totals %s satoshis but the prize is %s — refusing to emit a plan that loses money", got, prizeSat)
	}

	addrs, err := db.PayoutAddresses(ctx)
	if err != nil {
		return err
	}

	plan := buildPlan(*campaignID, prizeSat, finder, dist, addrs, blocksBy, solution)

	switch *format {
	case "text":
		return writeText(os.Stdout, plan)
	case "csv":
		return writeCSV(os.Stdout, plan)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	default:
		return fmt.Errorf("unknown -format %q", *format)
	}
}

// Line is one payment in the plan.
type Line struct {
	WorkerID  string `json:"worker_id"`
	Role      string `json:"role"` // finder, platform or helper
	Address   string `json:"address"`
	Satoshis  int64  `json:"satoshis"`
	BTC       string `json:"btc"`
	Blocks    int64  `json:"blocks,omitempty"`
	KeysSwept int64  `json:"keys_swept,omitempty"`
}

// Plan is the full distribution, ready for review.
type Plan struct {
	CampaignID   string   `json:"campaign_id"`
	PrizeSat     int64    `json:"prize_sat"`
	FinderID     string   `json:"finder_id"`
	SolutionKey  string   `json:"solution_private_key,omitempty"`
	Lines        []Line   `json:"lines"`
	TotalSat     int64    `json:"total_sat"`
	MissingAddrs []string `json:"missing_addresses,omitempty"`
}

func buildPlan(campaignID string, prizeSat *big.Int, finder string, d *payout.Distribution,
	addrs map[string]string, blocksBy map[string]int64, sol *store.Solution) *Plan {

	p := &Plan{
		CampaignID: campaignID,
		PrizeSat:   prizeSat.Int64(),
		FinderID:   finder,
	}
	if sol != nil {
		p.SolutionKey = sol.PrivateKey
	}

	add := func(worker, role string, sat *big.Int, blocks, keys int64) {
		if sat.Sign() == 0 {
			return
		}
		addr := addrs[worker]
		if addr == "" && role != "platform" {
			p.MissingAddrs = append(p.MissingAddrs, worker)
			addr = "MISSING"
		}
		p.Lines = append(p.Lines, Line{
			WorkerID: worker, Role: role, Address: addr,
			Satoshis: sat.Int64(), BTC: satsToBTC(sat),
			Blocks: blocks, KeysSwept: keys,
		})
		p.TotalSat += sat.Int64()
	}

	add(finder, "finder", d.FinderSat, blocksBy[finder], 0)
	add("platform", "platform", d.PlatformSat, 0, 0)
	for _, h := range d.Helpers {
		add(h.WorkerID, "helper", h.Satoshis, blocksBy[h.WorkerID], h.Tickets)
	}

	sort.SliceStable(p.MissingAddrs, func(i, j int) bool { return p.MissingAddrs[i] < p.MissingAddrs[j] })
	return p
}

func writeText(f *os.File, p *Plan) error {
	fmt.Fprintf(f, "Campanha    %s\n", p.CampaignID)
	fmt.Fprintf(f, "Premio      %s BTC (%d sat)\n", satsToBTC(big.NewInt(p.PrizeSat)), p.PrizeSat)
	fmt.Fprintf(f, "Encontrou   %s\n", p.FinderID)
	fmt.Fprintf(f, "Pagamentos  %d\n\n", len(p.Lines))

	fmt.Fprintf(f, "%-20s %-9s %-38s %16s %14s\n", "PARTICIPANTE", "PAPEL", "ENDERECO", "SATOSHIS", "BTC")
	for _, l := range p.Lines {
		fmt.Fprintf(f, "%-20s %-9s %-38s %16d %14s\n", trunc(l.WorkerID, 20), l.Role, l.Address, l.Satoshis, l.BTC)
	}
	fmt.Fprintf(f, "\n%-70s %16d\n", "TOTAL", p.TotalSat)

	if len(p.MissingAddrs) > 0 {
		fmt.Fprintf(f, "\nATENCAO: %d participante(s) sem endereco de pagamento cadastrado:\n", len(p.MissingAddrs))
		for _, w := range p.MissingAddrs {
			fmt.Fprintf(f, "  %s\n", w)
		}
		fmt.Fprintf(f, "Resolva antes de pagar. As linhas acima marcadas MISSING nao podem ser executadas.\n")
	}
	return nil
}

func writeCSV(f *os.File, p *Plan) error {
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"worker_id", "role", "address", "satoshis", "btc", "blocks"}); err != nil {
		return err
	}
	for _, l := range p.Lines {
		if err := w.Write([]string{
			l.WorkerID, l.Role, l.Address,
			strconv.FormatInt(l.Satoshis, 10), l.BTC,
			strconv.FormatInt(l.Blocks, 10),
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

// btcToSatoshis parses a decimal BTC string exactly, via big.Rat. A float64
// cannot represent every satoshi value above 2^53, and rounding the prize is
// the one error nobody would forgive.
func btcToSatoshis(s string) (*big.Int, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("cannot parse %q as a BTC amount", s)
	}
	r.Mul(r, new(big.Rat).SetInt64(satoshisPerBTC))
	if !r.IsInt() {
		return nil, fmt.Errorf("%s BTC is not a whole number of satoshis", s)
	}
	if r.Sign() < 0 {
		return nil, fmt.Errorf("prize must not be negative")
	}
	return r.Num(), nil
}

func satsToBTC(sat *big.Int) string {
	q, r := new(big.Int).QuoRem(sat, big.NewInt(satoshisPerBTC), new(big.Int))
	return fmt.Sprintf("%s.%08d", q, r)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
