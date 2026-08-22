package commissionerhq

import "testing"

func TestAttentionSetAllowlistsAreasAndDeduplicatesCodes(t *testing.T) {
	set := NewAttentionSet()
	set.Add("pool_shortfall", AttentionSeverityCritical, 3, "pool is short", AttentionAreaPool)
	set.Add("pool_shortfall", AttentionSeverityWarning, 1, "duplicate", AttentionAreaPool)
	set.Add("bad-area", AttentionSeverityWarning, 1, "must drop", "identity")
	set.Add("bad-severity", "fatal", 1, "must drop", AttentionAreaRuntime)
	items := set.Items()
	if len(items) != 1 || items[0].Code != "pool_shortfall" || items[0].Count != 3 {
		t.Fatalf("items = %#v", items)
	}
	items[0].Message = "mutated copy"
	if set.Items()[0].Message != "pool is short" {
		t.Fatal("Items must return a copy")
	}
}
