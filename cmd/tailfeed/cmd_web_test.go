package main

import "testing"

func TestIncrementAddrPort(t *testing.T) {
	got, err := incrementAddrPort("127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:8081" {
		t.Fatalf("incrementAddrPort() = %q, want %q", got, "127.0.0.1:8081")
	}
}

func TestIncrementAddrPortPreservesWildcardHost(t *testing.T) {
	got, err := incrementAddrPort(":8080")
	if err != nil {
		t.Fatal(err)
	}
	if got != ":8081" {
		t.Fatalf("incrementAddrPort() = %q, want %q", got, ":8081")
	}
}
