package routers

import (
	"context"

	"github.com/filipowm/go-unifi/unifi"
)

type Router interface {
	AddPort(ctx context.Context, config PortConfig) error
	CheckPort(ctx context.Context, port int) (*unifi.PortForward, bool, error)
	RemovePort(ctx context.Context, config PortConfig) error
	UpdatePort(ctx context.Context, port int, config PortConfig) error
}

type PortConfig struct {
	Name      string
	Enabled   bool
	Interface string
	SrcPort   int // External port (what users connect to)
	DstPort   int // Internal port (what service listens on)
	FwdPort   int // The forwarded port
	SrcIP     string
	DstIP     string
	Protocol  string
}
