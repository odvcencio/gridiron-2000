package wire

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	arbiter "m31labs.dev/arbiter"
)

//go:embed signal_rules.arb
var embeddedSignalRules []byte

type Classifier struct {
	program *arbiter.Program
}

func NewClassifier(overridePath string) (*Classifier, error) {
	source := embeddedSignalRules
	if path := strings.TrimSpace(overridePath); path != "" {
		override, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read signal rules: %w", err)
		}
		source = override
	}
	program, err := arbiter.Compile(source)
	if err != nil {
		return nil, fmt.Errorf("compile signal rules: %w", err)
	}
	return &Classifier{program: program}, nil
}

func (classifier *Classifier) Classify(text string) (Classification, error) {
	return classifier.ClassifyEvidence(text, "social")
}

func (classifier *Classifier) ClassifyEvidence(text, evidenceType string) (Classification, error) {
	if classifier == nil || classifier.program == nil {
		return Classification{}, fmt.Errorf("signal classifier is not initialized")
	}
	facts := map[string]any{
		"post": map[string]any{
			"text": strings.ToLower(strings.TrimSpace(text)),
		},
		"source": map[string]any{
			"evidence": strings.ToLower(strings.TrimSpace(evidenceType)),
		},
	}
	matches, err := arbiter.Eval(classifier.program, arbiter.DataFromMap(facts, classifier.program))
	if err != nil {
		return Classification{}, fmt.Errorf("evaluate signal rules: %w", err)
	}
	for _, match := range matches {
		if match.Action == "Ignore" {
			return Classification{Category: "noise", Label: "IGNORED", Rule: match.Name}, nil
		}
		if match.Action != "Classify" {
			continue
		}
		return Classification{
			Category:   stringParam(match.Params, "category"),
			Label:      stringParam(match.Params, "label"),
			Rule:       match.Name,
			Confidence: numberParam(match.Params, "confidence"),
			Relevant:   true,
		}, nil
	}
	return Classification{Category: "noise", Label: "IGNORED", Rule: "IgnoreNoise"}, nil
}

func stringParam(params map[string]any, key string) string {
	return strings.TrimSpace(fmt.Sprint(params[key]))
}

func numberParam(params map[string]any, key string) float64 {
	switch value := params[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
		return parsed
	}
}
