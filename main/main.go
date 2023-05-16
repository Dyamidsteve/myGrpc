package main

import (
	"encoding/json"
	"fmt"
	"log"
	mygrpc "myGprc"
	"myGprc/codec"
	"net"
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

func main() {
	addr := make(chan string)
	go startServer(addr)

	conn, _ := net.Dial("tcp", <-addr)
	defer conn.Close()
	//defer func() {_ = conn.Close()}()

	time.Sleep(time.Second)
	//客户端先发送options
	_ = json.NewEncoder(conn).Encode(mygrpc.DefaultOption)
	cc := codec.NewGobCodec(conn)
	// 发送请求并返回消息
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		//发送request
		_ = cc.Write(h, fmt.Sprintf("mygrpc req %d", h.Seq))

		//获取respons
		_ = cc.ReadHeader(h)
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
