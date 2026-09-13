package metabol

import (
	"context"
	"errors"
	"fmt"
)

func contextFailure(ctx context.Context, err error) (error, bool) {
	ctxErr := ctx.Err()
	if ctxErr == nil {
		return err, false
	}
	if err == nil {
		return ctxErr, true
	}
	if errors.Is(err, ctxErr) {
		return err, true
	}
	return fmt.Errorf("%w (%w)", err, ctxErr), true
}
