package mygrpc

import (
	"fmt"
	"reflect"
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
	s := NewService(&foo)
	_assert(len(s.methodMap) == 1, "wrong service Method expect 1,but got %d", len(s.methodMap))
	mType := s.methodMap["Sum"]
	_assert(mType != nil, "wrong method,Sum shouldnt be nil")

}

func TestMethodType_Call(t *testing.T) {
	var foo Foo
	s := NewService(&foo)
	mType := s.methodMap["Sum"]

	argv := mType.newArgv()
	reply := mType.newReplyv()
	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 4}))

	err := s.call(mType, argv, reply)
	_assert(err == nil && *reply.Interface().(*int) == 5 && mType.NumCalls() == 1, "failed to call Foo.Sum")

}
