// Command driver puts bad object-provider candidates in node1's way and
// records what node1 does: a hanging finder H, a finder F that claims a silent
// reader G twice, a dead app D, an unknown identity R and the real holder
// node2 twice; and a liar L that puts node2's identity on its descriptor and
// search result.
//
//	driver drive  <session.json> <o1>   records facts as JSON; stops only on a wiring fault
//	driver verify <session.json> <o1>   the same flow, with fresh apps, every check binding
//
// o1 is a raw blob stored on node2 alone.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// The bounds the checks hold node1 to. G and H each hold their query until a
// 15 s timer of node1 ends it (op_read.go, the first-byte timer; module.go,
// the external discoverer timeout), so a bound under 15 s is met only by a
// concurrent winner, and one over it only by a node that does end the wait.
const (
	readO1Bound     = 10 * time.Second
	findBound       = 25 * time.Second
	readO2HangBound = 25 * time.Second
	readO2Bound     = 8 * time.Second
	// holdBound is how long node1 may hold a silent candidate open: its 15 s
	// first-byte timer, and slack.
	holdBound = 20 * time.Second
	// readO1Attempts bounds the reads of O1 run until one has G in play.
	readO1Attempts = 3
)

// lease is every registration of the flow. The User revokes every grant at
// the end, so node1 drops the registrations on its next fan-out.
const lease = astral.Duration(60 * time.Second)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json> <o1>")
		os.Exit(2)
	}
	f := &facts{checks: provider.NewChecks(os.Args[1])}
	f.Checks = f.checks.Held
	err := run(os.Args[2], os.Args[3], f)
	if os.Args[1] == "drive" {
		provider.Emit("objects_provider_read_robustness", f)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// call is one timed call: how long it took, and how it failed.
type call struct {
	Seconds  float64 `json:"seconds"`
	Err      string  `json:"err"`
	Rejected bool    `json:"rejected"`
	Code     int     `json:"code"` // the reject code, 0 when not rejected
}

// facts is what the driver records.
type facts struct {
	O1    string            `json:"o1"`
	O2    string            `json:"o2"`
	X     string            `json:"x"`
	Label string            `json:"label"`
	Word  string            `json:"word"`
	Names map[string]string `json:"names"` // identity -> role name in the flow

	FindA       []string `json:"find_a"` // identities find(O1) named, in order
	FindACall   call     `json:"find_a_call"`
	ReadO1      call     `json:"read_o1"`
	ReadO1Bytes int      `json:"read_o1_bytes"`
	ReadO1IsO1  bool     `json:"read_o1_is_o1"` // the bytes hash to O1
	// how many reads of O1 ran before one had G in play; the read_o1 fields are that read's
	ReadO1Attempts int  `json:"read_o1_attempts"`
	ReadO2A        call `json:"read_o2_a"`
	ReadO2B        call `json:"read_o2_b"`

	// counters of the apps, by op and object
	FFindO1  int `json:"f_find_o1"`
	HFindO1  int `json:"h_find_o1"`
	GReadO1  int `json:"g_read_o1"`
	GReadO2  int `json:"g_read_o2"`   // after phase A
	GReadO2B int `json:"g_read_o2_b"` // after phase B

	// how long G held each read before node1 closed it, -1 when node1 never did
	GHoldO1 float64 `json:"g_hold_o1_s"`
	GHoldO2 float64 `json:"g_hold_o2_s"`

	DescribeSources []string `json:"describe_sources"` // SourceID of each descriptor carrying the label
	DescribeErr     string   `json:"describe_err"`
	SearchSources   []string `json:"search_sources"` // SourceID of each result for X
	SearchErr       string   `json:"search_err"`

	Checks map[string]bool `json:"checks"`
	checks *provider.Checks
}

func run(path, o1 string, f *facts) error {
	w, err := setUp(path, o1, f)
	if w != nil {
		defer w.close()
	}
	if err != nil {
		return err
	}
	if err := phaseA(w, f); err != nil {
		return err
	}
	if err := phaseB(w, f); err != nil {
		return err
	}
	return liar(w, f)
}
