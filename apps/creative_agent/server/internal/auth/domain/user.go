package domain

// User 用户实体。PasswordHash 存 bcrypt 哈希，永不序列化到 JSON。
// 纯领域对象，不含任何 gorm 标签（持久化结构在 infrastructure 层私有定义）。
type User struct {
	ID           uint64
	Username     string
	PasswordHash string
	Email        string
	CreatedAt    int64
	UpdatedAt    int64
}
