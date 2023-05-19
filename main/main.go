package main

import (
	"context"
	"log"
	mygrpc "myGprc"
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

func startServer(addrCh chan string) {
	var foo Foo
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", lis.Addr())
	addrCh <- lis.Addr().String()
	server := mygrpc.NewServer()
	_ = server.Register(&foo)
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

func call(addr1, addr2 string) {
	d := xclient.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
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

func broadcast(addr1, addr2 string) {
	d := xclient.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
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
	ch1, ch2 := make(chan string), make(chan string)
	go startServer(ch1)
	go startServer(ch2)

	addr1 := <-ch1
	addr2 := <-ch2

	time.Sleep(time.Second)
	call(addr1, addr2)
	broadcast(addr1, addr2)

}
