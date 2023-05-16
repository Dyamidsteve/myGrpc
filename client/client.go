package client

import (
	mygrpc "myGprc"
	"myGprc/codec"
	"sync"
)

// Call承载一次RPC调度所需信息,客户端传时只需赋值
// Seq,method,args，而远程端赋值reply和error
type Call struct {
	Seq           uint64
	ServiceMethod string     //"format '<service>.<method>'"
	Args          any        //arguments to the function
	Reply         any        //reply from the function
	Error         error      //if error occurs,it will be set
	Done          chan *Call //Strobes when call  is complete.
}

// 当调用结束，将调用信息告知调用端
func (call *Call) done() {
	call.Done <- call
}

// Client represents an RPC Client
type Client struct {
	cc       *codec.Codec
	opt      *mygrpc.Option
	sending  sync.Mutex
	header   codec.Header
	mu       sync.Mutex
	seq      uint64
	pending  map[uint64]*Call
	closing  bool
	shutdown bool
}

var _io.Closer = (*Client)(nil)

