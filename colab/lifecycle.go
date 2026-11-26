package colab

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// RuntimeStatus summarizes persisted runtime metadata.
type RuntimeStatus struct {
	Session  *Session
	Hardware string
	Shape    string
	Variant  RuntimeVariant
	State    string
}

// Status returns local persisted metadata for name, or the sole active session.
func (m *RuntimeManager) Status(_ context.Context, name string) (*RuntimeStatus, error) {
	if m == nil || m.Store == nil {
		return nil, fmt.Errorf("colab: runtime status requires a session store")
	}
	session, err := m.Store.Resolve(name)
	if err != nil {
		return nil, err
	}
	hardware := string(session.Accel)
	if hardware == "" || session.Accel == AcceleratorNone {
		hardware = "CPU"
	}
	shape := "Standard"
	if session.Shape == ShapeHighRAM {
		shape = "High-RAM"
	}
	return &RuntimeStatus{Session: session, Hardware: hardware, Shape: shape, Variant: defaultVariant(session.Accel), State: "IDLE"}, nil
}

// RuntimeURL returns browser URL for a runtime session.
func (m *RuntimeManager) RuntimeURL(session *Session) (string, error) {
	if m == nil || m.Client == nil || session == nil || session.Endpoint == "" {
		return "", fmt.Errorf("colab: runtime URL requires client and session endpoint")
	}
	base, err := url.Parse(m.Client.env.Domain)
	if err != nil {
		return "", fmt.Errorf("colab: parse Colab domain: %w", err)
	}
	endpoint := url.PathEscape(session.Endpoint)
	base.Path = "/notebooks/empty.ipynb"
	query := url.Values{"dbu": {"/tun/m/" + session.Endpoint}}
	base.RawQuery = query.Encode()
	base.Fragment = "datalabBackendUrl=" + url.QueryEscape(strings.TrimRight(m.Client.env.Domain, "/")+"/tun/m/"+endpoint)
	return base.String(), nil
}

// StopAll releases every locally tracked runtime. It returns first failure after attempting all.
func (m *RuntimeManager) StopAll(ctx context.Context) error {
	if m == nil || m.Store == nil {
		return fmt.Errorf("colab: stop all requires a session store")
	}
	sessions, err := m.Store.Load()
	if err != nil {
		return err
	}
	var first error
	for _, session := range sessions {
		if err := m.Unassign(ctx, session.Endpoint); err != nil && first == nil {
			first = err
		}
	}
	return first
}
