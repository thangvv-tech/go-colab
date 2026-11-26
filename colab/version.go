package colab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// PaymentURL is the Colab subscription management page.
const PaymentURL = "https://colab.research.google.com/signup"

// CheckLatestVersion reads a Go-colab release version from a caller-provided JSON endpoint.
// The endpoint must return {"version":"x.y.z"}. No default endpoint exists because
// go-colab is independent from the upstream Python package and its PyPI releases.
func CheckLatestVersion(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	if endpoint == "" {
		return "", fmt.Errorf("colab: release endpoint is not configured")
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("colab: version check: %s", response.Status)
	}
	var payload struct {
		Version string `json:"version"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Version != "" {
		return payload.Version, nil
	}
	if payload.Info.Version != "" {
		return payload.Info.Version, nil
	}
	return "", fmt.Errorf("colab: version endpoint returned no version")
}

// IsNewerVersion compares dotted numeric semantic versions. Unknown version formats differ by string.
func IsNewerVersion(current, candidate string) bool {
	currentParts, currentOK := versionParts(current)
	candidateParts, candidateOK := versionParts(candidate)
	if !currentOK || !candidateOK {
		return current != candidate
	}
	length := max(len(currentParts), len(candidateParts))
	for i := range length {
		var old, next int
		if i < len(currentParts) {
			old = currentParts[i]
		}
		if i < len(candidateParts) {
			next = candidateParts[i]
		}
		if next != old {
			return next > old
		}
	}
	return false
}

func versionParts(value string) ([]int, bool) {
	if value == "" {
		return nil, false
	}
	segments := strings.Split(strings.TrimPrefix(value, "v"), ".")
	parts := make([]int, len(segments))
	for i, segment := range segments {
		parsed, err := strconv.Atoi(segment)
		if err != nil {
			return nil, false
		}
		parts[i] = parsed
	}
	return parts, true
}
