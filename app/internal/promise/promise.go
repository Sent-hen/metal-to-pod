package promise

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// MaxWeightLb is the parcel-network weight limit; heavier shipments go LTL
// freight, which this service does not quote.
const MaxWeightLb = 150

// Request asks for a delivery promise.
type Request struct {
	Region       string  `json:"region"`
	ServiceLevel string  `json:"service_level"`
	WeightLb     float64 `json:"weight_lb"`
	// DC pins the ship-from location. Empty means "pick the best DC".
	DC string `json:"dc,omitempty"`
	// OrderTime defaults to now. Useful for back-testing promises.
	OrderTime *time.Time `json:"order_time,omitempty"`
}

// Option is one feasible way to fulfill the order.
type Option struct {
	DC           string  `json:"dc"`
	ShipDate     string  `json:"ship_date"`
	DeliveryDate string  `json:"delivery_date"`
	TransitDays  int     `json:"transit_business_days"`
	Zone         int     `json:"zone"`
	CostUSD      float64 `json:"cost_usd"`

	delivery time.Time
}

// Quote is the promise returned to the caller: the selected option plus the
// alternatives that were considered, so the sourcing decision is explainable.
type Quote struct {
	Region       string    `json:"region"`
	ServiceLevel string    `json:"service_level"`
	WeightLb     float64   `json:"weight_lb"`
	OrderTime    time.Time `json:"order_time"`
	Selected     Option    `json:"selected"`
	Alternatives []Option  `json:"alternatives"`
}

// ValidationError marks a request the caller must fix (maps to HTTP 422).
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Quote computes a delivery promise. The DC selection rule is: earliest
// delivery date wins; ties go to the cheaper option; remaining ties go to the
// DC code so the answer is deterministic across replicas.
func (n *Network) Quote(req Request, now time.Time) (Quote, error) {
	sl, ok := n.ServiceLevels[req.ServiceLevel]
	if !ok {
		return Quote{}, invalid("unknown service_level %q (valid: %v)", req.ServiceLevel, n.ServiceLevelNames())
	}
	lanes, ok := n.lanes[req.Region]
	if !ok {
		return Quote{}, invalid("unknown region %q (valid: %v)", req.Region, n.Regions())
	}
	if req.WeightLb <= 0 || req.WeightLb > MaxWeightLb {
		return Quote{}, invalid("weight_lb must be > 0 and <= %d", MaxWeightLb)
	}
	if req.DC != "" {
		if _, known := n.dcs[req.DC]; !known {
			return Quote{}, invalid("unknown dc %q", req.DC)
		}
		if _, served := lanes[req.DC]; !served {
			return Quote{}, invalid("dc %s has no lane to region %s", req.DC, req.Region)
		}
	}

	orderTime := now
	if req.OrderTime != nil {
		orderTime = *req.OrderTime
	}

	var options []Option
	for code, lane := range lanes {
		if req.DC != "" && code != req.DC {
			continue
		}
		options = append(options, n.option(n.dcs[code], lane, sl, req.WeightLb, orderTime))
	}

	sort.Slice(options, func(i, j int) bool {
		a, b := options[i], options[j]
		if !a.delivery.Equal(b.delivery) {
			return a.delivery.Before(b.delivery)
		}
		if a.CostUSD != b.CostUSD {
			return a.CostUSD < b.CostUSD
		}
		return a.DC < b.DC
	})

	return Quote{
		Region:       req.Region,
		ServiceLevel: req.ServiceLevel,
		WeightLb:     req.WeightLb,
		OrderTime:    orderTime.UTC(),
		Selected:     options[0],
		Alternatives: options[1:],
	}, nil
}

func (n *Network) option(dc *DC, lane Lane, sl ServiceLevel, weight float64, orderTime time.Time) Option {
	ship := n.shipDate(orderTime.In(dc.loc), dc.CutoffHour)
	transit := lane.GroundDays
	if sl.FixedTransitDays > 0 {
		transit = sl.FixedTransitDays
	}
	delivery := n.addBusinessDays(ship, transit)
	return Option{
		DC:           dc.Code,
		ShipDate:     ship.Format(dateLayout),
		DeliveryDate: delivery.Format(dateLayout),
		TransitDays:  transit,
		Zone:         lane.Zone,
		CostUSD:      cost(sl, lane.Zone, weight),
		delivery:     delivery,
	}
}

// cost applies a simple zone-based rate card: each zone beyond 1 adds 10%.
func cost(sl ServiceLevel, zone int, weight float64) float64 {
	raw := (sl.BaseUSD + sl.PerLbUSD*math.Ceil(weight)) * (1 + 0.1*float64(zone-1))
	return math.Round(raw*100) / 100
}

// shipDate returns the carrier pickup date for an order placed at local time t
// at a DC with the given cutoff hour.
func (n *Network) shipDate(local time.Time, cutoffHour int) time.Time {
	d := civilDate(local)
	if !n.isBusinessDay(d) || local.Hour() >= cutoffHour {
		return n.nextBusinessDay(d)
	}
	return d
}

func (n *Network) addBusinessDays(d time.Time, days int) time.Time {
	for i := 0; i < days; i++ {
		d = n.nextBusinessDay(d)
	}
	return d
}

func (n *Network) nextBusinessDay(d time.Time) time.Time {
	d = d.AddDate(0, 0, 1)
	for !n.isBusinessDay(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func (n *Network) isBusinessDay(d time.Time) bool {
	switch d.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	return !n.holidays[d.Format(dateLayout)]
}

// civilDate drops the clock and zone, keeping the local calendar date. Date
// arithmetic is then done in UTC where every day is exactly 24h (no DST).
func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
