package league

import "gridiron-2000/internal/notify"

// NotificationReceipt is the PII-free outcome of one notification batch.
// Queued means accepted by the bounded in-process queue; it does not mean
// delivered. A ledger entry is durable before a message is offered to that
// queue, so QueueDrops explicitly reports the at-most-once gap.
type NotificationReceipt struct {
	Requested            int  `json:"requested"`
	Queued               int  `json:"queued"`
	PreferenceSuppressed int  `json:"preference_suppressed"`
	AlreadyRecorded      int  `json:"already_recorded"`
	LedgerFailures       int  `json:"ledger_failures"`
	QueueDrops           int  `json:"queue_drops"`
	TransportDisabled    bool `json:"transport_disabled"`
	TransportNotWired    bool `json:"transport_not_wired"`
}

func (r *NotificationReceipt) merge(other NotificationReceipt) {
	r.Requested += other.Requested
	r.Queued += other.Queued
	r.PreferenceSuppressed += other.PreferenceSuppressed
	r.AlreadyRecorded += other.AlreadyRecorded
	r.LedgerFailures += other.LedgerFailures
	r.QueueDrops += other.QueueDrops
	r.TransportDisabled = r.TransportDisabled || other.TransportDisabled
	r.TransportNotWired = r.TransportNotWired || other.TransportNotWired
}

func (s *Service) notificationTransport() (*notify.Queue, bool) {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	return s.notifyQueue, s.notifyTransportEnabled
}

func (s *Service) notificationTransportReceipt() NotificationReceipt {
	queue, enabled := s.notificationTransport()
	return NotificationReceipt{
		TransportDisabled: queue != nil && !enabled,
		TransportNotWired: queue == nil,
	}
}
