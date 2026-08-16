package wire

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed default_sources.json
var embeddedDefaultSources []byte

type feedSourceFile struct {
	Feeds []FeedSource `json:"feeds"`
}

type feedValidator struct {
	ETag         string
	LastModified string
}

func loadFeedSources(path string, useDefaults bool) ([]FeedSource, error) {
	payload := embeddedDefaultSources
	if path = strings.TrimSpace(path); path != "" {
		var err error
		payload, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read wire sources: %w", err)
		}
	} else if !useDefaults {
		return nil, nil
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(payload), 1<<20))
	decoder.DisallowUnknownFields()
	var document feedSourceFile
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode wire sources: %w", err)
	}
	return normalizeFeedSources(document.Feeds)
}

func normalizeFeedSources(configured []FeedSource) ([]FeedSource, error) {
	if len(configured) > 32 {
		return nil, fmt.Errorf("wire source file exceeds 32 feeds")
	}
	seen := map[string]struct{}{}
	sources := make([]FeedSource, 0, len(configured))
	for _, source := range configured {
		if !source.Enabled {
			continue
		}
		source.Name = compactText(source.Name, 100)
		source.URL = strings.TrimSpace(source.URL)
		source.EvidenceType = strings.ToLower(strings.TrimSpace(source.EvidenceType))
		if source.Name == "" {
			return nil, fmt.Errorf("enabled feed name is required")
		}
		if err := validateFeedURL(source.URL); err != nil {
			return nil, fmt.Errorf("feed %q: %w", source.Name, err)
		}
		switch source.EvidenceType {
		case "news", "community_feed", "social":
		default:
			return nil, fmt.Errorf("feed %q has unsupported evidence type %q", source.Name, source.EvidenceType)
		}
		if _, exists := seen[source.URL]; exists {
			continue
		}
		seen[source.URL] = struct{}{}
		sources = append(sources, source)
	}
	return sources, nil
}

func validateFeedURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("feed URL is invalid")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("feed URL must use HTTPS")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (service *Service) runFeeds(ctx context.Context) {
	service.syncFeeds(ctx)
	ticker := time.NewTicker(service.config.FeedInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.syncFeeds(ctx)
		}
	}
}

func (service *Service) syncFeeds(ctx context.Context) {
	var group sync.WaitGroup
	for _, source := range service.feedSources {
		source := source
		group.Add(1)
		go func() {
			defer group.Done()
			service.syncFeed(ctx, source)
		}()
	}
	group.Wait()
}

func (service *Service) syncFeed(ctx context.Context, source FeedSource) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		service.recordFeedError(source, err)
		return
	}
	request.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9")
	request.Header.Set("User-Agent", "GRIDIRON-2000/1.0 private-league-feed-reader")
	service.mu.RLock()
	validator := service.feedValidators[source.URL]
	service.mu.RUnlock()
	if validator.ETag != "" {
		request.Header.Set("If-None-Match", validator.ETag)
	}
	if validator.LastModified != "" {
		request.Header.Set("If-Modified-Since", validator.LastModified)
	}
	response, err := service.client.Do(request)
	if err != nil {
		service.recordFeedError(source, err)
		return
	}
	defer response.Body.Close()
	now := service.now().UTC()
	if response.StatusCode == http.StatusNotModified {
		service.updateFeedStatus(source, func(status FeedStatus) FeedStatus {
			status.State = "ready"
			status.LastChecked = now
			status.LastError = ""
			return status
		})
		return
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		service.recordFeedError(source, fmt.Errorf("feed returned HTTP %d", response.StatusCode))
		return
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, service.config.FeedMaxBytes+1))
	if err != nil {
		service.recordFeedError(source, err)
		return
	}
	if int64(len(payload)) > service.config.FeedMaxBytes {
		service.recordFeedError(source, fmt.Errorf("feed exceeds %d bytes", service.config.FeedMaxBytes))
		return
	}
	items, err := parseSyndication(payload, source.URL)
	if err != nil {
		service.recordFeedError(source, err)
		return
	}
	sort.SliceStable(items, func(left, right int) bool {
		return feedItemTime(items[left]).Before(feedItemTime(items[right]))
	})
	cutoff := now.Add(-service.config.FeedMaxAge)
	var accepted, ignored int64
	lastPublished := time.Time{}
	for _, item := range items {
		published := feedItemTime(item)
		if !published.IsZero() && published.Before(cutoff) {
			continue
		}
		if published.After(lastPublished) {
			lastPublished = published
		}
		itemKey := strings.TrimSpace(item.ID)
		if itemKey == "" {
			itemKey = item.Link
		}
		if itemKey == "" {
			itemKey = item.Title + "\x00" + item.Description
		}
		sourceURI := "feed://" + hashParts("feed", source.URL)[:16] + "/" + hashParts("item", itemKey)
		cid := hashParts("feed-content", item.Title, item.Description, item.Link, item.UpdatedAt.String())
		if service.feedItemSeen(sourceURI, cid) {
			continue
		}
		signalID := hashParts("feed-signal", sourceURI)
		if service.store.HasCID(signalID, cid) {
			service.markFeedItemSeen(sourceURI, cid)
			continue
		}
		text := strings.TrimSpace(item.Title)
		if item.Description != "" && !strings.EqualFold(item.Description, item.Title) {
			text = strings.TrimSpace(text + ". " + item.Description)
		}
		wasAccepted, ingestErr := service.ingestExternal(externalSignalInput{
			ID:           signalID,
			Source:       SourceFeed,
			SourceName:   source.Name,
			ReportedBy:   item.Author,
			SourceURI:    sourceURI,
			SourceURL:    item.Link,
			EvidenceType: source.EvidenceType,
			ClusterKey:   canonicalSourceURL(item.Link),
			CID:          cid,
			Text:         text,
			OccurredAt:   published,
			ObservedAt:   now,
		})
		if ingestErr != nil {
			service.recordFeedError(source, ingestErr)
			return
		}
		service.markFeedItemSeen(sourceURI, cid)
		if wasAccepted {
			accepted++
		} else {
			ignored++
		}
	}
	service.mu.Lock()
	service.feedValidators[source.URL] = feedValidator{
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}
	service.mu.Unlock()
	service.updateFeedStatus(source, func(status FeedStatus) FeedStatus {
		status.State = "ready"
		status.Accepted += accepted
		status.Ignored += ignored
		status.LastChecked = now
		if lastPublished.After(status.LastPublished) {
			status.LastPublished = lastPublished
		}
		status.LastError = ""
		return status
	})
}

func (service *Service) feedItemSeen(sourceURI, cid string) bool {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.feedSeen[sourceURI] == cid
}

func (service *Service) markFeedItemSeen(sourceURI, cid string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.feedSeen[sourceURI] = cid
}

func (service *Service) recordFeedError(source FeedSource, sourceErr error) {
	service.updateFeedStatus(source, func(status FeedStatus) FeedStatus {
		status.State = "error"
		status.LastChecked = service.now().UTC()
		status.LastError = safeError(sourceErr)
		return status
	})
}

func (service *Service) updateFeedStatus(source FeedSource, update func(FeedStatus) FeedStatus) {
	service.mu.Lock()
	defer service.mu.Unlock()
	status := service.feedStatuses[source.URL]
	if status.Name == "" {
		status = FeedStatus{Name: source.Name, URL: source.URL, EvidenceType: source.EvidenceType, State: "waiting"}
	}
	service.feedStatuses[source.URL] = update(status)
}

func feedItemTime(item feedItem) time.Time {
	if !item.PublishedAt.IsZero() {
		return item.PublishedAt
	}
	return item.UpdatedAt
}
