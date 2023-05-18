package main

import (
	"context"
	"log"
	mygrpc "myGprc"
	"myGprc/client"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	var foo Foo
	if err := mygrpc.Register(&foo); err != nil {
		log.Fatal("rpc server:register error:", err)
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", listener.Addr())
	addr <- listener.Addr().String()
	mygrpc.Accept(listener)
}

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

func main() {
	//TestReflect()

	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

	client, _ := client.Dial("tcp", <-addr)
	defer client.Close()
	//defer func() {_ = conn.Close()}()

	time.Sleep(time.Second)

	//wg控制主协程等待其他协程结束
	var wg sync.WaitGroup
	// 发送请求并返回消息
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			ctx, _ := context.WithTimeout(context.Background(), time.Second)
			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Fatal("Call Foo.Sum error:", err)
			}
			log.Printf("%d + %d= %d \n", args.Num1, args.Num2, reply)
		}(i)
	}

	wg.Wait()
}
