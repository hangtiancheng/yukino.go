package domain

// User is the user entity. PasswordHash stores the bcrypt hash and is never serialized to JSON.
// A pure domain object with no gorm tags (the persistence structure is privately defined in the infrastructure layer).
type User struct {
	ID           uint64
	Username     string
	PasswordHash string
	Email        string
	CreatedAt    int64
	UpdatedAt    int64
}
