package loader

import (
	"context"
)

type Loader interface {
	GetMedia(ctx context.Context, key string) ([]byte, error)
}
