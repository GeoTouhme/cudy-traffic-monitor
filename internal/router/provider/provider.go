// Package provider defines read-only router contracts. A Snapshot must represent
// one coherent observation, with stable identifiers and explicit counter scope.
package provider

import (
	"context"
	"errors"
	"time"
)

type Code string

const (
	Unsupported  Code = "unsupported"
	Unvalidated  Code = "unvalidated_firmware"
	Unauthorized Code = "unauthorized"
	Unavailable  Code = "unavailable"
	InvalidData  Code = "invalid_data"
	Internal     Code = "internal"
	Canceled     Code = "canceled"
	Timeout      Code = "timeout"
)

// Error deliberately excludes underlying transport messages, URLs and bodies:
// those can contain credentials. Log CodeOf(err), never raw router responses.
type Error struct {
	Code      Code
	Operation string
}

func (e *Error) Error() string                   { return e.Operation + ": " + string(e.Code) }
func NewError(code Code, operation string) error { return &Error{code, operation} }
func CodeOf(err error) Code {
	if errors.Is(err, context.Canceled) {
		return Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}

type Support struct {
	Supported bool   `json:"supported"`
	Evidence  string `json:"evidence"`
}
type Capabilities struct {
	Mode           string  `json:"mode"`
	Simulated      bool    `json:"simulated"`
	Authentication Support `json:"authentication"`
	Identification Support `json:"identification"`
	Devices        Support `json:"devices"`
	Counters       Support `json:"counters"`
	WANOnly        Support `json:"wan_only"`
	Health         Support `json:"health"`
	Recovery       Support `json:"session_recovery"`
}
type Info struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Firmware string `json:"firmware"`
	BootID   string `json:"boot_id"` // Empty means reboot continuity is unknown.
}
type Device struct {
	ID   string `json:"id"` // Provider stable ID; never use display name as identity.
	Name string `json:"name"`
}
type Counter struct {
	ID                string `json:"id"`
	Scope             string `json:"scope"` // simulated, unknown, interface, or wan
	Unit              string `json:"unit"`
	Width             uint8  `json:"width_bits"` // 0 (unknown), 32, or 64
	Rollover          bool   `json:"rollover_supported"`
	MaxBytesPerSecond uint64 `json:"max_bytes_per_second"`
}
type Reading struct {
	DeviceID      string  `json:"device_id"`
	Counter       Counter `json:"counter"`
	Epoch         string  `json:"epoch"` // Changes on reset. Empty cannot prove rollover.
	RX            *uint64 `json:"rx_bytes"`
	TX            *uint64 `json:"tx_bytes"`
	MissingReason string  `json:"missing_reason,omitempty"`
}
type Snapshot struct {
	At       time.Time `json:"at"`
	Router   Info      `json:"router"`
	Devices  []Device  `json:"devices"` // Complete inventory; omission means absent.
	Readings []Reading `json:"readings"`
}

// Providers must honor contexts, serialize their session state, avoid all
// configuration mutations and return sanitized typed errors. Methods other
// than Capabilities and Close require Connect. Read includes identification,
// inventory and counters so the collector need not poll each separately.
type Provider interface {
	Capabilities() Capabilities
	Connect(context.Context) error
	Identify(context.Context) (Info, error)
	Devices(context.Context) ([]Device, error)
	Counters(context.Context) ([]Counter, error)
	Read(context.Context) (Snapshot, error)
	Health(context.Context) error
	Recover(context.Context) error
	Close() error
}
