package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type wireState struct {
	SchemaVersion   int               `json:"schema_version"`
	Cursor          int64             `json:"cursor,omitempty"`
	Signals         map[string]Signal `json:"signals"`
	Order           []string          `json:"order"`
	RelevantSignals int64             `json:"relevant_signals"`
	IgnoredPosts    int64             `json:"ignored_posts"`
	DeletedSignals  int64             `json:"deleted_signals"`
	UpdatedAt       time.Time         `json:"updated_at,omitzero"`
}

// Store owns the league's social index. Only the current-state file contains a
// short post excerpt; the append-only journal stores hashes and derived facts.
type Store struct {
	mu          sync.RWMutex
	root        string
	recentLimit int
	state       wireState
}

func NewStore(root string, recentLimit int) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("signal wire root is required")
	}
	if recentLimit <= 0 {
		recentLimit = 1000
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create signal wire root: %w", err)
	}
	store := &Store{
		root:        root,
		recentLimit: recentLimit,
		state: wireState{
			SchemaVersion: SchemaVersion,
			Signals:       map[string]Signal{},
			Order:         []string{},
		},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *Store) Cursor() int64 {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.state.Cursor
}

func (store *Store) Get(id string) (Signal, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	signal, ok := store.state.Signals[id]
	if !ok || signal.Deleted {
		return Signal{}, false
	}
	return signal, true
}

func (store *Store) HasCID(id, cid string) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	signal, ok := store.state.Signals[id]
	return ok && !signal.Deleted && cid != "" && signal.CID == cid
}

func (store *Store) Apply(signal Signal, operation string, cursor int64) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	operation = strings.ToLower(strings.TrimSpace(operation))
	if operation == "delete" {
		return store.deleteLocked(signal, cursor)
	}
	if signal.ID == "" || signal.SourceURI == "" {
		return false, fmt.Errorf("signal id and source URI are required")
	}
	existing, existed := store.state.Signals[signal.ID]
	if existed && existing.CID != "" && existing.CID == signal.CID {
		if cursor <= store.state.Cursor {
			return false, nil
		}
		store.advanceCursorLocked(cursor, signal.ObservedAt)
		return false, store.persistLocked()
	}
	signal.SchemaVersion = SchemaVersion
	signal.Deleted = false
	store.state.Signals[signal.ID] = signal
	store.moveToEndLocked(signal.ID)
	store.state.RelevantSignals++
	store.advanceCursorLocked(cursor, signal.ObservedAt)
	store.trimLocked()
	if err := store.persistLocked(); err != nil {
		return false, err
	}
	if err := store.appendJournalLocked(JournalEvent{
		SchemaVersion: SchemaVersion,
		Operation:     operation,
		SignalID:      signal.ID,
		Source:        signal.Source,
		SourceDID:     signal.SourceDID,
		SourceName:    signal.SourceName,
		ReportedBy:    signal.ReportedBy,
		SourceURI:     signal.SourceURI,
		EvidenceType:  signal.EvidenceType,
		TrustTier:     signal.TrustTier,
		ClusterID:     signal.ClusterID,
		Category:      signal.Category,
		TextHash:      signal.TextHash,
		Rule:          signal.Rule,
		TrustRule:     signal.TrustRule,
		Confidence:    signal.Confidence,
		ObservedAt:    signal.ObservedAt,
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (store *Store) RecordIgnored(cursor int64, observedAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.state.IgnoredPosts++
	store.advanceCursorLocked(cursor, observedAt)
	return store.persistLocked()
}

func (store *Store) Advance(cursor int64, observedAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if cursor <= store.state.Cursor {
		return nil
	}
	store.advanceCursorLocked(cursor, observedAt)
	return store.persistLocked()
}

func (store *Store) Recent(limit int, category string) []Signal {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	category = strings.ToLower(strings.TrimSpace(category))
	type clusterView struct {
		signal  Signal
		latest  time.Time
		sources map[string]struct{}
	}
	clusters := make(map[string]*clusterView, len(store.state.Order))
	for _, id := range store.state.Order {
		signal, ok := store.state.Signals[id]
		if !ok || signal.Deleted {
			continue
		}
		clusterID := signal.ClusterID
		if clusterID == "" {
			clusterID = signal.ID
		}
		view, exists := clusters[clusterID]
		if !exists {
			view = &clusterView{signal: signal, latest: signalFreshness(signal), sources: map[string]struct{}{}}
			clusters[clusterID] = view
		}
		view.sources[signalSourceKey(signal)] = struct{}{}
		if freshness := signalFreshness(signal); freshness.After(view.latest) {
			view.latest = freshness
		}
		if signal.Confidence > view.signal.Confidence || (signal.Confidence == view.signal.Confidence && signal.ObservedAt.After(view.signal.ObservedAt)) {
			view.signal = signal
		}
	}
	views := make([]*clusterView, 0, len(clusters))
	for _, view := range clusters {
		if category != "" && view.signal.Category != category {
			continue
		}
		view.signal.Corroborations = len(view.sources)
		views = append(views, view)
	}
	sort.Slice(views, func(left, right int) bool {
		if views[left].latest.Equal(views[right].latest) {
			return views[left].signal.ID < views[right].signal.ID
		}
		return views[left].latest.After(views[right].latest)
	})
	if len(views) > limit {
		views = views[:limit]
	}
	out := make([]Signal, 0, len(views))
	for _, view := range views {
		out = append(out, view.signal)
	}
	return out
}

func signalFreshness(signal Signal) time.Time {
	if !signal.OccurredAt.IsZero() {
		return signal.OccurredAt
	}
	return signal.ObservedAt
}

func (store *Store) Metrics() (relevant, ignored, deleted int64, lastEventAt time.Time) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.state.RelevantSignals, store.state.IgnoredPosts, store.state.DeletedSignals, store.state.UpdatedAt
}

func (store *Store) SourceCounts() map[string]int64 {
	store.mu.RLock()
	defer store.mu.RUnlock()
	counts := map[string]int64{}
	for _, signal := range store.state.Signals {
		if signal.Deleted {
			continue
		}
		key := signal.EvidenceType
		if key == "" {
			key = signal.Source
		}
		counts[key]++
	}
	return counts
}

func (store *Store) Export(writer io.Writer) error {
	store.mu.RLock()
	defer store.mu.RUnlock()
	encoder := json.NewEncoder(writer)
	for _, id := range store.state.Order {
		signal, ok := store.state.Signals[id]
		if !ok || signal.Deleted {
			continue
		}
		if err := encoder.Encode(signal); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) deleteLocked(tombstone Signal, cursor int64) (bool, error) {
	existing, exists := store.state.Signals[tombstone.ID]
	if !exists {
		store.advanceCursorLocked(cursor, tombstone.ObservedAt)
		return false, store.persistLocked()
	}
	if existing.Deleted {
		store.advanceCursorLocked(cursor, tombstone.ObservedAt)
		return false, store.persistLocked()
	}
	existing.Text = ""
	existing.CID = ""
	existing.Deleted = true
	existing.ObservedAt = tombstone.ObservedAt
	store.state.Signals[tombstone.ID] = existing
	store.state.DeletedSignals++
	store.advanceCursorLocked(cursor, tombstone.ObservedAt)
	if err := store.persistLocked(); err != nil {
		return false, err
	}
	if err := store.appendJournalLocked(JournalEvent{
		SchemaVersion: SchemaVersion,
		Operation:     "delete",
		SignalID:      existing.ID,
		Source:        existing.Source,
		SourceDID:     existing.SourceDID,
		SourceName:    existing.SourceName,
		ReportedBy:    existing.ReportedBy,
		SourceURI:     existing.SourceURI,
		EvidenceType:  existing.EvidenceType,
		TrustTier:     existing.TrustTier,
		ClusterID:     existing.ClusterID,
		Category:      existing.Category,
		TextHash:      existing.TextHash,
		Rule:          existing.Rule,
		TrustRule:     existing.TrustRule,
		Confidence:    existing.Confidence,
		ObservedAt:    tombstone.ObservedAt,
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (store *Store) advanceCursorLocked(cursor int64, observedAt time.Time) {
	if cursor > store.state.Cursor {
		store.state.Cursor = cursor
	}
	if observedAt.After(store.state.UpdatedAt) {
		store.state.UpdatedAt = observedAt
	}
}

func (store *Store) moveToEndLocked(id string) {
	for index, existing := range store.state.Order {
		if existing != id {
			continue
		}
		copy(store.state.Order[index:], store.state.Order[index+1:])
		store.state.Order = store.state.Order[:len(store.state.Order)-1]
		break
	}
	store.state.Order = append(store.state.Order, id)
}

func (store *Store) trimLocked() {
	if len(store.state.Order) <= store.recentLimit {
		return
	}
	extra := len(store.state.Order) - store.recentLimit
	for _, id := range store.state.Order[:extra] {
		delete(store.state.Signals, id)
	}
	store.state.Order = append([]string(nil), store.state.Order[extra:]...)
}

func (store *Store) load() error {
	encoded, err := os.ReadFile(store.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read signal wire state: %w", err)
	}
	var state wireState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return fmt.Errorf("decode signal wire state: %w", err)
	}
	if state.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported signal wire schema %d", state.SchemaVersion)
	}
	if state.Signals == nil {
		state.Signals = map[string]Signal{}
	}
	if state.Order == nil {
		state.Order = []string{}
	}
	store.state = state
	return nil
}

func (store *Store) persistLocked() error {
	encoded, err := json.MarshalIndent(store.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode signal wire state: %w", err)
	}
	temp, err := os.CreateTemp(store.root, ".wire-state-*.json")
	if err != nil {
		return fmt.Errorf("create signal wire state: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write signal wire state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync signal wire state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, store.statePath()); err != nil {
		return fmt.Errorf("replace signal wire state: %w", err)
	}
	return nil
}

func (store *Store) appendJournalLocked(event JournalEvent) error {
	if err := os.MkdirAll(store.eventsDir(), 0o700); err != nil {
		return fmt.Errorf("create signal journal: %w", err)
	}
	path := filepath.Join(store.eventsDir(), event.ObservedAt.UTC().Format("2006-01-02")+".ndjson")
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open signal journal: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("append signal journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync signal journal: %w", err)
	}
	return file.Close()
}

func (store *Store) statePath() string {
	return filepath.Join(store.root, "state.json")
}

func (store *Store) eventsDir() string {
	return filepath.Join(store.root, "events")
}

func signalSourceKey(signal Signal) string {
	if signal.SourceURI != "" {
		return signal.SourceURI
	}
	return strings.Join([]string{signal.Source, signal.SourceName, signal.ReportedBy}, "|")
}
