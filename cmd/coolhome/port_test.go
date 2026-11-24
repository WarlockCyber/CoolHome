package main

import (
	"errors"
	"net"
	"os"
	"testing"
)

func TestIsAddrInUseRecognisesATakenPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// A second bind on the same port is exactly what the check exists for.
	_, err = net.Listen("tcp", ln.Addr().String())
	if err == nil {
		t.Fatal("expected the second bind to fail")
	}
	if !isAddrInUse(err) {
		t.Errorf("isAddrInUse(%v) = false, want true", err)
	}
}

func TestIsAddrInUseRejectsOtherErrors(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("plain"),
		os.ErrNotExist,
		&net.AddrError{Err: "too many colons", Addr: ":::"},
	} {
		if isAddrInUse(err) {
			t.Errorf("isAddrInUse(%v) = true, want false", err)
		}
	}
}
