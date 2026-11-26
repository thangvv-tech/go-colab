package colab_test

import (
	"errors"
	"testing"

	"github.com/thangvv-tech/go-colab/colab"
)

func TestAcceleratorConstants(t *testing.T) {
	cases := []struct {
		got  colab.Accelerator
		want string
	}{
		{colab.AcceleratorNone, "NONE"},
		{colab.AcceleratorT4, "T4"},
		{colab.AcceleratorL4, "L4"},
		{colab.AcceleratorA100, "A100"},
		{colab.AcceleratorH100, "H100"},
		{colab.AcceleratorG4, "G4"},
		{colab.AcceleratorV5E1, "V5E1"},
		{colab.AcceleratorV6E1, "V6E1"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("Accelerator %q: got %q", c.want, c.got)
		}
	}
}

func TestShapeConstants(t *testing.T) {
	if int(colab.ShapeStandard) != 0 {
		t.Errorf("ShapeStandard: want 0 got %d", colab.ShapeStandard)
	}
	if int(colab.ShapeHighRAM) != 1 {
		t.Errorf("ShapeHighRAM: want 1 got %d", colab.ShapeHighRAM)
	}
}

func TestColabErrorMessage(t *testing.T) {
	e := &colab.ColabError{StatusCode: 500, Message: "internal error", Body: "{}"}
	if e.Error() != "internal error" {
		t.Errorf("ColabError.Error(): got %q", e.Error())
	}
}

func TestTooManyAssignmentsErrorWraps(t *testing.T) {
	cause := &colab.ColabError{StatusCode: 412, Message: "quota exceeded"}
	wrapped := &colab.TooManyAssignmentsError{Cause: cause}
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is should find cause inside TooManyAssignmentsError")
	}
}
