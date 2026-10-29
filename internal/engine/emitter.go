package engine

import (
	"sync"
	"time"

	"github.com/litmuschaos/litmus-lite/internal/compose"
	"github.com/litmuschaos/litmus-lite/internal/probe"
	"github.com/litmuschaos/litmus-lite/internal/report"
	"github.com/litmuschaos/litmus-lite/internal/scenario"
)

// Emitter receives what happens during a run. The engine calls it and never
// waits on it: an implementation must return quickly and must not fail the
// run. A nil Options.Emitter means nothing is emitted.
//
// Every run that calls Started ends with exactly one of Finished or Failed,
// on every exit path, after faults have been rolled back.
type Emitter interface {
	// Started is the first call. startedAt is the run's time zero: the
	// offsets of points and of the final series count from it.
	Started(sc *scenario.Scenario, startedAt time.Time)
	// Phase marks a phase boundary: steady, inject or recover.
	Phase(name string)
	// FaultStarted is called when a fault is armed, FaultEnded once it has
	// been rolled back. A fault that never started is not announced.
	FaultStarted(f report.FaultEvent)
	FaultEnded(f report.FaultEvent)
	// Point is one closed one-second bucket, computed by report.BucketPoint.
	Point(p report.Point)
	// Sample is one probe sample.
	Sample(s probe.Sample)
	// Warning mirrors a line added to Result.Warnings.
	Warning(msg string)
	// LoadStarted is called when a load generator is started. startOffsetS is
	// seconds from the run's time zero. LoadPoint is one second of its
	// progress and LoadFinished is called once when it exits; offsets in both
	// count from the run's time zero. LoadFinished is not called when no
	// generator was started, and LoadPoint only when the tool reports progress.
	LoadStarted(tool string, startOffsetS float64)
	LoadPoint(p compose.LivePoint)
	LoadFinished(series []compose.LoadPoint, errMsg string)
	// Finished carries the complete result of a run that ran to the end.
	Finished(res *report.Result)
	// Failed ends a run that did not complete. res is nil unless the run got
	// far enough to have a partial result; started lists the faults that had
	// been armed.
	Failed(reason string, res *report.Result, started []report.FaultEvent)
}

type nopEmitter struct{}

func (nopEmitter) Started(*scenario.Scenario, time.Time)              {}
func (nopEmitter) Phase(string)                                       {}
func (nopEmitter) FaultStarted(report.FaultEvent)                     {}
func (nopEmitter) FaultEnded(report.FaultEvent)                       {}
func (nopEmitter) Point(report.Point)                                 {}
func (nopEmitter) Sample(probe.Sample)                                {}
func (nopEmitter) Warning(string)                                     {}
func (nopEmitter) LoadStarted(string, float64)                        {}
func (nopEmitter) LoadPoint(compose.LivePoint)                        {}
func (nopEmitter) LoadFinished([]compose.LoadPoint, string)           {}
func (nopEmitter) Finished(*report.Result)                            {}
func (nopEmitter) Failed(string, *report.Result, []report.FaultEvent) {}

// farFuture stands in for the end of a fault that is still running.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// livePoints emits one point per second, as each one-second bucket closes.
//
// A bucket is held back while a probe that started before the bucket's end is
// still in flight, because that probe's sample belongs to the bucket and is
// not recorded until it returns. This is what makes a live point equal the
// point of the final series.
type livePoints struct {
	em    Emitter
	start time.Time

	mu       sync.Mutex
	samples  func() []probe.Sample // snapshot of samples recorded so far
	faults   []report.FaultEvent   // faults armed so far; a running one ends in the far future
	inflight map[int]time.Time     // probe id -> start time
	next     int                   // index of the next bucket to emit
	nextID   int

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func newLivePoints(em Emitter, start time.Time, samples func() []probe.Sample) *livePoints {
	return &livePoints{
		em: em, start: start, samples: samples,
		inflight: map[int]time.Time{},
		stop:     make(chan struct{}), done: make(chan struct{}),
	}
}

// probeStarted and probeDone bracket one probe call.
func (l *livePoints) probeStarted() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	l.inflight[l.nextID] = time.Now()
	return l.nextID
}

func (l *livePoints) probeDone(id int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.inflight, id)
	l.mu.Unlock()
}

// setFaults replaces the fault snapshot. Faults that have not been armed are
// left out, and a fault without an end time is treated as still running.
func (l *livePoints) setFaults(events []report.FaultEvent) {
	if l == nil {
		return
	}
	snap := make([]report.FaultEvent, 0, len(events))
	for _, f := range events {
		if f.Start.IsZero() {
			continue
		}
		if f.End.IsZero() {
			f.End = farFuture
		}
		snap = append(snap, f)
	}
	l.mu.Lock()
	l.faults = snap
	l.mu.Unlock()
}

func (l *livePoints) begin() {
	if l == nil {
		return
	}
	go func() {
		defer close(l.done)
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-l.stop:
				return
			case <-t.C:
				l.flush()
			}
		}
	}()
}

// end stops the ticker and returns how many buckets were emitted. It can be
// called more than once.
func (l *livePoints) end() int {
	if l == nil {
		return 0
	}
	l.once.Do(func() { close(l.stop) })
	<-l.done
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.next
}

func (l *livePoints) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	limit := time.Now()
	for _, s := range l.inflight {
		if s.Before(limit) {
			limit = s
		}
	}
	var samples []probe.Sample
	loaded := false
	for {
		t := l.start.Add(time.Duration(l.next) * time.Second)
		if !t.Add(time.Second).Before(limit) {
			return
		}
		if !loaded {
			samples = l.samples()
			loaded = true
		}
		l.em.Point(report.BucketPoint(l.start, t, samples, l.faults))
		l.next++
	}
}
