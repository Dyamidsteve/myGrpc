package main

import (
	"fmt"
	"log"
	mygrpc "myGprc"
	"myGprc/client"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

func startServer(addr chan string) {
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
			argv = append(argv, method.Type.In(j).Name())
		}
		for j := 0; j < method.Type.NumOut(); j++ {
			returns = append(returns, method.Type.Out(j).Name())
		}

		log.Printf("func (w *%s) %s (%s) %s",
			typ.Elem().Name(),
			method.Name,
			strings.Join(argv, ","),
			strings.Join(returns, ","))
	}
}

func main() {

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
			args := fmt.Sprintf("geerpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("CALL Foo.Sum error:", err)
			}
			log.Println("reply:", reply)
		}(i)
	}

	wg.Wait()
}
