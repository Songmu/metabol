package metabol

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestContextFailure(t *testing.T) {
	t.Parallel()

	operationErr := errors.New("operation failed")
	tests := []struct {
		name         string
		ctxErr       error
		err          error
		wantCanceled bool
		wantErrs     []error
	}{
		{
			name: "active context without operation error",
		},
		{
			name:     "active context with operation error",
			err:      operationErr,
			wantErrs: []error{operationErr},
		},
		{
			name:         "canceled without operation error",
			ctxErr:       context.Canceled,
			wantCanceled: true,
			wantErrs:     []error{context.Canceled},
		},
		{
			name:         "canceled with operation error",
			ctxErr:       context.Canceled,
			err:          operationErr,
			wantCanceled: true,
			wantErrs:     []error{operationErr, context.Canceled},
		},
		{
			name:         "deadline exceeded with operation error",
			ctxErr:       context.DeadlineExceeded,
			err:          operationErr,
			wantCanceled: true,
			wantErrs:     []error{operationErr, context.DeadlineExceeded},
		},
		{
			name:         "operation error already wraps cancellation",
			ctxErr:       context.Canceled,
			err:          fmt.Errorf("operation failed: %w", context.Canceled),
			wantCanceled: true,
			wantErrs:     []error{context.Canceled},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := &mutableErrorContext{
				Context: context.Background(),
				err:     tt.ctxErr,
			}
			got, canceled := contextFailure(ctx, tt.err)

			if canceled != tt.wantCanceled {
				t.Fatalf("contextFailure canceled = %t, want %t", canceled, tt.wantCanceled)
			}
			if tt.err == nil && tt.ctxErr == nil {
				if got != nil {
					t.Fatalf("contextFailure error = %v, want nil", got)
				}
				return
			}
			for _, wantErr := range tt.wantErrs {
				if !errors.Is(got, wantErr) {
					t.Errorf("contextFailure error = %v, want wrapping %v", got, wantErr)
				}
			}
			if tt.ctxErr != nil {
				assertCount(t, got.Error(), tt.ctxErr.Error(), 1)
			}
		})
	}
}
