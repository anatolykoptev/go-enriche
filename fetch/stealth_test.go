package fetch

import "testing"

func TestNewStealthClient_Default(t *testing.T) {
	t.Parallel()
	client, err := NewStealthClient()
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Transport == nil {
		t.Error("expected non-nil transport (stealth roundtripper)")
	}
}

func TestNewStealthClient_WithTimeout(t *testing.T) {
	t.Parallel()
	client, err := NewStealthClient(StealthWithTimeout(30))
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewStealthClient_StdHTTP(t *testing.T) {
	t.Parallel()
	client, err := NewStealthClient(StealthWithStdHTTP())
	if err != nil {
		t.Fatalf("NewStealthClient error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
