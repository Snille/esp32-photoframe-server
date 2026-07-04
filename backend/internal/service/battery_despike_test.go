package service

import "testing"

func TestMedian(t *testing.T) {
	tests := []struct {
		name string
		vals []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{42}, 42},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{1, 2, 3, 4}, 2.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := median(tt.vals); got != tt.want {
				t.Errorf("median(%v) = %v, want %v", tt.vals, got, tt.want)
			}
		})
	}
}

func TestDespikeSoC_IsolatedSpikeRejected(t *testing.T) {
	// Steady ~80% with a single-wake collapse to ~9% (matches an observed
	// PhotoFrame-02 sample: 4030mV/~80% -> 3675mV/~9% -> 4034mV/~81%).
	socs := []float64{80, 81, 80, 9, 81, 80, 79}
	keep := despikeSoC(socs)
	for i, k := range keep {
		want := i != 3
		if k != want {
			t.Errorf("keep[%d] = %v, want %v (socs=%v)", i, k, want, socs)
		}
	}
}

func TestDespikeSoC_TwoInARowSpikeRejected(t *testing.T) {
	// Matches observed PhotoFrame-03 data: 88, 48, 44, 89 — two consecutive
	// sagged readings sandwiched by good ones.
	socs := []float64{88, 89, 88, 48, 44, 89, 88, 91, 88}
	keep := despikeSoC(socs)
	for i, k := range keep {
		want := i != 3 && i != 4
		if k != want {
			t.Errorf("keep[%d] = %v, want %v (socs=%v)", i, k, want, socs)
		}
	}
}

func TestDespikeSoC_ConfirmedRechargeNotRejected(t *testing.T) {
	// A genuine recharge: SoC jumps from ~20% to ~90% and STAYS there across
	// several subsequent readings (each one confirms the last, unlike a spike
	// which is contradicted by its neighbors). None of this should be flagged.
	socs := []float64{22, 20, 21, 90, 91, 90, 89, 90}
	keep := despikeSoC(socs)
	for i, k := range keep {
		if !k {
			t.Errorf("keep[%d] = false, want true (genuine recharge, socs=%v)", i, socs)
		}
	}
}

func TestDespikeSoC_LatestReadingJudgedAgainstHistoryOnly(t *testing.T) {
	// The most recent entry has no "future" sample to confirm or refute it —
	// a lone low reading right after a steady run is (correctly) distrusted
	// until a follow-up reading either confirms or refutes it.
	socs := []float64{80, 81, 80, 79, 9}
	keep := despikeSoC(socs)
	if keep[len(keep)-1] {
		t.Errorf("keep[last] = true, want false (unconfirmed drop): socs=%v", socs)
	}
	for i := 0; i < len(socs)-1; i++ {
		if !keep[i] {
			t.Errorf("keep[%d] = false, want true (steady run): socs=%v", i, socs)
		}
	}
}

func TestDespikeSoC_RealWorldPhotoFrame03Sample(t *testing.T) {
	// Replayed from the actual battery_samples history that prompted this fix
	// (voltage_mv converted to the same SoC basis via the lipoCurve), oldest
	// first: a device oscillating between a healthy ~88-92% and implausible
	// dips to 11-66%, sometimes two dips in a row.
	mv := []int{
		3882, 3898, 3878, 3749, 3802, 4136, 4118, 3422, 4127,
		3722, 3883, 3897, 3406, 4094, 4126, 4094, 4102, 3703, 3740, 4099,
	}
	socs := make([]float64, len(mv))
	for i, v := range mv {
		socs[i] = voltageToSoC(v)
	}
	keep := despikeSoC(socs)
	kept := 0
	for _, k := range keep {
		if k {
			kept++
		}
	}
	// The healthy baseline should dominate what's kept; the isolated/paired
	// dips (a clear minority of samples here) should mostly be rejected.
	if kept < len(socs)/2 {
		t.Fatalf("only %d/%d samples kept — despike is too aggressive: socs=%v keep=%v", kept, len(socs), socs, keep)
	}
	// The worst dip (3406 mV, ~2% SoC — the exact kind of reading the user
	// saw) must not survive: its neighbors are both a healthy ~88-92%.
	for i, v := range mv {
		if v == 3406 && keep[i] {
			t.Errorf("3406 mV (index %d) kept as trustworthy — expected it to be despiked", i)
		}
	}
}
