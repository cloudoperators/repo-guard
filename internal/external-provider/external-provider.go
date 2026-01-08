package externalprovider

import "context"

type ExternalProvider interface {
	Users(ctx context.Context, group string) ([]string, error)
	TestConnection(ctx context.Context) error
}
