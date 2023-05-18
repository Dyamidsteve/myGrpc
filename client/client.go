package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	mygrpc "myGprc"
	"myGprc/codec"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	connected        = "200 Connected to Gee RPC"
	dafaultRPCPath   = "/_geerpc_"
	defaultDebugPath = "/debug/geerpc"
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

type clientResult struct {
	client *Client
	err    error
}

type newClientFunc func(conn net.Conn, opt *mygrpc.Option) (client *Client, err error)

// 给连接封装超时机制
// 相比开放的Dial方法，dialTimeout允许自行设置Client创建方法
func dialTimeout(newcleFunc newClientFunc, network, addr string, opts ...*mygrpc.Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, addr, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}

	// close the connection if client is nil
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()

	//相比原来的Dial，这里用chan通信，由另一个协程向管道发信息
	//主协程阻塞等待多个channel数据到达，若超时管道先有数据则超时
	//反之则是返回数据
	ch := make(chan clientResult)
	go func() {
		client, err := newcleFunc(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()

	//若未限制最大连接时长
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}

	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client:connect timeout")
	case result := <-ch:
		return result.client, result.err
	}

}

// Client represents an RPC Client
type Client struct {
	cc       codec.Codec      //消息编解码器
	opt      *mygrpc.Option   //消息类型，json等
	sending  sync.Mutex       //保证请求的有序发送
	header   codec.Header     //请求头
	mu       sync.Mutex       //对Client中各类互斥资源的锁
	seq      uint64           //请求ID
	pending  map[uint64]*Call //存储未处理完的请求，key是请求ID，val是Call示例
	closing  bool             //为true是用户主动关闭
	shutdown bool             //为true是有错误发生
}

// var _ioCloser = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

func NewHTTPClient(conn net.Conn, opt *mygrpc.Option) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", dafaultRPCPath))

	//require successful http response
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = fmt.Errorf("unexpected HTTP response: %s", resp.Status)
	}
	return nil, err
}

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	//如果已经关闭
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

// IsAvailable return true if the client does work
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.closing && !client.shutdown
}

// 登记Call实例，并更新client.seq请求ID
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}

	call.Seq = client.seq
	client.pending[client.seq] = call

	client.seq++
	return call.Seq, nil

}

// 根据请求ID删去Call实例
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()

	call := client.pending[seq]
	delete(client.pending, seq)
	return call

	// if call, ok := client.pending[seq]; ok {
	// 	delete(client.pending, seq)
	// 	return call
	// }
	// return nil

}

// 由于发生错误而中断所有RPC的Call实例
func (client *Client) terminateCalls(err error) {
	//涉及Call实例删除，要关闭请求的发送
	client.sending.Lock()
	defer client.sending.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		//赋值error信息
		call.Error = err
		call.done()
	}

}

/*
对一个客户端端来说，接收响应、发送请求是最重要的 2 个功能。
那么首先实现接收功能，接收到的响应有三种情况：
call 不存在，可能是请求没有发送完整，或者因为其他原因被取消，但是服务端仍旧处理了。
call 存在，但服务端处理出错，即 h.Error 不为空。
call 存在，服务端处理正常，那么需要从 body 中读取 Reply 的值。
*/
func (client *Client) receive() {
	var err error
	// break when err occurs
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		//当收到回应时，将该请求实例删除
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			// it usually means that Write partially failed
			// and call was already removed.
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// means that an error occurred from server
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = fmt.Errorf("reading body %s", err.Error())
			}
			call.done()
		}
	}
	// error occurs,so terminateCalls pending calls
	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *mygrpc.Option) (*Client, error) {
	f := codec.NewCodeFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type: %s", opt.CodecType)
		log.Println("rpc client:codec error:", err)
		return nil, err
	}

	// send options with server
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client:options error:", err)
		_ = conn.Close()
		return nil, err
	}

	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *mygrpc.Option) *Client {
	client := &Client{
		cc:      cc,
		seq:     1,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}

	//每次创建新客户端，都自动开辟新的协程来读取服务端响应
	go client.receive()
	return client
}

// 为简化调用Dial，用户可以预设Option作为可选参数
// 这里使用...不是为了允许设置多个，而是允许没有,即可以不输入参数，使用默认设置
func parseOptions(opts ...*mygrpc.Option) (*mygrpc.Option, error) {
	//用户未输入参数或输入nil则使用默认Option
	if len(opts) == 0 || opts[0] == nil {
		return mygrpc.DefaultOption, nil
	}

	if len(opts) != 1 {
		return nil, fmt.Errorf("number of options is more than one")
	}
	opt := opts[0]
	opt.MagicNumber = mygrpc.DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = mygrpc.DefaultOption.CodecType
	}
	return opt, nil
}

// Dial connetc to an RPC server at the specified netword in tcp
func Dial(network, address string, opts ...*mygrpc.Option) (client *Client, err error) {
	return dialTimeout(NewClient, network, address, opts...)
}

// DialHTTP connect to RPC server in HTTP
func DialHTTP(network, addr string, opts ...*mygrpc.Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, addr, opts...)
}

// XDial calls different functions to connect to a RPC server
// according to the first parameter rpcAddr
// rpcAddr is a general format protocol to represent
// eg, http@10.0.0.1:7001,tcp@10.0.0.1:9999,unix@/tmp/geerpc.sock
func XDial(rpcAddr string, opts ...*mygrpc.Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err:wrong format '%s', expect protocol@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	case "tcp":
		return Dial("tcp", addr, opts...)
	}
}

// 发送请求
func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()

	//登记call实例，并获取请求ID
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	//encode and send message
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)

		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// 打包并发送请求，同时返回Call实例，
// 调用端可以等待Call中的done管道有结果后继续新的处理
func (client *Client) Go(serviceMethod string, args, reply any, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call)
	} else if cap(done) == 0 {
		log.Panic("rpc client done channel is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}

	//开辟另一个协程发送call
	//go client.send(call)
	client.send(call)
	return call
}

// 请求调度的超时机制交给了context，由用户来判断
// 发送请求并阻塞等待回应，返回reply和error信息,reply作为指针传参获取
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply any) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return fmt.Errorf("rpc client:call failed:" + ctx.Err().Error())
	case cl := <-call.Done:
		return cl.Error

	}

	/*
		用户可以使用 context.WithTimeout 创建具备超时检测能力的 context 对象来控制。例如：
			ctx, _ := context.WithTimeout(context.Background(), time.Second)
			var reply int
			err := client.Call(ctx, "Foo.Sum", &Args{1, 2}, &reply)
			...
	*/

}
