package core

import (
	"strconv"
	"testing"
	"time"
)

func TestStripeSignature(t *testing.T) {
	body := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Unix(1_800_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	good := "t=" + ts + ",v1=" + hmacHex("whsec_test", ts+"."+string(body))
	if !stripeSigOK("whsec_test", good, body, now) {
		t.Fatal("valid signature rejected")
	}
	if stripeSigOK("whsec_other", good, body, now) {
		t.Fatal("wrong secret accepted")
	}
	if stripeSigOK("whsec_test", good, []byte(`{"tampered":1}`), now) {
		t.Fatal("tampered body accepted")
	}
	if stripeSigOK("whsec_test", good, body, now.Add(10*time.Minute)) {
		t.Fatal("old signature accepted (replay)")
	}
	if stripeSigOK("whsec_test", "garbage", body, now) {
		t.Fatal("garbage accepted")
	}
}

func TestMinorUnits(t *testing.T) {
	for in, want := range map[float64]int64{1999: 199900, 19.99: 1999, 0.1: 10} {
		if got := minor(in); got != want {
			t.Fatalf("minor(%v)=%d want %d", in, got, want)
		}
	}
}
