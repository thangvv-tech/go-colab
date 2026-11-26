package colab

import (
	"context"
	"fmt"
)

// UsageInfo contains account-level Colab compute unit usage.
type UsageInfo struct {
	PaidComputeUnitsBalance float64 `json:"paidComputeUnitsBalance"`
	ConsumptionRateHourly   float64 `json:"consumptionRateHourly"`
	AssignmentsCount        int     `json:"assignmentsCount"`
}

// GetUsage returns the authenticated account's compute unit balance and usage rate.
func (c *Client) GetUsage(ctx context.Context) (*UsageInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("colab: client is required")
	}
	var response struct {
		CurrentBalance        float64 `json:"currentBalance"`
		ConsumptionRateHourly float64 `json:"consumptionRateHourly"`
		AssignmentsCount      int     `json:"assignmentsCount"`
	}
	if err := c.GetJSON(ctx, tunnelURL(c.env.Domain, "/ccu-info"), &response); err != nil {
		return nil, err
	}
	return &UsageInfo{
		PaidComputeUnitsBalance: response.CurrentBalance,
		ConsumptionRateHourly:   response.ConsumptionRateHourly,
		AssignmentsCount:        response.AssignmentsCount,
	}, nil
}
