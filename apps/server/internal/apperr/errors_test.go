package apperr

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCarriesKindMessageAndCause(t *testing.T) {
	cause := errors.New("dial tcp 127.0.0.1:3101")
	err := New(Upstream, "机台服务暂时不可用，请重试", cause)
	if err.Kind != Upstream || err.Message != "机台服务暂时不可用，请重试" {
		t.Fatalf("fields lost: %+v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause is unreachable")
	}
	want := "机台服务暂时不可用，请重试: dial tcp 127.0.0.1:3101"
	if err.Error() != want {
		t.Fatalf("got %q", err.Error())
	}
}

func TestErrorWithoutCausePrintsMessageOnly(t *testing.T) {
	err := New(NotFound, "机台或配方不存在", nil)
	if err.Error() != "机台或配方不存在" || err.Unwrap() != nil {
		t.Fatalf("got %q", err.Error())
	}
}

func TestKindOfFindsWrappedBusinessErrors(t *testing.T) {
	wrapped := fmt.Errorf("handler: %w", New(Parse, "配方解析失败", errors.New("bad row")))
	if kind, ok := KindOf(wrapped); !ok || kind != Parse {
		t.Fatalf("got %v %v", kind, ok)
	}
	if message := MessageOf(wrapped); message != "配方解析失败" {
		t.Fatalf("got %q", message)
	}
	if _, ok := KindOf(errors.New("plain")); ok {
		t.Fatal("plain errors have no kind")
	}
	if message := MessageOf(errors.New("plain detail")); message != "plain detail" {
		t.Fatalf("got %q", message)
	}
}
