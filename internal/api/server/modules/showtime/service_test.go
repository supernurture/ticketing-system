package showtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An explicit 0 is rejected, not defaulted.
func TestListRejectsBadPaging(t *testing.T) {
	service := NewService(nil)
	tests := map[string]Filter{
		"page 0":         {Page: 0, Limit: defaultLimit},
		"page too large": {Page: maxPage + 1, Limit: defaultLimit},
		"limit 0":        {Page: 1, Limit: 0},
		"limit too big":  {Page: 1, Limit: maxLimit + 1},
	}
	for name, f := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := service.List(context.Background(), f)

			var invalid ValidationError
			if !errors.As(err, &invalid) {
				t.Errorf("error = %v, want a ValidationError", err)
			}
		})
	}
}

// Validation fails before the repository is used, so nil is enough.
func TestBuildRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	service := &Service{now: func() time.Time { return now }}
	valid := Input{MovieID: 1, StudioID: 1, StartAt: now.Add(time.Hour), Price: 50000}

	tests := map[string]struct {
		edit func(in *Input)
		want string
	}{
		"missing movie":    {func(in *Input) { in.MovieID = 0 }, "movie_id is required"},
		"missing studio":   {func(in *Input) { in.StudioID = 0 }, "studio_id is required"},
		"missing price":    {func(in *Input) { in.Price = 0 }, "price must be greater than 0"},
		"negative price":   {func(in *Input) { in.Price = -1 }, "price must be greater than 0"},
		"missing start_at": {func(in *Input) { in.StartAt = time.Time{} }, "start_at is required"},
		"start_at in past": {func(in *Input) { in.StartAt = now.Add(-time.Minute) }, "start_at must be in the future"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			in := valid
			tc.edit(&in)

			_, err := service.build(context.Background(), in)

			var invalid ValidationError
			if !errors.As(err, &invalid) || invalid.Message != tc.want {
				t.Errorf("error = %v, want ValidationError %q", err, tc.want)
			}
		})
	}
}
