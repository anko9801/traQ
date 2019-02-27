package model

import (
	"github.com/satori/go.uuid"
	"github.com/traPtitech/traQ/utils/validator"
	"time"
)

// Message データベースに格納するmessageの構造体
type Message struct {
	ID        uuid.UUID  `gorm:"type:char(36);not null;primary_key"`
	UserID    uuid.UUID  `gorm:"type:char(36);not null;"`
	ChannelID uuid.UUID  `gorm:"type:char(36);not null;index"`
	Text      string     `gorm:"type:text;not null"                 validate:"required"`
	CreatedAt time.Time  `gorm:"precision:6;not null;index"`
	UpdatedAt time.Time  `gorm:"precision:6;not null"`
	DeletedAt *time.Time `gorm:"precision:6;index"`
}

// TableName DBの名前を指定するメソッド
func (m *Message) TableName() string {
	return "messages"
}

// Validate 構造体を検証します
func (m *Message) Validate() error {
	return validator.ValidateStruct(m)
}

// Unread 未読レコード
type Unread struct {
	UserID    uuid.UUID `gorm:"type:char(36);not null;primary_key"`
	MessageID uuid.UUID `gorm:"type:char(36);not null;primary_key"`
	CreatedAt time.Time `gorm:"precision:6;not null"`
}

// TableName テーブル名
func (unread *Unread) TableName() string {
	return "unreads"
}

// ArchivedMessage 編集前のアーカイブ化されたメッセージの構造体
type ArchivedMessage struct {
	ID        uuid.UUID `gorm:"type:char(36);not null;primary_key"`
	MessageID uuid.UUID `gorm:"type:char(36);not null;index"`
	UserID    uuid.UUID `gorm:"type:char(36);not null"`
	Text      string    `gorm:"type:text;not null"`
	DateTime  time.Time `gorm:"precision:6;not null"`
}

// TableName ArchivedMessage構造体のテーブル名
func (am *ArchivedMessage) TableName() string {
	return "archived_messages"
}
