package commissionerhq

import "testing"

func TestParsePeersAcceptsBoundedOrigins(t *testing.T) {
	peers, err := parsePeers("g2k", "skl=http://gridiron-sk.stablekernel.svc.cluster.local,fun=https://fun.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 || peers[0].ID != "skl" || peers[0].BaseURL.Path != "" {
		t.Fatalf("peers = %#v", peers)
	}
}

func TestParsePeersRejectsUnsafeOrAmbiguousTopology(t *testing.T) {
	tests := []string{
		"g2k=https://self.example",
		"skl=https://user:pass@sk.example",
		"skl=https://sk.example/path",
		"skl=https://sk.example?token=x",
		"skl=https://sk.example#fragment",
		"skl=file:///tmp/state",
		"skl=https://one.example,skl=https://two.example",
		"BAD=https://sk.example",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := parsePeers("g2k", raw); err == nil {
				t.Fatalf("parsePeers accepted %q", raw)
			}
		})
	}
}

func TestConfigFromEnvRequiresTokenForPeers(t *testing.T) {
	t.Setenv("COMMISSIONER_INSTANCE_ID", "g2k")
	t.Setenv("COMMISSIONER_HQ_PEERS", "skl=https://sk.example")
	t.Setenv("COMMISSIONER_HQ_TOKEN", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("peer topology without token must fail closed")
	}
}
