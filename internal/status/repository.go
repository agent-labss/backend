package status

import "context"

type DatabasePinger interface {
	Ping(context.Context) error
}
