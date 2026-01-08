package static

import (
	"context"

	externalprovider "github.com/cloudoperators/repo-guard/internal/external-provider"
)

type StaticClient struct {
	Groups map[string][]string
}

func NewStaticClient(groups map[string][]string) externalprovider.ExternalProvider {
	return &StaticClient{Groups: groups}
}

func (c *StaticClient) Users(ctx context.Context, group string) ([]string, error) {
	if members, ok := c.Groups[group]; ok {
		return append([]string{}, members...), nil
	}

	return []string{}, nil
}

func (c *StaticClient) TestConnection(ctx context.Context) error { return nil }
