package services

import (
	"testing"
	"time"
)

func TestBatchCheckTimeoutUsesLargestEnabledProviderTimeout(t *testing.T) {
	providers := []Provider{
		{AvailabilityMonitorEnabled: true, AvailabilityConfig: &AvailabilityConfig{Timeout: 45000}},
		{AvailabilityMonitorEnabled: true, AvailabilityConfig: &AvailabilityConfig{Timeout: 15000}},
		{AvailabilityMonitorEnabled: false, AvailabilityConfig: &AvailabilityConfig{Timeout: 120000}},
	}

	got := batchCheckTimeout(providers)
	want := 50 * time.Second
	if got != want {
		t.Fatalf("batchCheckTimeout=%s want %s", got, want)
	}
}

func TestBatchCheckTimeoutUsesDefaultWhenNoEnabledProviders(t *testing.T) {
	got := batchCheckTimeout([]Provider{{AvailabilityMonitorEnabled: false}})
	want := time.Duration(DefaultTimeoutMs)*time.Millisecond + 5*time.Second
	if got != want {
		t.Fatalf("batchCheckTimeout=%s want %s", got, want)
	}
}
