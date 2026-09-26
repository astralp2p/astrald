package messaging

import "time"

// ContractDuration is the validity of the node→participant relay contract,
// mirroring apphost's RegisterDuration.
const ContractDuration = 10 * 365 * 24 * time.Hour

type Config struct {
	// HostingDuration is the validity of the hosting contract a mailbox is
	// provisioned or renewed under. A quarter of it is the renewal window. Zero
	// or less takes the default.
	HostingDuration time.Duration `yaml:"hosting_duration,omitempty"`

	// RenewalInterval is how often the renewal pass runs after the one at Run.
	// Zero or less takes the default.
	//
	// note: an interval over a quarter of HostingDuration lets a contract lapse
	// between two passes.
	RenewalInterval time.Duration `yaml:"renewal_interval,omitempty"`

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
	RenewalInterval: 24 * time.Hour,
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

// withHostingDefaults answers the config with a HostingDuration or a
// RenewalInterval of zero or less set to its default.
//
// why hosting_duration: a contract signed for no time is born expired and stays
// inside the renewal window, so every pass would sign, index and store another
// one for every mailbox.
//
// why renewal_interval: the renewal pass ticks on it, and a ticker takes only a
// positive interval.
func (c Config) withHostingDefaults() Config {
	if c.HostingDuration <= 0 {
		c.HostingDuration = defaultConfig.HostingDuration
	}
	if c.RenewalInterval <= 0 {
		c.RenewalInterval = defaultConfig.RenewalInterval
	}
	return c
}
