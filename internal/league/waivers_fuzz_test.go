package league

import (
	"fmt"
	"testing"
	"time"
)

// FuzzPickNextClaim exercises the waiver resolver's one-round selection
// (pickNextClaim, claimLess, claimLessFAAB — waivers.go): the pure
// decision section 5.4 steps 2/3 of the waiver spec make once per
// resolved claim inside Store.ProcessWaivers.
//
// raw decodes into a small, synthetic claim set: a mode byte, then one
// 4-byte record per claim (team index, priority, bid, and a filed-at
// second). Every synthetic claim's AddID is its own index (p0, p1, ...),
// so two claims can still tie on every comparator field (team, priority,
// bid) and remain individually identifiable in the assertions below.
// Store.ProcessWaivers only ever calls pickNextClaim with a non-empty
// remaining slice (store.go's "for len(remaining) > 0" loop); this target
// keeps that same precondition rather than fuzzing a call shape
// production never makes.
func FuzzPickNextClaim(f *testing.F) {
	f.Add([]byte{0x00, 0x01, 0x02, 0x03, 0x04})
	f.Add([]byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	f.Add([]byte{0x01, 1, 1, 1, 0, 2, 1, 1, 0}) // two teams, same priority/bid: a FAAB tie
	f.Add(make([]byte, 33))                     // mode + 8 all-zero claims
	f.Add([]byte{0x00, 200, 5, 9, 3, 100, 5, 9, 3})

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) < 1 {
			return
		}
		mode := "perf"
		if raw[0]%2 == 1 {
			mode = "faab"
		}
		raw = raw[1:]
		const recordSize = 4
		n := len(raw) / recordSize
		if n == 0 {
			return
		}
		if n > 32 {
			// Bound one fuzz iteration's work; the selection scan is O(n)
			// regardless, so this loses no coverage of the algorithm's
			// shape.
			n = 32
		}
		order := []string{"team-0", "team-1", "team-2", "team-3"}
		before := make([]WaiverClaim, n)
		for i := 0; i < n; i++ {
			rec := raw[i*recordSize : i*recordSize+recordSize]
			before[i] = WaiverClaim{
				ID:       fmt.Sprintf("clm-%d", i),
				TeamID:   order[int(rec[0])%len(order)],
				AddID:    fmt.Sprintf("p%d", i), // unique: tells ties apart below
				Priority: int(rec[1]),
				Bid:      int(rec[2]),
				FiledAt:  time.Unix(int64(rec[3]), 0),
			}
		}
		claims := append([]WaiverClaim(nil), before...)

		chosen, rest := pickNextClaim(claims, order, mode)

		if len(rest) != len(before)-1 {
			t.Fatalf("pickNextClaim(%+v, mode=%s) left %d claims, want %d", before, mode, len(rest), len(before)-1)
		}

		less := claimLess
		if mode == "faab" {
			less = claimLessFAAB
		}
		// chosen must be a genuine minimum under the active comparator: no
		// claim in the original set may compare less than it.
		for _, c := range before {
			if less(c, chosen, order) {
				t.Fatalf("pickNextClaim(%+v, mode=%s) chose %+v, but %+v compares less", before, mode, chosen, c)
			}
		}

		chosenSeen := false
		for _, c := range before {
			if c.AddID == chosen.AddID {
				chosenSeen = true
				break
			}
		}
		if !chosenSeen {
			t.Fatalf("pickNextClaim(%+v, mode=%s) chose %+v, which was never in the input", before, mode, chosen)
		}

		restIDs := make(map[string]bool, len(rest))
		for _, c := range rest {
			if c.AddID == chosen.AddID {
				t.Fatalf("pickNextClaim(%+v, mode=%s) left the chosen claim %+v in rest too", before, mode, c)
			}
			if restIDs[c.AddID] {
				t.Fatalf("pickNextClaim(%+v, mode=%s) duplicated claim %+v in rest", before, mode, c)
			}
			restIDs[c.AddID] = true
		}
		for _, c := range before {
			if c.AddID == chosen.AddID {
				continue
			}
			if !restIDs[c.AddID] {
				t.Fatalf("pickNextClaim(%+v, mode=%s) dropped claim %+v", before, mode, c)
			}
		}
	})
}
