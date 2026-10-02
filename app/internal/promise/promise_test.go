package promise

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T) *Network {
	t.Helper()
	n, err := Load("testdata/network.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return n
}

func at(t *testing.T, rfc3339 string) *time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatal(err)
	}
	return &ts
}

func TestQuoteShipAndDeliveryDates(t *testing.T) {
	n := loadFixture(t)
	tests := []struct {
		name         string
		req          Request
		wantDC       string
		wantShip     string
		wantDelivery string
	}{
		{
			name:   "before cutoff ships same day",
			req:    Request{Region: "south", ServiceLevel: "ground", WeightLb: 4, OrderTime: at(t, "2026-09-29T15:00:00Z")}, // Tue 10:00 CDT
			wantDC: "DAL", wantShip: "2026-09-29", wantDelivery: "2026-09-30",
		},
		{
			name:   "after cutoff ships next business day",
			req:    Request{Region: "south", ServiceLevel: "ground", WeightLb: 4, OrderTime: at(t, "2026-09-29T22:00:00Z")}, // Tue 17:00 CDT
			wantDC: "DAL", wantShip: "2026-09-30", wantDelivery: "2026-10-01",
		},
		{
			name:   "friday after cutoff rolls over the weekend",
			req:    Request{Region: "south", ServiceLevel: "ground", WeightLb: 4, DC: "DAL", OrderTime: at(t, "2026-10-02T22:00:00Z")}, // Fri 17:00 CDT
			wantDC: "DAL", wantShip: "2026-10-05", wantDelivery: "2026-10-06",
		},
		{
			name:   "weekend order ships monday",
			req:    Request{Region: "south", ServiceLevel: "ground", WeightLb: 4, DC: "DAL", OrderTime: at(t, "2026-10-03T14:00:00Z")}, // Sat 09:00 CDT
			wantDC: "DAL", wantShip: "2026-10-05", wantDelivery: "2026-10-06",
		},
		{
			name:   "carrier holiday is skipped for pickup",
			req:    Request{Region: "south", ServiceLevel: "ground", WeightLb: 4, DC: "DAL", OrderTime: at(t, "2026-09-04T22:00:00Z")}, // Fri before Labor Day, after cutoff
			wantDC: "DAL", wantShip: "2026-09-08", wantDelivery: "2026-09-09",
		},
		{
			name:   "carrier holiday is skipped in transit",
			req:    Request{Region: "southeast", ServiceLevel: "ground", WeightLb: 4, DC: "DAL", OrderTime: at(t, "2026-09-04T15:00:00Z")}, // Fri 10:00 CDT, 2-day lane
			wantDC: "DAL", wantShip: "2026-09-04", wantDelivery: "2026-09-09",
		},
		{
			name:   "tie on delivery date goes to the cheaper DC",
			req:    Request{Region: "midwest", ServiceLevel: "ground", WeightLb: 10, OrderTime: at(t, "2026-09-29T14:00:00Z")},
			wantDC: "DAL", wantShip: "2026-09-29", wantDelivery: "2026-10-01",
		},
		{
			name:   "expedited uses fixed transit from the cheapest zone",
			req:    Request{Region: "northeast", ServiceLevel: "overnight", WeightLb: 2, OrderTime: at(t, "2026-09-29T14:00:00Z")},
			wantDC: "ATL", wantShip: "2026-09-29", wantDelivery: "2026-09-30",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := n.Quote(tt.req, time.Time{})
			if err != nil {
				t.Fatalf("Quote: %v", err)
			}
			got := q.Selected
			if got.DC != tt.wantDC || got.ShipDate != tt.wantShip || got.DeliveryDate != tt.wantDelivery {
				t.Errorf("got dc=%s ship=%s deliver=%s, want dc=%s ship=%s deliver=%s",
					got.DC, got.ShipDate, got.DeliveryDate, tt.wantDC, tt.wantShip, tt.wantDelivery)
			}
		})
	}
}

// 20:30Z is 13:30 in Reno (before its 14:00 cutoff), 15:30 in Dallas (before
// 16:00) and 16:30 in Atlanta (after 15:00). Same instant, different answers.
func TestCutoffIsEvaluatedInDCLocalTime(t *testing.T) {
	n := loadFixture(t)
	want := map[string]string{"RNO": "2026-09-29", "DAL": "2026-09-29", "ATL": "2026-09-30"}
	for dc, ship := range want {
		q, err := n.Quote(Request{Region: "midwest", ServiceLevel: "ground", WeightLb: 1, DC: dc, OrderTime: at(t, "2026-09-29T20:30:00Z")}, time.Time{})
		if err != nil {
			t.Fatalf("%s: %v", dc, err)
		}
		if q.Selected.ShipDate != ship {
			t.Errorf("%s ship date = %s, want %s", dc, q.Selected.ShipDate, ship)
		}
	}
}

func TestQuoteListsAlternativesInRankOrder(t *testing.T) {
	n := loadFixture(t)
	q, err := n.Quote(Request{Region: "midwest", ServiceLevel: "ground", WeightLb: 10, OrderTime: at(t, "2026-09-29T14:00:00Z")}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range q.Alternatives {
		got = append(got, o.DC)
	}
	if strings.Join(got, ",") != "ATL,RNO" {
		t.Errorf("alternatives = %v, want [ATL RNO]", got)
	}
}

func TestQuoteDefaultsOrderTimeToNow(t *testing.T) {
	n := loadFixture(t)
	now := *at(t, "2026-09-29T15:00:00Z")
	q, err := n.Quote(Request{Region: "south", ServiceLevel: "ground", WeightLb: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !q.OrderTime.Equal(now) {
		t.Errorf("order_time = %v, want %v", q.OrderTime, now)
	}
}

func TestCost(t *testing.T) {
	n := loadFixture(t)
	q, err := n.Quote(Request{Region: "south", ServiceLevel: "ground", WeightLb: 3.2, DC: "DAL", OrderTime: at(t, "2026-09-29T15:00:00Z")}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// (7.50 + 0.55 * ceil(3.2)) * (1 + 0.1 * (zone 2 - 1)) = 9.70 * 1.1
	if q.Selected.CostUSD != 10.67 {
		t.Errorf("cost = %v, want 10.67", q.Selected.CostUSD)
	}
}

func TestQuoteValidation(t *testing.T) {
	n := loadFixture(t)
	bad := map[string]Request{
		"unknown region":        {Region: "mars", ServiceLevel: "ground", WeightLb: 1},
		"unknown service level": {Region: "south", ServiceLevel: "teleport", WeightLb: 1},
		"zero weight":           {Region: "south", ServiceLevel: "ground", WeightLb: 0},
		"freight weight":        {Region: "south", ServiceLevel: "ground", WeightLb: MaxWeightLb + 1},
		"unknown dc":            {Region: "south", ServiceLevel: "ground", WeightLb: 1, DC: "XXX"},
	}
	for name, req := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := n.Quote(req, time.Now())
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
		})
	}
}

func TestParseRejectsBadNetworks(t *testing.T) {
	cases := map[string]string{
		"unknown field": `{"distribution_centers":[{"code":"A","timezone":"UTC","cutoff_hour":1,"cutof":2}],"service_levels":{"g":{}}}`,
		"bad timezone":  `{"distribution_centers":[{"code":"A","timezone":"Mars/Olympus","cutoff_hour":1}],"service_levels":{"g":{}}}`,
		"bad cutoff":    `{"distribution_centers":[{"code":"A","timezone":"UTC","cutoff_hour":24}],"service_levels":{"g":{}}}`,
		"lane to unknown dc": `{"distribution_centers":[{"code":"A","timezone":"UTC","cutoff_hour":1}],"service_levels":{"g":{}},
			"lanes":[{"dc":"B","region":"r","zone":1,"ground_days":1}]}`,
		"bad holiday": `{"holidays":["12/25/2026"],"distribution_centers":[{"code":"A","timezone":"UTC","cutoff_hour":1}],"service_levels":{"g":{}}}`,
		"no dcs":      `{"service_levels":{"g":{}}}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(doc)); err == nil {
				t.Fatal("Parse succeeded, want error")
			}
		})
	}
}
