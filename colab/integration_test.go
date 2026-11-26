//go:build integration

package colab_test

import (
	"context"
	"os"
	"testing"

	"github.com/thangvv-tech/go-colab/colab"
)

func TestRuntimeRoundTrip(t *testing.T) {
	if os.Getenv("COLAB_INTEGRATION") != "1" {
		t.Skip("set COLAB_INTEGRATION=1 to allocate a live Colab runtime")
	}
	ctx := context.Background()
	client, err := colab.NewClient(ctx, colab.NewADCAuth(), colab.ProdEnv)
	if err != nil {
		t.Fatal(err)
	}
	manager := colab.NewRuntimeManager(client, nil)
	session, err := manager.Assign(ctx, colab.AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := manager.Unassign(context.WithoutCancel(ctx), session.Endpoint); err != nil {
			t.Error(err)
		}
	}()
	result, err := colab.NewExecutor(session, nil).Exec(ctx, "print('go-colab integration ok')")
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "go-colab integration ok\n" || result.Error != "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
