package mygrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"myGprc/codec"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int        //标志这是mygrpc请求
	CodecType   codec.Type //编解码类型，如gob或json
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server:accept error:", err)
			return
		}
		go s.Serve(conn)
	}
}

func (s *Server) Serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	var opt Option
	//按照协议，先解析出Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	//检查是否对应本服务的请求
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	f := codec.NewCodeFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server:invalid CodecType:%s", opt.CodecType)
		return
	}

	s.serveCodec(f(conn))

}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

// 请求处理
func (s *Server) serveCodec(cc codec.Codec) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		//读取请求
		req, err := s.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			//回复请求
			s.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		//处理请求（每处理完一个Done一个wg）
		//处理请求是并发的，但是回复请求的报文必须是逐个发送的，
		//并发容易导致多个回复报文交织在一起，客户端无法解析。
		//在这里使用锁(sending)保证。
		go s.handleRequest(cc, req, sending, wg)

	}
	wg.Wait()
	//处理完所有请求后关闭连接
	_ = cc.Close()
}

type request struct {
	h            *codec.Header
	argv, replyv reflect.Value
}

// 读取请求头
func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	//初始化请求头,由于decode的时候必须是个实例化的指针变量
	var h = &codec.Header{}

	if err := cc.ReadHeader(h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server:read header error:", err)
		}
		return nil, err
	}

	return h, nil
}

// 读取请求
func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	// 先读取请求头
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	//初始化赋值给请求结构体
	req := &request{h: h}

	//读取请求体
	req.argv = reflect.New(reflect.TypeOf(""))
	if err := cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server:read body error:", err)
	}
	return req, nil
}

// 返回数据
func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()

	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server:write response error:", err)
	}
}

// 处理数据
func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	//day1,just print argv and send a  hello message
	defer wg.Done()
	log.Println(req.h, req.argv.Elem())

	req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp%d", req.h.Seq))
	s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }
