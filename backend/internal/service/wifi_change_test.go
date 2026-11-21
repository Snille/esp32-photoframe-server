package service

import (
	"strings"
	"testing"
	"time"
)

func TestWifiChangeTakeIsOneShot(t *testing.T) {
	s := NewWifiChangeStore()
	if _, err := s.Request(7, "Guest", "secret-pass", "IOT"); err != nil {
		t.Fatal(err)
	}
	ssid, pw, ok := s.Take(7)
	if !ok || ssid != "Guest" || pw != "secret-pass" {
		t.Fatalf("first Take = %q %q %v", ssid, pw, ok)
	}
	if _, _, ok := s.Take(7); ok {
		t.Fatal("second Take must return nothing: the change goes out once")
	}
	// The password is gone from the store after it was handed over.
	if s.changes[7].password != "" {
		t.Fatal("password still held after Take")
	}
	st, _ := s.Status(7)
	if st.State != WifiChangeSent || st.SentAt == nil {
		t.Fatalf("state after Take = %+v", st)
	}
}

func TestWifiChangeObserveResult(t *testing.T) {
	cases := []struct {
		reported string
		want     WifiChangeState
	}{
		{"Guest", WifiChangeMoved},
		{"IOT", WifiChangeReverted},
		{"", WifiChangeSent}, // old firmware: no SSID header, result unknown
	}
	for _, tc := range cases {
		s := NewWifiChangeStore()
		s.Request(1, "Guest", "", "IOT")
		// A pull before the send only reports the old network: no verdict.
		s.Observe(1, "IOT")
		if st, _ := s.Status(1); st.State != WifiChangePending {
			t.Fatalf("Observe before Take changed state to %s", st.State)
		}
		s.Take(1)
		s.Observe(1, tc.reported)
		st, _ := s.Status(1)
		if st.State != tc.want {
			t.Errorf("reported %q: state %s, want %s", tc.reported, st.State, tc.want)
		}
	}
}

func TestWifiChangeExpiryDropsPassword(t *testing.T) {
	s := NewWifiChangeStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	s.Request(2, "Guest", "secret-pass", "IOT")
	now = now.Add(WifiChangePendingTTL + time.Minute)
	if _, _, ok := s.Take(2); ok {
		t.Fatal("expired change was still sent")
	}
	if s.changes[2].password != "" {
		t.Fatal("expired change still holds the password")
	}
	if st, _ := s.Status(2); st.State != WifiChangeExpired {
		t.Fatalf("state = %s, want expired", st.State)
	}
}

func TestWifiChangeCancelAndReplace(t *testing.T) {
	s := NewWifiChangeStore()
	s.Request(3, "A", "password1", "IOT")
	s.Request(3, "B", "password2", "IOT")
	ssid, pw, _ := s.Take(3)
	if ssid != "B" || pw != "password2" {
		t.Fatalf("a new request must replace the old one, got %q %q", ssid, pw)
	}
	s.Request(4, "A", "password1", "IOT")
	s.Cancel(4)
	if _, _, ok := s.Take(4); ok {
		t.Fatal("cancelled change was sent")
	}
}

func TestValidateWifiChange(t *testing.T) {
	ok := [][2]string{{"Guest", ""}, {"Guest", "12345678"}, {strings.Repeat("x", 32), strings.Repeat("p", 63)}, {"Kök", "lösenord"}}
	for _, c := range ok {
		if err := ValidateWifiChange(c[0], c[1]); err != nil {
			t.Errorf("%q/%q rejected: %v", c[0], c[1], err)
		}
	}
	bad := [][2]string{{"", "12345678"}, {strings.Repeat("x", 33), ""}, {"Guest", "short"}, {"Guest", strings.Repeat("p", 64)}, {"\xff", ""}}
	for _, c := range bad {
		if err := ValidateWifiChange(c[0], c[1]); err == nil {
			t.Errorf("%q/%q accepted", c[0], c[1])
		}
	}
}
