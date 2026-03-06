package session

import (
	"context"
	"errors"
)

var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID        string
	Title     string
	CreatedAt int64
	UpdatedAt int64
}

type Service interface {
	Create(ctx context.Context) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	Update(ctx context.Context, sess Session) error
	Delete(ctx context.Context, id string) error
	Current(ctx context.Context) (Session, error)
	SetCurrent(ctx context.Context, id string) error
}
