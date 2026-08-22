package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type externalSignalInput struct {
	ID           string
	Source       string
	SourceName   string
	ReportedBy   string
	SourceURI    string
	SourceURL    string
	EvidenceType string
	ClusterKey   string
	CID          string
	Text         string
	OccurredAt   time.Time
	ObservedAt   time.Time
}

func (service *Service) ingestExternal(input externalSignalInput) (bool, error) {
	text := compactText(input.Text, 480)
	classification, err := service.classifier.ClassifyEvidence(text, input.EvidenceType)
	if err != nil {
		return false, err
	}
	if !classification.Relevant {
		return false, nil
	}
	trust, err := service.trust.Assess(input.EvidenceType)
	if err != nil {
		return false, err
	}
	if input.ObservedAt.IsZero() {
		input.ObservedAt = service.now().UTC()
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = input.ObservedAt
	}
	if input.ID == "" {
		input.ID = hashParts("signal", input.SourceURI)
	}
	if input.CID == "" {
		input.CID = hashParts("content", text, input.SourceURL)
	}
	clusterKey := strings.TrimSpace(input.ClusterKey)
	if clusterKey == "" {
		clusterKey = canonicalSourceURL(input.SourceURL)
	}
	if clusterKey == "" {
		clusterKey = strings.ToLower(text)
	}
	textHash := sha256.Sum256([]byte(text))
	confidence := classification.Confidence * trust.Weight
	if confidence > 0.99 {
		confidence = 0.99
	}
	return service.store.Apply(Signal{
		SchemaVersion: SchemaVersion,
		ID:            input.ID,
		Source:        input.Source,
		SourceName:    compactText(input.SourceName, 100),
		ReportedBy:    compactText(input.ReportedBy, 100),
		SourceURI:     input.SourceURI,
		SourceURL:     input.SourceURL,
		EvidenceType:  input.EvidenceType,
		TrustTier:     trust.Tier,
		ClusterID:     hashParts("cluster", clusterKey),
		CID:           input.CID,
		Category:      classification.Category,
		Label:         classification.Label,
		Text:          text,
		TextHash:      hex.EncodeToString(textHash[:]),
		Rule:          classification.Rule,
		TrustRule:     trust.Rule,
		Confidence:    confidence,
		Provisional:   true,
		OccurredAt:    input.OccurredAt.UTC(),
		ObservedAt:    input.ObservedAt.UTC(),
	}, "create", 0)
}

func (service *Service) SubmitSighting(submission CommunitySubmission) (Signal, error) {
	submission.ReporterID = strings.TrimSpace(submission.ReporterID)
	submission.ReporterName = compactText(submission.ReporterName, 100)
	submission.EvidenceType = strings.ToLower(strings.TrimSpace(submission.EvidenceType))
	rawSourceName := strings.TrimSpace(submission.SourceName)
	if utf8.RuneCountInString(rawSourceName) > 100 {
		return Signal{}, fmt.Errorf("source name must be 100 characters or fewer")
	}
	submission.SourceName = compactText(rawSourceName, 100)
	submission.SourceURL = strings.TrimSpace(submission.SourceURL)
	rawSummary := strings.TrimSpace(submission.Summary)
	if utf8.RuneCountInString(rawSummary) > 480 {
		return Signal{}, fmt.Errorf("summary must be 480 characters or fewer")
	}
	submission.Summary = compactText(rawSummary, 480)
	if submission.ReporterID == "" {
		return Signal{}, fmt.Errorf("league identity is required")
	}
	if submission.Summary == "" {
		return Signal{}, fmt.Errorf("summary is required")
	}
	if submission.SourceName == "" {
		return Signal{}, fmt.Errorf("where you saw it is required")
	}
	switch submission.EvidenceType {
	case "community", "submitted_news", "market":
	default:
		return Signal{}, fmt.Errorf("sighting type is invalid")
	}
	if submission.SourceURL != "" {
		if len(submission.SourceURL) > 2048 {
			return Signal{}, fmt.Errorf("source link is too long")
		}
		parsed, err := url.Parse(submission.SourceURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return Signal{}, fmt.Errorf("Paste a public web link that starts with https.")
		}
		submission.SourceURL = parsed.String()
	}
	now := service.now().UTC()
	reporterKey := hashParts("reporter", submission.ReporterID)
	reportKey := hashParts("report", submission.ReporterID, submission.EvidenceType, submission.SourceName, submission.SourceURL, submission.Summary)
	sourceURI := "league://" + reporterKey[:16] + "/" + reportKey
	clusterKey := canonicalSourceURL(submission.SourceURL)
	if clusterKey == "" {
		clusterKey = strings.ToLower(submission.Summary)
	}
	accepted, err := service.ingestExternal(externalSignalInput{
		ID:           reportKey,
		Source:       SourceLeague,
		SourceName:   submission.SourceName,
		ReportedBy:   submission.ReporterName,
		SourceURI:    sourceURI,
		SourceURL:    submission.SourceURL,
		EvidenceType: submission.EvidenceType,
		ClusterKey:   clusterKey,
		CID:          hashParts("submission", submission.Summary, submission.SourceURL),
		Text:         submission.Summary,
		OccurredAt:   now,
		ObservedAt:   now,
	})
	if err != nil {
		return Signal{}, err
	}
	if !accepted {
		return Signal{}, fmt.Errorf("that sighting is already on the wire")
	}
	if signal, ok := service.store.Get(reportKey); ok {
		return signal, nil
	}
	return Signal{ID: reportKey}, nil
}

func hashParts(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
