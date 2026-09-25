package messaging

import "time"

// ContractDuration is the validity of the node→participant relay contract,
// mirroring apphost's RegisterDuration.
const ContractDuration = 10 * 365 * 24 * time.Hour

type Config struct {
	// HostingDuration is the validity of the hosting contract a mailbox is
	// provisioned under.
	//
	// note: nothing renews a hosting contract. A mailbox whose contract expires
	// stops being served, and its stored mail stays.
	HostingDuration time.Duration `yaml:"hosting_duration,omitempty"`

	// TokenDuration is the validity of the access token issued to a new
	// participant when create_identity names no duration.
	TokenDuration time.Duration `yaml:"token_duration,omitempty"`

	// DeliveryTimeout bounds one delivery, one receipt, and the read of either
	// on the answering side.
	DeliveryTimeout time.Duration `yaml:"delivery_timeout,omitempty"`

	// WaitDefault is the window a wait that names none parks for, and WaitMax
	// the most any ask is granted.
	//
	// why the deployment names both: what caps a held call is the client's own
	// request timeout and any proxy in front of it, which the node cannot know.
	WaitDefault time.Duration `yaml:"wait_default,omitempty"`
	WaitMax     time.Duration `yaml:"wait_max,omitempty"`

	// MaxPayloadBytes bounds a message body, on the way out and on the way in.
	MaxPayloadBytes int `yaml:"max_payload_bytes,omitempty"`

	// MaxReadBytes bounds the message bodies one read answers.
	MaxReadBytes int `yaml:"max_read_bytes,omitempty"`
}

var defaultConfig = Config{
	HostingDuration: ContractDuration,
	TokenDuration:   365 * 24 * time.Hour,
	DeliveryTimeout: 15 * time.Second,
	// why two minutes and fifteen: both sit under the untuned request caps of
	// the surveyed MCP clients, and under the MCP endpoint's thirty-minute
	// session.
	WaitDefault:     2 * time.Minute,
	WaitMax:         15 * time.Minute,
	MaxPayloadBytes: 64 << 10,
	MaxReadBytes:    64 << 10,
}
