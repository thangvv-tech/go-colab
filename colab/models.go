package colab

import "time"

type Accelerator string

const (
	AcceleratorNone Accelerator = "NONE"
	AcceleratorT4   Accelerator = "T4"
	AcceleratorL4   Accelerator = "L4"
	AcceleratorA100 Accelerator = "A100"
	AcceleratorH100 Accelerator = "H100"
	AcceleratorG4   Accelerator = "G4"
	AcceleratorV5E1 Accelerator = "V5E1"
	AcceleratorV6E1 Accelerator = "V6E1"
)

type Shape int

const (
	ShapeStandard Shape = 0
	ShapeHighRAM  Shape = 1
)

type AssignmentVariant int

const (
	VariantDefault AssignmentVariant = 0
	VariantGPU     AssignmentVariant = 1
	VariantTPU     AssignmentVariant = 2
)

type RuntimeProxyInfo struct {
	Token          string    `json:"token"`
	TokenExpiresIn int       `json:"tokenExpiresInSeconds"`
	URL            string    `json:"url"`
	ExpiresAt      time.Time `json:"-"`
}

type ListedAssignment struct {
	Accelerator      Accelerator       `json:"accelerator"`
	Endpoint         string            `json:"endpoint"`
	Variant          AssignmentVariant `json:"variant"`
	MachineShape     Shape             `json:"machineShape"`
	RuntimeProxyInfo RuntimeProxyInfo  `json:"runtimeProxyInfo"`
}

type Assignment struct {
	Endpoint         string           `json:"endpoint"`
	RuntimeProxyInfo RuntimeProxyInfo `json:"runtimeProxyInfo"`
}

type PostAssignmentResponse struct {
	Accelerator      Accelerator       `json:"accelerator"`
	Endpoint         string            `json:"endpoint"`
	RuntimeProxyInfo RuntimeProxyInfo  `json:"runtimeProxyInfo"`
	Variant          AssignmentVariant `json:"variant"`
}

type GetAssignmentResponse struct {
	Acc     string `json:"acc"`
	Nbh     string `json:"nbh"`
	Token   string `json:"token"`
	Variant string `json:"variant"`
}

// ColabError is returned by all API calls on non-2xx responses.
type ColabError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *ColabError) Error() string { return e.Message }

// TooManyAssignmentsError wraps HTTP 412.
type TooManyAssignmentsError struct{ Cause error }

func (e *TooManyAssignmentsError) Error() string {
	return "too many assignments: " + e.Cause.Error()
}
func (e *TooManyAssignmentsError) Unwrap() error { return e.Cause }
