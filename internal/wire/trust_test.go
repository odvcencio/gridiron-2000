package wire

import "testing"

func TestTrustPolicyRanksSourcesWithoutGrantingAuthority(t *testing.T) {
	policy, err := NewTrustPolicy("")
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := policy.Assess("news")
	if err != nil {
		t.Fatal(err)
	}
	market, err := policy.Assess("market")
	if err != nil {
		t.Fatal(err)
	}
	if publisher.Tier != "PUBLISHER" || publisher.Weight <= market.Weight {
		t.Fatalf("publisher=%+v market=%+v", publisher, market)
	}
	community, err := policy.Assess("community_feed")
	if err != nil {
		t.Fatal(err)
	}
	if community.Tier != "COMMUNITY" || community.Weight <= market.Weight {
		t.Fatalf("community=%+v market=%+v", community, market)
	}
}
