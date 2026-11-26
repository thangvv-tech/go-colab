package colab_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thangvv-tech/go-colab/colab"
)

func TestGetUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tun/m/ccu-info" || r.URL.Query().Get("authuser") != "0" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		fmt.Fprint(w, `)]}'
{"currentBalance":12.5,"consumptionRateHourly":1.25,"assignmentsCount":2}`)
	}))
	defer server.Close()

	client := colab.NewClientFromHTTP(server.Client(), colab.Environment{Domain: server.URL})
	usage, err := client.GetUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.PaidComputeUnitsBalance != 12.5 || usage.ConsumptionRateHourly != 1.25 || usage.AssignmentsCount != 2 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}
