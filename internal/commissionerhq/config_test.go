package commissionerhq

import "testing"

func TestParsePeersAcceptsExplicitServiceAndPublicOrigins(t *testing.T) {
	peers, err := parsePeers("g2k", "skl=http://service.internal:80/|https://SK.example:443/,fun=https://fun.example|http://public.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 || peers[0].ID != "skl" {
		t.Fatalf("peers = %#v", peers)
	}
	if got := peers[0].ServiceURL.String(); got != "http://service.internal" {
		t.Fatalf("service origin = %q", got)
	}
	if got := peers[0].PublicURL.String(); got != "https://sk.example" {
		t.Fatalf("public origin = %q", got)
	}
	if got := peers[1].PublicURL.String(); got != "http://public.example:8080" {
		t.Fatalf("non-default public origin = %q", got)
	}
}

func TestParsePeersRejectsUnsafeOrAmbiguousTopology(t *testing.T) {
	tests := []string{
		"g2k=https://self.example|https://self.example",
		"skl=https://service.example",
		"skl=https://service.example|https://public.example|https://extra.example",
		"skl=https://user:pass@service.example|https://public.example",
		"skl=https://service.example|https://user:pass@public.example",
		"skl=https://service.example/path|https://public.example",
		"skl=https://service.example|https://public.example/path",
		"skl=https://service.example?token=x|https://public.example",
		"skl=https://service.example|https://public.example?token=x",
		"skl=https://service.example#fragment|https://public.example",
		"skl=https://service.example|https://public.example#",
		"skl=https://service.example|https://public.example#fragment",
		"skl=file:///tmp/state|https://public.example",
		"skl=https://service.example:bad|https://public.example",
		"skl=https://service.example:0|https://public.example",
		"skl=https://service.example:65536|https://public.example",
		"skl=https://one.example|https://one.example,skl=https://two.example|https://two.example",
		"BAD=https://service.example|https://public.example",
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
	t.Setenv("COMMISSIONER_HQ_PEERS", "skl=https://service.example|https://public.example")
	t.Setenv("COMMISSIONER_HQ_TOKEN", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("peer topology without token must fail closed")
	}
}

func TestNormalizedPublicURLStripsDefaultPort(t *testing.T) {
	got, err := normalizedPublicURL("HTTPS://Example.test:443/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.test" {
		t.Fatalf("normalized = %q", got)
	}
}
