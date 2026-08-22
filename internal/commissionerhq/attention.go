package commissionerhq

const (
	AttentionAreaRuntime      = "runtime"
	AttentionAreaMembership   = "membership"
	AttentionAreaDraft        = "draft"
	AttentionAreaSchedule     = "schedule"
	AttentionAreaPool         = "pool"
	AttentionAreaOpenData     = "open-data"
	AttentionSeverityInfo     = "info"
	AttentionSeverityWarning  = "warning"
	AttentionSeverityCritical = "critical"
)

var attentionAreas = map[string]struct{}{
	AttentionAreaRuntime:    {},
	AttentionAreaMembership: {},
	AttentionAreaDraft:      {},
	AttentionAreaSchedule:   {},
	AttentionAreaPool:       {},
	AttentionAreaOpenData:   {},
}

var attentionSeverities = map[string]struct{}{
	AttentionSeverityInfo:     {},
	AttentionSeverityWarning:  {},
	AttentionSeverityCritical: {},
}

type AttentionSet struct {
	items []Attention
	seen  map[string]struct{}
}

func NewAttentionSet() *AttentionSet {
	return &AttentionSet{seen: make(map[string]struct{})}
}

func (a *AttentionSet) Add(code, severity string, count int, message, area string) {
	if a == nil || code == "" || message == "" {
		return
	}
	if _, ok := attentionAreas[area]; !ok {
		return
	}
	if _, ok := attentionSeverities[severity]; !ok {
		return
	}
	if _, ok := a.seen[code]; ok {
		return
	}
	a.seen[code] = struct{}{}
	a.items = append(a.items, Attention{
		Code: code, Severity: severity, Count: count, Message: message, Area: area,
	})
}

func (a *AttentionSet) Items() []Attention {
	if a == nil {
		return nil
	}
	items := make([]Attention, len(a.items))
	copy(items, a.items)
	return items
}
