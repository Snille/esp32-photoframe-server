package service

import (
	"errors"
	"sync"
	"time"
	"unicode/utf8"
)

// Wi-Fi network change, pushed to a frame once over config-sync.
//
// The firmware (apply_config_from_json) reads wifi_ssid + wifi_password from the
// "config" object of X-Config-Payload, tests the new network, and goes back to
// the old one if it cannot connect. The server only has to hand those two
// fields over exactly once.
//
// The password is held in memory only — never in the database, never in a log —
// and is dropped the moment it is handed to the frame (Take). A server restart
// drops a change that was not sent yet; the user just asks again.

// WifiChangeState is where a requested network change stands.
type WifiChangeState string

const (
	WifiChangePending  WifiChangeState = "pending"  // waiting for the frame's next pull
	WifiChangeSent     WifiChangeState = "sent"     // handed to the frame, result not known yet
	WifiChangeMoved    WifiChangeState = "moved"    // frame reported the new SSID
	WifiChangeReverted WifiChangeState = "reverted" // frame reported another SSID: it went back
	WifiChangeExpired  WifiChangeState = "expired"  // frame never pulled before the TTL ran out
)

// WifiChangePendingTTL bounds how long a password waits in memory for a frame
// that does not pull (asleep for the night, off the network).
const WifiChangePendingTTL = 48 * time.Hour

// WifiChangeStatus is the password-free view of a change, safe to return to
// the web UI.
type WifiChangeStatus struct {
	State        WifiChangeState `json:"state"`
	TargetSSID   string          `json:"target_ssid"`
	PreviousSSID string          `json:"previous_ssid"`
	ReportedSSID string          `json:"reported_ssid,omitempty"`
	RequestedAt  time.Time       `json:"requested_at"`
	SentAt       *time.Time      `json:"sent_at,omitempty"`
	ResolvedAt   *time.Time      `json:"resolved_at,omitempty"`
}

type wifiChange struct {
	status   WifiChangeStatus
	password string // cleared by Take; only ever non-empty while pending
}

// WifiChangeStore keeps one change per device.
type WifiChangeStore struct {
	mu      sync.Mutex
	changes map[uint]*wifiChange
	now     func() time.Time
}

func NewWifiChangeStore() *WifiChangeStore {
	return &WifiChangeStore{changes: map[uint]*wifiChange{}, now: time.Now}
}

var (
	ErrWifiSSIDInvalid     = errors.New("SSID must be 1-32 bytes of valid UTF-8")
	ErrWifiPasswordInvalid = errors.New("password must be empty (keep the current one) or 8-63 characters")
)

// ValidateWifiChange checks the fields the way the frame stores them: an
// 802.11 SSID is at most 32 bytes, a WPA passphrase 8-63 characters. An empty
// password tells the frame to keep its current one.
func ValidateWifiChange(ssid, password string) error {
	if ssid == "" || len(ssid) > maxSSIDBytes || !utf8.ValidString(ssid) {
		return ErrWifiSSIDInvalid
	}
	if password != "" {
		n := len(password)
		if n < 8 || n > 63 || !utf8.ValidString(password) {
			return ErrWifiPasswordInvalid
		}
	}
	return nil
}

// Request stores a change for the frame's next pull, replacing any earlier one.
func (s *WifiChangeStore) Request(deviceID uint, ssid, password, currentSSID string) (WifiChangeStatus, error) {
	if err := ValidateWifiChange(ssid, password); err != nil {
		return WifiChangeStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := &wifiChange{
		status: WifiChangeStatus{
			State:        WifiChangePending,
			TargetSSID:   ssid,
			PreviousSSID: currentSSID,
			RequestedAt:  s.now(),
		},
		password: password,
	}
	s.changes[deviceID] = ch
	return ch.status, nil
}

// Cancel drops a change that was not sent yet. A sent change cannot be called
// back; Cancel then only clears the status line.
func (s *WifiChangeStore) Cancel(deviceID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.changes, deviceID)
}

// Status returns the current change, if any.
func (s *WifiChangeStore) Status(deviceID uint) (WifiChangeStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.changes[deviceID]
	if ch == nil {
		return WifiChangeStatus{}, false
	}
	s.expireLocked(ch)
	return ch.status, true
}

// Take hands a pending change over for sending and clears the password, so it
// goes out exactly once. ok is false when nothing is pending.
func (s *WifiChangeStore) Take(deviceID uint) (ssid, password string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.changes[deviceID]
	if ch == nil {
		return "", "", false
	}
	s.expireLocked(ch)
	if ch.status.State != WifiChangePending {
		return "", "", false
	}
	ssid, password = ch.status.TargetSSID, ch.password
	ch.password = ""
	t := s.now()
	ch.status.State = WifiChangeSent
	ch.status.SentAt = &t
	return ssid, password, true
}

// Observe records the SSID a frame reported on a pull. Call it BEFORE Take on
// the same pull: that pull's header still shows the network the frame was on
// when it asked, so only pulls after the send can tell the result. An empty
// SSID (firmware older than 2.22.0) leaves the result unknown.
func (s *WifiChangeStore) Observe(deviceID uint, reportedSSID string) {
	if reportedSSID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.changes[deviceID]
	if ch == nil || ch.status.State != WifiChangeSent {
		return
	}
	t := s.now()
	ch.status.ReportedSSID = reportedSSID
	ch.status.ResolvedAt = &t
	if reportedSSID == ch.status.TargetSSID {
		ch.status.State = WifiChangeMoved
	} else {
		ch.status.State = WifiChangeReverted
	}
}

// expireLocked drops the password of a change that waited too long.
func (s *WifiChangeStore) expireLocked(ch *wifiChange) {
	if ch.status.State == WifiChangePending && s.now().Sub(ch.status.RequestedAt) > WifiChangePendingTTL {
		ch.password = ""
		ch.status.State = WifiChangeExpired
	}
}
