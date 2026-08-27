package routers

import (
	"context"

	"unifi-port-forward/pkg/ports"

	"github.com/filipowm/go-unifi/unifi"
)

type Router interface {
	AddPort(ctx context.Context, config PortConfig) error
	CheckPort(ctx context.Context, port ports.Spec, protocol string) (*unifi.PortForward, bool, error)
	RemovePort(ctx context.Context, config PortConfig) error
	UpdatePort(ctx context.Context, port ports.Spec, config PortConfig) error
	DeletePortForwardByID(ctx context.Context, ruleID string) error
	ListAllPortForwards(ctx context.Context) ([]*unifi.PortForward, error)
}

type PortConfig struct {
	Name      string
	Enabled   bool
	Interface string
	DstPort   ports.Spec // External port(s) (what users connect to)
	FwdPort   ports.Spec // Internal port(s) (what the service listens on)
	SrcIP     string
	DstIP     string
	Protocol  string
}
