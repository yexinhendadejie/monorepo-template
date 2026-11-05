// Package apperr 定义跨层的业务错误。
//
// 业务层只负责说明“出了什么错”，HTTP 层根据 Kind 决定状态码与响应体，
// 这样新增传输方式（gRPC、CLI）时不必复制错误语义。
package apperr

import (
	"errors"
	"fmt"
)

// Kind 是错误类别，决定调用方如何处置。
type Kind int

const (
	// Invalid 表示调用方给的参数不合法。
	Invalid Kind = iota
	// NotFound 表示机台或配方不存在。
	NotFound
	// Upstream 表示机台服务不可用或其返回内容不可信。
	Upstream
	// Parse 表示拿到了字节但无法解析成配方。
	Parse
)

// Error 携带面向用户的说明与底层原因。
type Error struct {
	Kind    Kind
	Message string
	Err     error
}

// New 构造一个业务错误；cause 可以为 nil。
func New(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: message, Err: cause}
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

// Unwrap 让 errors.Is/As 能穿透到原始错误。
func (e *Error) Unwrap() error { return e.Err }

// KindOf 返回错误链上第一个业务错误的类别。
func KindOf(err error) (Kind, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind, true
	}
	return Invalid, false
}

// MessageOf 返回业务错误里面向用户的说明，找不到时用底层错误的文本兜底。
func MessageOf(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Message
	}
	return err.Error()
}
