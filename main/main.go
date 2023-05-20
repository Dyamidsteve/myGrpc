package main

import (
	"context"
	"log"
	mygrpc "myGprc"
	"myGprc/registry"
	"myGprc/xclient"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}

// 开启注册中心服务
func startRegistry(wg *sync.WaitGroup) {
	lis, _ := net.Listen("tcp", ":9999")
	registry.HandleHTTP()
	wg.Done()
	_ = http.Serve(lis, nil)
}

// 开启服务端
func startServer(registryAddr string, wg *sync.WaitGroup) {
	var foo Foo
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", lis.Addr())
	server := mygrpc.NewServer()
	_ = server.Register(&foo)
	// 隔一段时间发送心跳，来告诉注册中心自己还提供着服务
	registry.Heartbeat(registryAddr, "tcp@"+lis.Addr().String(), 0)
	wg.Done()
	server.Accept(lis)
}

func startHTTPServer(addr chan string) {
	var foo Foo
	if err := mygrpc.Register(&foo); err != nil {
		log.Fatal("rpc server:register error:", err)
	}

	listener, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	mygrpc.HandleHTTP()
	addr <- listener.Addr().String()
	_ = http.Serve(listener, nil)
}

func foo(xc *xclient.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var reply int
	var err error
	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.BroadCast(ctx, serviceMethod, args, &reply)
	}

	if err != nil {
		log.Printf("%s %s error %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

// 客户端调用call方法demo
func call(registry string) {
	d := xclient.NewGeeRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer xc.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i * i})
			// expect 2 - 5 timeout
		}(i)
	}

	wg.Wait()
}

// 客户端调用broadcast方法demo
func broadcast(registry string) {
	d := xclient.NewGeeRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer xc.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "broadcast", "Foo.Sum", &Args{Num1: i, Num2: i * i})
			ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
			foo(xc, ctx, "broadcast", "Foo.Sleep", &Args{Num1: i, Num2: i * i})
		}(i)
	}

	wg.Wait()
}

/*
func TestReflect() {
	var wg sync.WaitGroup
	typ := reflect.TypeOf(&wg)
	//输出类型的每个方法名
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		argv := make([]string, 0, method.Type.NumIn())
		returns := make([]string, method.Type.NumOut())

		for j := 0; j < method.Type.NumIn(); j++ {
			argv = append(argv, method.Type.In(j).String())
		}
		for j := 0; j < method.Type.NumOut(); j++ {
			returns = append(returns, method.Type.Out(j).String())
		}

		log.Printf("func (w *%s) %s (%s) %s",
			typ.Elem().Name(),
			method.Name,
			strings.Join(argv, ","),
			strings.Join(returns, ","))
	}
}
*/

func main() {
	//TestReflect()

	log.SetFlags(0)
	registryAddr := "http://localhost:9999/_geerpc_/registry"

	var wg sync.WaitGroup

	//先启动注册中心
	wg.Add(1)
	go startRegistry(&wg)
	wg.Wait()

	//再启动RPC服务端
	wg.Add(2)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()

	//最后客户端远程调用
	time.Sleep(time.Second)
	call(registryAddr)
	broadcast(registryAddr)

}
