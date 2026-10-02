// Package promise implements delivery-promise quoting: given where an order is
// going, how fast the customer wants it, and when it was placed, decide which
// distribution center (DC) should ship it, when it will ship, when it will
// arrive, and what it will cost.
//
// The fulfillment network (DCs, lanes, service levels, carrier holidays) is
// data, not code. It is loaded from JSON so operations can change a cutoff time
// or add a lane with a config rollout instead of a rebuild.
package promise

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
	// Embed the IANA tz database: the runtime image is distroless and has no
	// /usr/share/zoneinfo, and DC cutoffs are defined in local time.
	_ "time/tzdata"
)

const dateLayout = "2006-01-02"

// DC is a distribution center that can ship orders.
type DC struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	// CutoffHour is the local hour (0-23) after which an order misses today's
	// carrier pickup and ships the next business day.
	CutoffHour int `json:"cutoff_hour"`

	loc *time.Location
}

// ServiceLevel prices a shipping speed. Ground uses per-lane transit times;
// expedited levels promise a fixed number of business days from any DC.
type ServiceLevel struct {
	BaseUSD          float64 `json:"base_usd"`
	PerLbUSD         float64 `json:"per_lb_usd"`
	FixedTransitDays int     `json:"fixed_transit_days,omitempty"`
}

// Lane is a DC -> destination region pairing.
type Lane struct {
	DC         string `json:"dc"`
	Region     string `json:"region"`
	Zone       int    `json:"zone"`
	GroundDays int    `json:"ground_days"`
}

// Network is the full fulfillment network definition.
type Network struct {
	Holidays      []string                `json:"holidays"`
	DCs           []DC                    `json:"distribution_centers"`
	ServiceLevels map[string]ServiceLevel `json:"service_levels"`
	Lanes         []Lane                  `json:"lanes"`

	holidays map[string]bool
	dcs      map[string]*DC
	lanes    map[string]map[string]Lane // region -> dc code -> lane
}

// Load reads and validates a network definition from a file.
func Load(path string) (*Network, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes and validates a network definition. Unknown fields are
// rejected so a typo in a config rollout fails the pipeline instead of being
// silently ignored in production.
func Parse(data []byte) (*Network, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var n Network
	if err := dec.Decode(&n); err != nil {
		return nil, fmt.Errorf("decode network: %w", err)
	}
	if err := n.index(); err != nil {
		return nil, err
	}
	return &n, nil
}

func (n *Network) index() error {
	if len(n.DCs) == 0 {
		return fmt.Errorf("network has no distribution_centers")
	}
	if len(n.ServiceLevels) == 0 {
		return fmt.Errorf("network has no service_levels")
	}

	n.holidays = make(map[string]bool, len(n.Holidays))
	for _, h := range n.Holidays {
		if _, err := time.Parse(dateLayout, h); err != nil {
			return fmt.Errorf("holiday %q: want YYYY-MM-DD", h)
		}
		n.holidays[h] = true
	}

	n.dcs = make(map[string]*DC, len(n.DCs))
	for i := range n.DCs {
		dc := &n.DCs[i]
		if dc.Code == "" {
			return fmt.Errorf("distribution_centers[%d]: code is required", i)
		}
		if _, dup := n.dcs[dc.Code]; dup {
			return fmt.Errorf("dc %s: duplicate code", dc.Code)
		}
		if dc.CutoffHour < 0 || dc.CutoffHour > 23 {
			return fmt.Errorf("dc %s: cutoff_hour %d out of range 0-23", dc.Code, dc.CutoffHour)
		}
		loc, err := time.LoadLocation(dc.Timezone)
		if err != nil {
			return fmt.Errorf("dc %s: timezone %q: %w", dc.Code, dc.Timezone, err)
		}
		dc.loc = loc
		n.dcs[dc.Code] = dc
	}

	for name, sl := range n.ServiceLevels {
		if sl.BaseUSD < 0 || sl.PerLbUSD < 0 {
			return fmt.Errorf("service level %s: negative price", name)
		}
		if sl.FixedTransitDays < 0 {
			return fmt.Errorf("service level %s: negative fixed_transit_days", name)
		}
	}

	n.lanes = make(map[string]map[string]Lane)
	for i, l := range n.Lanes {
		if _, ok := n.dcs[l.DC]; !ok {
			return fmt.Errorf("lanes[%d]: unknown dc %q", i, l.DC)
		}
		if l.Region == "" {
			return fmt.Errorf("lanes[%d]: region is required", i)
		}
		if l.Zone < 1 || l.Zone > 8 {
			return fmt.Errorf("lanes[%d] %s->%s: zone %d out of range 1-8", i, l.DC, l.Region, l.Zone)
		}
		if l.GroundDays < 1 || l.GroundDays > 10 {
			return fmt.Errorf("lanes[%d] %s->%s: ground_days %d out of range 1-10", i, l.DC, l.Region, l.GroundDays)
		}
		if n.lanes[l.Region] == nil {
			n.lanes[l.Region] = make(map[string]Lane)
		}
		if _, dup := n.lanes[l.Region][l.DC]; dup {
			return fmt.Errorf("lanes[%d]: duplicate lane %s->%s", i, l.DC, l.Region)
		}
		n.lanes[l.Region][l.DC] = l
	}
	return nil
}

// Regions returns the destination regions served by at least one lane.
func (n *Network) Regions() []string {
	out := make([]string, 0, len(n.lanes))
	for r := range n.lanes {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// ServiceLevelNames returns the configured service levels, sorted.
func (n *Network) ServiceLevelNames() []string {
	out := make([]string, 0, len(n.ServiceLevels))
	for s := range n.ServiceLevels {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
