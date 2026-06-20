package services

import (
	"net"
	"strings"
	"testing"
)

func TestProviderRelayStartReturnsBindError(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()

	relay := &ProviderRelayService{
		providerService: NewProviderService(),
		addr:            listener.Addr().String(),
		rrLastStart:     make(map[string]string),
	}

	err = relay.Start()
	if err == nil {
		t.Fatal("expected bind error")
	}
	if !strings.Contains(err.Error(), "listen") && !strings.Contains(err.Error(), "bind") {
		t.Fatalf("expected bind/listen error, got %v", err)
	}
}
