package auth

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type User struct {
	ID           int64 `gorm:"primaryKey"`
	Email        string
	PasswordHash string
	Role         string
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// FindByEmail returns the user, or ok=false when no user has that email.
func (r *Repository) FindByEmail(ctx context.Context, email string) (user User, ok bool, err error) {
	err = r.db.WithContext(ctx).Where("email = ?", email).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, false, nil
	}
	return user, err == nil, err
}
