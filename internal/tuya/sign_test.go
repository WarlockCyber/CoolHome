package tuya

import (
	"strings"
	"testing"
)

// RFC 4231, test case 1.
func TestHMACSHA256KnownVector(t *testing.T) {
	key := strings.Repeat("\x0b", 20)
	got := hmacSHA256Hex(key, "Hi There")
	want := "B0344C61D8DB38535CA8AFCEAF0BF12B881DC200C9833DA726E9376C2E32CFF7"
	if got != want {
		t.Fatalf("hmacSHA256Hex = %s, want %s", got, want)
	}
}

func TestSHA256EmptyBody(t *testing.T) {
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := sha256Hex(""); got != want {
		t.Fatalf("sha256Hex(\"\") = %s, want %s", got, want)
	}
}

func TestCanonicalStringLayout(t *testing.T) {
	// The blank third line is deliberate: the signed header section is empty.
	got := canonicalString("GET", sha256Hex(""), "/v1.0/token?grant_type=1")
	want := "GET\n" + sha256Hex("") + "\n\n/v1.0/token?grant_type=1"
	if got != want {
		t.Fatalf("canonicalString = %q, want %q", got, want)
	}
}

func TestSignDigestsAreUpperHexAndDifferByScope(t *testing.T) {
	const (
		clientID = "client"
		secret   = "secret"
		access   = "access-token"
	)
	ts := int64(1700000000000)
	tokenSign := signToken(clientID, secret, ts, "GET", sha256Hex(""), "/v1.0/token?grant_type=1")
	businessSign := signBusiness(clientID, access, secret, ts, "GET", sha256Hex(""), "/v1.0/devices/x")

	for name, sign := range map[string]string{"token": tokenSign, "business": businessSign} {
		if len(sign) != 64 {
			t.Fatalf("%s signature length = %d, want 64", name, len(sign))
		}
		if sign != strings.ToUpper(sign) {
			t.Fatalf("%s signature is not uppercase hex: %s", name, sign)
		}
	}
	if tokenSign == businessSign {
		t.Fatal("token and business signatures must differ")
	}
}
