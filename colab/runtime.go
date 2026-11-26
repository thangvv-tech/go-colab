package colab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RuntimeVariant tells Colab which runtime family to allocate.
type RuntimeVariant string

const (
	// RuntimeVariantDefault requests a CPU runtime.
	RuntimeVariantDefault RuntimeVariant = "DEFAULT"
	// RuntimeVariantGPU requests a GPU runtime.
	RuntimeVariantGPU RuntimeVariant = "GPU"
	// RuntimeVariantTPU requests a TPU runtime.
	RuntimeVariantTPU RuntimeVariant = "TPU"
)

// AssignOptions configures a Colab runtime allocation.
type AssignOptions struct {
	Name         string
	NotebookHash string
	Variant      RuntimeVariant
	Accelerator  Accelerator
	HighMemory   bool
}

// RuntimeManager manages Colab runtime allocations.
type RuntimeManager struct {
	Client *Client
	Store  *SessionStore
	Mode   APIMode
}

var assignRetryDelay = func(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// NewRuntimeManager creates a manager using client and optional local session store.
func NewRuntimeManager(client *Client, store *SessionStore) *RuntimeManager {
	return &RuntimeManager{Client: client, Store: store, Mode: APIModeTunnel}
}

// UUIDToWebSafeBase64 converts a canonical UUID to Colab's web-safe notebook hash.
func UUIDToWebSafeBase64(id string) (string, error) {
	if len(id) != 36 {
		return "", fmt.Errorf("colab: notebook hash must be a canonical UUID")
	}
	for i := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if id[i] != '-' {
				return "", fmt.Errorf("colab: notebook hash must be a canonical UUID")
			}
			continue
		}
		if !isHex(id[i]) {
			return "", fmt.Errorf("colab: notebook hash must be a canonical UUID")
		}
	}

	encoded := strings.ReplaceAll(id, "-", "_")
	return encoded + strings.Repeat(".", 44-len(encoded)), nil
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func randomUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("colab: generate notebook UUID: %w", err)
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(bytes[:4]), hex.EncodeToString(bytes[4:6]), hex.EncodeToString(bytes[6:8]), hex.EncodeToString(bytes[8:10]), hex.EncodeToString(bytes[10:])), nil
}

// List returns active runtime assignments from Colab.
func (m *RuntimeManager) List(ctx context.Context) ([]ListedAssignment, error) {
	if m == nil || m.Client == nil {
		return nil, fmt.Errorf("colab: runtime manager has no client")
	}

	if m.Mode == APIModePublic {
		return m.listPublic(ctx)
	}
	var response struct {
		Assignments []ListedAssignment `json:"assignments"`
	}
	if err := m.Client.GetJSON(ctx, tunnelURL(m.Client.env.Domain, "/assignments"), &response); err != nil {
		return nil, err
	}
	return response.Assignments, nil
}

// Assign allocates or reconnects to a Colab runtime and optionally persists it.
func (m *RuntimeManager) Assign(ctx context.Context, opts AssignOptions) (*Session, error) {
	if m == nil || m.Client == nil {
		return nil, fmt.Errorf("colab: runtime manager has no client")
	}
	if m.Mode == APIModePublic {
		return nil, fmt.Errorf("colab: public API mode does not support runtime allocation")
	}

	if opts.NotebookHash == "" {
		id, err := randomUUID()
		if err != nil {
			return nil, err
		}
		opts.NotebookHash = id
	}
	notebookHash, err := UUIDToWebSafeBase64(opts.NotebookHash)
	if err != nil {
		return nil, err
	}

	accelerator := opts.Accelerator
	if accelerator == "" {
		accelerator = AcceleratorNone
	}
	variant := opts.Variant
	if variant == "" {
		variant = defaultVariant(accelerator)
	}

	assignURL, err := assignmentURL(m.Client.env.Domain, notebookHash, variant, accelerator, opts.HighMemory)
	if err != nil {
		return nil, err
	}

	var endpoint string
	var proxy RuntimeProxyInfo
	resolvedAccelerator := accelerator
	for attempt := 0; ; attempt++ {
		var raw json.RawMessage
		if err := m.Client.GetJSON(ctx, assignURL, &raw); err != nil {
			return nil, err
		}
		var probe struct {
			Accelerator Accelerator `json:"accelerator"`
			Endpoint    string      `json:"endpoint"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("colab: decode assignment response: %w", err)
		}
		if probe.Endpoint != "" {
			var assignment Assignment
			if err := json.Unmarshal(raw, &assignment); err != nil {
				return nil, fmt.Errorf("colab: decode existing assignment: %w", err)
			}
			endpoint, proxy = assignment.Endpoint, assignment.RuntimeProxyInfo
			if probe.Accelerator != "" {
				resolvedAccelerator = probe.Accelerator
			}
			break
		}
		var get GetAssignmentResponse
		if err := json.Unmarshal(raw, &get); err != nil {
			return nil, fmt.Errorf("colab: decode assignment token: %w", err)
		}
		if get.Token == "" {
			return nil, fmt.Errorf("colab: assignment response missing XSRF token")
		}
		var post PostAssignmentResponse
		err := m.Client.postWithTokenOnce(ctx, assignURL, get.Token, &post)
		if err == nil {
			endpoint, proxy = post.Endpoint, post.RuntimeProxyInfo
			if post.Accelerator != "" {
				resolvedAccelerator = post.Accelerator
			}
			break
		}
		var requestErr *ColabError
		if errors.As(err, &requestErr) && requestErr.StatusCode == http.StatusPreconditionFailed {
			return nil, &TooManyAssignmentsError{Cause: err}
		}
		if !retryableAssignmentError(err) || attempt >= 3 {
			return nil, err
		}
		if err := assignRetryDelay(ctx, 10*time.Second*(1<<attempt)); err != nil {
			return nil, err
		}
	}
	if endpoint == "" {
		return nil, fmt.Errorf("colab: assignment response missing endpoint")
	}

	now := time.Now()
	session := &Session{
		Name:      opts.Name,
		Endpoint:  endpoint,
		URL:       proxy.URL,
		Token:     proxy.Token,
		TokenExp:  now.Add(time.Duration(proxy.TokenExpiresIn) * time.Second),
		Shape:     requestedShape(accelerator, opts.HighMemory),
		Accel:     resolvedAccelerator,
		CreatedAt: now,
	}
	if session.Name == "" {
		session.Name = endpoint
	}
	if m.Store != nil {
		if err := m.Store.Add(session); err != nil {
			return nil, fmt.Errorf("colab: persist session: %w", err)
		}
	}
	return session, nil
}

// Unassign terminates a Colab runtime and removes matching local state after success.
func (m *RuntimeManager) Unassign(ctx context.Context, endpoint string) error {
	if m == nil || m.Client == nil {
		return fmt.Errorf("colab: runtime manager has no client")
	}
	if endpoint == "" {
		return fmt.Errorf("colab: endpoint is required")
	}
	if m.Mode == APIModePublic {
		return fmt.Errorf("colab: public API mode does not support runtime release")
	}

	rawURL := tunnelURL(m.Client.env.Domain, "/unassign/"+url.PathEscape(endpoint))
	var get GetAssignmentResponse
	if err := m.Client.GetJSON(ctx, rawURL, &get); err != nil {
		return err
	}
	if get.Token == "" {
		return fmt.Errorf("colab: unassign response missing XSRF token")
	}
	if err := m.Client.PostWithToken(ctx, rawURL, get.Token, nil); err != nil {
		return err
	}
	if m.Store != nil {
		if err := m.removeStoredEndpoint(endpoint); err != nil {
			return fmt.Errorf("colab: remove session: %w", err)
		}
	}
	return nil
}

func (m *RuntimeManager) listPublic(ctx context.Context) ([]ListedAssignment, error) {
	var response struct {
		Runtimes []struct {
			Name           string `json:"name"`
			ConnectionInfo struct {
				ExpireTime string `json:"expireTime"`
				Endpoint   string `json:"endpoint"`
				URL        string `json:"url"`
				Token      string `json:"token"`
			} `json:"connectionInfo"`
			RuntimeSpec struct {
				Variant     string `json:"variant"`
				Accelerator string `json:"accelerator"`
				Shape       string `json:"shape"`
			} `json:"runtimeSpec"`
		} `json:"runtimes"`
	}
	if err := m.Client.GetJSON(ctx, strings.TrimRight(m.Client.env.PublicAPI, "/")+"/v1beta/runtimes", &response); err != nil {
		return nil, err
	}
	assignments := make([]ListedAssignment, 0, len(response.Runtimes))
	for _, runtime := range response.Runtimes {
		endpoint := runtime.ConnectionInfo.Endpoint
		if endpoint == "" {
			endpoint = strings.TrimPrefix(runtime.Name, "runtimes/")
		}
		accelerator := Accelerator(runtime.RuntimeSpec.Accelerator)
		switch accelerator {
		case AcceleratorNone, AcceleratorG4, AcceleratorT4, AcceleratorL4, AcceleratorA100, AcceleratorH100, AcceleratorV5E1, AcceleratorV6E1:
		default:
			accelerator = AcceleratorNone
		}
		variant := VariantDefault
		if runtime.RuntimeSpec.Variant == "VARIANT_GPU" {
			variant = VariantGPU
		} else if runtime.RuntimeSpec.Variant == "VARIANT_TPU" {
			variant = VariantTPU
		}
		shape := ShapeStandard
		if runtime.RuntimeSpec.Shape == "SHAPE_HIGHMEM" {
			shape = ShapeHighRAM
		}
		expiresIn := 3600
		if expires, err := time.Parse(time.RFC3339, runtime.ConnectionInfo.ExpireTime); err == nil {
			expiresIn = max(0, int(time.Until(expires).Seconds()))
		}
		assignments = append(assignments, ListedAssignment{
			Accelerator:      accelerator,
			Endpoint:         endpoint,
			Variant:          variant,
			MachineShape:     shape,
			RuntimeProxyInfo: RuntimeProxyInfo{Token: runtime.ConnectionInfo.Token, TokenExpiresIn: expiresIn, URL: runtime.ConnectionInfo.URL},
		})
	}
	return assignments, nil
}

func (m *RuntimeManager) removeStoredEndpoint(endpoint string) error {
	sessions, err := m.Store.Load()
	if err != nil {
		return err
	}
	for name, session := range sessions {
		if session.Endpoint == endpoint {
			return m.Store.Remove(name)
		}
	}
	return nil
}

func defaultVariant(accelerator Accelerator) RuntimeVariant {
	switch accelerator {
	case AcceleratorV5E1, AcceleratorV6E1:
		return RuntimeVariantTPU
	case "", AcceleratorNone:
		return RuntimeVariantDefault
	default:
		return RuntimeVariantGPU
	}
}

func requestedShape(accelerator Accelerator, highMemory bool) Shape {
	if highMemory && accelerator != AcceleratorL4 && accelerator != AcceleratorV5E1 && accelerator != AcceleratorV6E1 {
		return ShapeHighRAM
	}
	return ShapeStandard
}

func assignmentURL(domain, notebookHash string, variant RuntimeVariant, accelerator Accelerator, highMemory bool) (string, error) {
	u, err := url.Parse(tunnelURL(domain, "/assign"))
	if err != nil {
		return "", fmt.Errorf("colab: invalid tunnel domain: %w", err)
	}
	q := u.Query()
	q.Set("nbh", notebookHash)
	q.Set("variant", string(variant))
	if accelerator != AcceleratorNone {
		q.Set("accelerator", string(accelerator))
	}
	if requestedShape(accelerator, highMemory) == ShapeHighRAM {
		q.Set("shape", "hm")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func tunnelURL(domain, path string) string {
	return strings.TrimRight(domain, "/") + tunnelEndpoint + path
}

func retryableAssignmentError(err error) bool {
	var requestErr *ColabError
	if errors.As(err, &requestErr) {
		return requestErr.StatusCode == http.StatusBadGateway || requestErr.StatusCode == http.StatusServiceUnavailable || requestErr.StatusCode == http.StatusGatewayTimeout
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}
