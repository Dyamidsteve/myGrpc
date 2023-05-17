package mygrpc

import (
	"fmt"
	"testing"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func _assert(condition bool, msg string, v ...any) {
	if !condition {
		panic(fmt.Sprintf("assertion failed:"+msg, v...))
	}

}

func TestNewServer(t *testing.T) {
	var foo Foo
	s := NewServer(&foo)
	_assert(len(s.method) == 1, "wrong service Method expect 1,but got %d", len(s.method))
	mType := s.method["Sum"]
	_assert(mType != nil, "wrong method,sum shouldnt be nil")

}
