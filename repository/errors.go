package repository

import "errors"

var (
	// ErrNilID 汎用エラー 引数のIDがNilです
	ErrNilID = errors.New("nil id")
	// ErrNotFound 汎用エラー 見つかりません
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists 汎用エラー 既に存在しています
	ErrAlreadyExists = errors.New("already exists")
	// ErrForbidden 汎用エラー 禁止されています
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidArgs 汎用エラー 引数が不正
	ErrInvalidArgs = errors.New("invalid args")
)

// ArgumentError 引数エラー
type ArgumentError struct {
	FieldName string
	Message   string
}

// Error Messageを返します
func (ae *ArgumentError) Error() string {
	return ae.Message
}

// ArgError 引数エラーを発生させます
func ArgError(field, message string) *ArgumentError {
	return &ArgumentError{FieldName: field, Message: message}
}
