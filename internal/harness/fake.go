package harness

import "io"

// FakeHarness is a programmable harness for use in tests across
// packages. Recorded call slices let assertions inspect what the
// runner passed in; configured fields drive the responses.
type FakeHarness struct {
	IDValue      ID
	Argv         []string
	Stdin        io.Reader
	BuildErr     error
	Events       []Event
	ParseErr     error
	AuthEnvValue map[string]string
	AuthErr      error
	Caps         Capabilities

	BuildCalls []Request
	ParseCalls [][]byte
	AuthCalls  []AuthConfig
}

// ID returns the configured fake ID.
func (f *FakeHarness) ID() ID { return f.IDValue }

// BuildArgv records the request and returns the configured argv / stdin / error.
func (f *FakeHarness) BuildArgv(req Request) ([]string, io.Reader, error) {
	f.BuildCalls = append(f.BuildCalls, req)
	return f.Argv, f.Stdin, f.BuildErr
}

// ParseEvent records the raw line and returns the next configured
// Event from Events, or an empty Event if exhausted. ParseErr takes
// precedence when non-nil.
func (f *FakeHarness) ParseEvent(raw []byte) (Event, error) {
	f.ParseCalls = append(f.ParseCalls, raw)
	if f.ParseErr != nil {
		return Event{}, f.ParseErr
	}
	if len(f.Events) == 0 {
		return Event{}, nil
	}
	next := f.Events[0]
	f.Events = f.Events[1:]
	return next, nil
}

// AuthEnv records the config and returns the configured env / error.
func (f *FakeHarness) AuthEnv(cfg AuthConfig) (map[string]string, error) {
	f.AuthCalls = append(f.AuthCalls, cfg)
	return f.AuthEnvValue, f.AuthErr
}

// Capabilities returns the configured capability matrix.
func (f *FakeHarness) Capabilities() Capabilities { return f.Caps }
