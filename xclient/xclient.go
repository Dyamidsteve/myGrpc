package xclient

import (
	"context"
	mygrpc "myGprc"
	"myGprc/client"
	"reflect"
	"sync"
)

// 支持负载均衡的客户端
type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *mygrpc.Option
	mu      sync.Mutex
	clients map[string]*client.Client //管理多个client
}

// var _io.Closer = (*XClient)(nil)

func NewXClient(d Discovery, mode SelectMode, opt *mygrpc.Option) *XClient {
	return &XClient{d: d, mode: mode, opt: opt, clients: make(map[string]*client.Client)}
}

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}

	return nil

}

// 虽然叫dial，但实际上是获取rpcAddr对应的客户端，若已经存在则直接返回，不存在则XDial方法获取新的client
func (xc *XClient) dial(rpcAddr string) (*client.Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()

	clt, ok := xc.clients[rpcAddr]
	//有对应client但是client状态关闭
	if ok && !clt.IsAvailable() {
		//手动关闭client，集合中删除该client
		_ = clt.Close()
		delete(xc.clients, rpcAddr)
		clt = nil
	}

	//XClient中clients集合没有该rpcAddr对应的client则新创建连接
	if clt == nil {
		var err error
		clt, err = client.XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = clt
	}

	return clt, nil

}

// XClient的Call方法，先找到客户端，再调用客户端的call
func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, argv, reply any) error {
	//获取客户端
	clt, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	//调用客户端的call方法
	return clt.Call(ctx, serviceMethod, argv, reply)
}

// 封装了服务发现的XClient的Call方法
func (xc *XClient) Call(ctx context.Context, serviceMethod string, argv, reply any) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	// 调用对应client的call方法
	return xc.call(rpcAddr, ctx, serviceMethod, argv, reply)
}

// 广播给每个服务器调用call
func (xc *XClient) BroadCast(ctx context.Context, serviceMethod string, argv, reply any) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}

	//由于是全体广播，不能光等上一个广播完才广播下一个
	var wg sync.WaitGroup
	var mu sync.Mutex //protect e and replyDone

	var e error
	replyDone := reply == nil // if reply is nil, don't need to set value
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()

			var cloneReply any
			if reply != nil {
				cloneReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err = xc.call(rpcAddr, ctx, serviceMethod, argv, reply)
			//设置主协程的error值的时候，必须互斥访问,且当error发生时候，控制一些语句不再执行
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel() // if any call failed, cancel unfinished calls
			}
			if err == nil && !replyDone {
				//原本reply为nil，则直接设为nil
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(cloneReply).Elem())
				replyDone = true
			}

			mu.Unlock()

		}(rpcAddr)
	}
	wg.Wait()
	return e

}
