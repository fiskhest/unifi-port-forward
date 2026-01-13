package routers

import (
	"context"

	"github.com/filipowm/go-unifi/unifi"
)

type Router interface {
	AddPort(ctx context.Context, config PortConfig) error
	CheckPort(ctx context.Context, port int, protocol string) (*unifi.PortForward, bool, error)
	RemovePort(ctx context.Context, config PortConfig) error
	UpdatePort(ctx context.Context, port int, config PortConfig) error
	ListAllPortForwards(ctx context.Context) ([]*unifi.PortForward, error)
}

type PortConfig struct {
	Name      string
	Enabled   bool
	Interface string
	DstPort   int // External port (what users connect to)
	FwdPort   int // Internal port (what service listens on)
	SrcIP     string
	DstIP     string
	Protocol  string
}
