package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestGCPAdaptorRegistration(t *testing.T) {
	adaptor := GetAdaptor(constant.APITypeGCP)
	if adaptor == nil {
		t.Fatal("expected GCP adaptor to be registered")
	}
	if got := adaptor.GetChannelName(); got != "google-cloud" {
		t.Fatalf("unexpected channel name: %q", got)
	}
}
