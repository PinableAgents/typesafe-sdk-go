package agenttool

import (
	"testing"

	"github.com/PinableAgents/typesafe-sdk-go/contrib/agentpolicy"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

func TestHostPolicyConfiguration(t *testing.T) {
	_, client := local(t, mockapi.New())
	if _, err := NewWithConfig(client, agentpolicy.Config{}); err == nil {
		t.Fatal("invalid trusted-host policy accepted")
	}
	cfg := agentpolicy.DefaultConfig()
	if _, err := NewWithConfig(client, cfg); err != nil {
		t.Fatal(err)
	}
}
