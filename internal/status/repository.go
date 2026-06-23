package status

import "context"

type DatabasePinger interface {
	Ping(ctx context.Context) error
}
