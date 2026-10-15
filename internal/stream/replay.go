package stream

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// Pusher sends already-numbered events to sinks, each through its own queue
// and writer, the way a live run does. It is what `stream replay` uses to push
// a recording: the events keep their seq and ts, so a receiver sees what the
// original run would have sent.
type Pusher struct {
	sinks []*runner
}

// NewPusher starts one runner per sink. Nothing is dropped: a replay is a
// test of a receiver, and a missing event would make the test lie.
func NewPusher(sinks ...Sink) *Pusher {
	p := &Pusher{}
	for _, s := range sinks {
		p.sinks = append(p.sinks, newRunner(s, math.MaxInt32, nil))
	}
	return p
}

// Send queues one event line for every sink.
func (p *Pusher) Send(typ string, line []byte) {
	class := classOf(typ)
	for _, s := range p.sinks {
		s.q.push(class, line)
	}
}

// Warnings returns what has gone wrong with the sinks.
func (p *Pusher) Warnings() []string { return runnerWarnings(p.sinks) }

// Close waits up to timeout for the sinks to deliver what is queued and
// returns the names of those that did not finish.
func (p *Pusher) Close(timeout time.Duration) []string { return closeRunners(p.sinks, timeout) }

// ReplayOptions controls ReplayPush.
type ReplayOptions struct {
	// Speed divides the recorded gaps between events. Zero or less sends
	// everything at once.
	Speed float64
	// MaxGap caps the pause between two events. Zero means no cap.
	MaxGap time.Duration
	// RunID, when set, replaces the run id (one recording only).
	RunID string
	// Fresh gives every recording a new random run id, so that the same
	// recording can be pushed again to a receiver that has seen it.
	Fresh bool
}

// ReplayPush sends the recordings, one after the other, at their recorded pace
// divided by Speed. It returns the run id used for each recording, in order,
// and stops early when ctx is cancelled.
func ReplayPush(ctx context.Context, recs []*Recording, p *Pusher, opt ReplayOptions) ([]string, error) {
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		id := rec.RunID()
		switch {
		case opt.RunID != "":
			id = opt.RunID
		case opt.Fresh:
			id = NewRunID()
		}
		ids = append(ids, id)
		for i, ev := range rec.Events {
			if i > 0 && opt.Speed > 0 {
				if gap := ev.TS.Sub(rec.Events[i-1].TS); gap > 0 {
					d := time.Duration(float64(gap) / opt.Speed)
					if opt.MaxGap > 0 && d > opt.MaxGap {
						d = opt.MaxGap
					}
					t := time.NewTimer(d)
					select {
					case <-ctx.Done():
						t.Stop()
						return ids, ctx.Err()
					case <-t.C:
					}
				}
			} else if err := ctx.Err(); err != nil {
				return ids, err
			}
			line := rec.Raw[i]
			if id != rec.RunID() {
				// Change only the run id; the data is forwarded untouched.
				var m map[string]json.RawMessage
				if err := json.Unmarshal(line, &m); err != nil {
					return ids, err
				}
				m["runId"], _ = json.Marshal(id)
				var err error
				if line, err = json.Marshal(m); err != nil {
					return ids, err
				}
			}
			p.Send(ev.Type, line)
		}
	}
	return ids, nil
}
