// Package recorder coordinates AI-driven browser recording sessions.
//
// The recorder owns the session lifecycle (start / stop / busy state) and
// forwards progress events to the UI. The actual browser driving is delegated
// to an Agent, which is the seam where the LLM + Playwright integration goes.
package recorder

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"testdesk/internal/scripts"
)

// Mode says who drives the browser while recording.
type Mode string

const (
	ModeAI     Mode = "ai"     // an LLM performs the steps from Goal
	ModeManual Mode = "manual" // the user performs the steps in an open browser
)

type Request struct {
	URL      string           `json:"url"`
	Language scripts.Language `json:"language"`
	// Mode is ModeAI when empty.
	Mode Mode `json:"mode"`
	// Goal describes what the user wants to test, in natural language. Used by ModeAI only.
	Goal string `json:"goal"`
}

type EventKind string

const (
	EventStatus  EventKind = "status"  // session state change
	EventMessage EventKind = "message" // agent narration
	EventAction  EventKind = "action"  // a browser action the agent performed
	EventCode    EventKind = "code"    // final script source
	EventError   EventKind = "error"
	EventDone    EventKind = "done"
)

type Event struct {
	Kind EventKind `json:"kind"`
	Text string    `json:"text"`
}

// Agent drives a browser according to req and returns the generated script source.
// It must return promptly once ctx is cancelled.
type Agent interface {
	Record(ctx context.Context, req Request, emit func(Event)) (code string, err error)
}

var ErrBusy = errors.New("a recording is already in progress")

// Recorder runs at most one recording session at a time.
type Recorder struct {
	ai     Agent
	manual Agent
	emit   func(Event)

	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(ai, manual Agent, emit func(Event)) *Recorder {
	return &Recorder{ai: ai, manual: manual, emit: emit}
}

func (r *Recorder) Start(req Request) error {
	if err := validate(req); err != nil {
		return err
	}

	r.mu.Lock()
	if r.cancel != nil {
		r.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.mu.Unlock()

	go r.run(ctx, req)
	return nil
}

// Stop asks the running session to finish. It is a no-op when idle.
func (r *Recorder) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *Recorder) run(ctx context.Context, req Request) {
	defer r.finish()

	agent := r.ai
	if req.Mode == ModeManual {
		agent = r.manual
	}

	r.emit(Event{Kind: EventStatus, Text: "recording"})
	code, err := agent.Record(ctx, req, r.emit)
	switch {
	case err != nil && ctx.Err() == nil:
		r.emit(Event{Kind: EventError, Text: err.Error()})
	case code != "":
		// Checked before the cancel case: stopping a manual recording still returns the code
		// for the steps captured so far.
		r.emit(Event{Kind: EventCode, Text: code})
	case ctx.Err() != nil:
		// The user stopped the session; that is not an error.
		r.emit(Event{Kind: EventStatus, Text: "stopped"})
	}
	r.emit(Event{Kind: EventDone})
}

func (r *Recorder) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func validate(req Request) error {
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid url %q", req.URL)
	}
	if !req.Language.Valid() {
		return fmt.Errorf("unsupported language %q", req.Language)
	}
	switch req.Mode {
	case "", ModeAI, ModeManual:
	default:
		return fmt.Errorf("unsupported mode %q", req.Mode)
	}
	return nil
}
