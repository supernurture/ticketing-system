package auth

import (
	"context"

	"gorm.io/gorm"
)

type User struct {
	ID           int64
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
	result := r.db.WithContext(ctx).Raw(`
		SELECT id, email, password_hash, role
		FROM users
		WHERE email = ?`, email).Scan(&user)
	if result.Error != nil {
		return User{}, false, result.Error
	}
	return user, result.RowsAffected > 0, nil
}
