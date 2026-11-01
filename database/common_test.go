package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestRenderDaoErrorClassifiesKinds(t *testing.T) {
	ctx := context.Background()

	canceled := renderDaoError(ctx, 1, "op", fmt.Errorf("query: %w", context.Canceled), "x")
	assert.True(t, IsCanceled(canceled))
	assert.False(t, IsNotFound(canceled))

	assert.True(t, IsNotFound(renderDaoError(ctx, 1, "op", pgx.ErrNoRows, "x")))
	assert.True(t, IsConflict(renderDaoError(ctx, 1, "op", ErrConflict, "x")))

	other := renderDaoError(ctx, 1, "op", fmt.Errorf("boom"), "x")
	assert.False(t, IsCanceled(other) || IsNotFound(other) || IsConflict(other))
}
