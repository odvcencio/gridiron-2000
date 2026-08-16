package wire

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	arbiter "m31labs.dev/arbiter"
)

//go:embed trust_rules.arb
var embeddedTrustRules []byte

type TrustPolicy struct {
	program *arbiter.Program
}

func NewTrustPolicy(overridePath string) (*TrustPolicy, error) {
	source := embeddedTrustRules
	if path := strings.TrimSpace(overridePath); path != "" {
		override, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read trust rules: %w", err)
		}
		source = override
	}
	program, err := arbiter.Compile(source)
	if err != nil {
		return nil, fmt.Errorf("compile trust rules: %w", err)
	}
	return &TrustPolicy{program: program}, nil
}

func (policy *TrustPolicy) Assess(evidenceType string) (TrustAssessment, error) {
	if policy == nil || policy.program == nil {
		return TrustAssessment{}, fmt.Errorf("trust policy is not initialized")
	}
	facts := map[string]any{
		"source": map[string]any{
			"evidence": strings.ToLower(strings.TrimSpace(evidenceType)),
		},
	}
	matches, err := arbiter.Eval(policy.program, arbiter.DataFromMap(facts, policy.program))
	if err != nil {
		return TrustAssessment{}, fmt.Errorf("evaluate trust rules: %w", err)
	}
	for _, match := range matches {
		if match.Action != "Trust" {
			continue
		}
		return TrustAssessment{
			Tier:   stringParam(match.Params, "tier"),
			Rule:   match.Name,
			Weight: numberParam(match.Params, "weight"),
		}, nil
	}
	return TrustAssessment{}, fmt.Errorf("trust rules returned no decision")
}
