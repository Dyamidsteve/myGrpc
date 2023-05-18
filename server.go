package mygrpc

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"io"
	"log"
	"myGprc/codec"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MagicNumber      = 0x3bef5c
	DefaultTimeOut   = time.Second
	connected        = "200 Connected to Gee RPC"
	dafaultRPCPath   = "/_geerpc_"
	defaultDebugPath = "/debug/geerpc"
)

type Option struct {
	MagicNumber    int           //标志这是mygrpc请求
	CodecType      codec.Type    //编解码类型，如gob或json
	ConnectTimeout time.Duration //连接时长 0 means no limit
	HandleTimeout  time.Duration //处理时长
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

// 定义用于反射的方法结构体
type methodType struct {
	method    reflect.Method //方法本身
	ArgType   reflect.Type   //第一个参数的类型
	ReplyType reflect.Type   //第二个参数的类型
	numCalls  uint64         //后续统计方法调用次数
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value
	// arg may be a pointer type,or a value type
	if m.ArgType.Kind() == reflect.Pointer {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}

	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	// reply musst be a pointer type
	replyv := reflect.New(m.ReplyType.Elem())
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	default:
		break
	}

	return replyv
}

// Server represents a RPC server
type Server struct {
	serviceMap sync.Map //并发安全的万能map
}

type service struct {
	name      string
	typ       reflect.Type
	rcvr      reflect.Value //调用的时候作为参数，如rcvr.<Method>
	methodMap map[string]*methodType
}

func NewServer() *Server {
	return &Server{}
}

func NewService(rcv interface{}) *service {
	s := new(service)
	s.rcvr = reflect.ValueOf(rcv)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcv)

	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid server name", s.name)
	}

	s.registerMehods()
	return s
}

// ******Service Methods
func (s *service) registerMehods() {
	s.methodMap = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		// 入参三个是因为第0个是自身，类似于this
		// 方法的输入参数和返回参数必须分别满足3、1
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		// 方法的输出参数必须是个error类型
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		argType, replyType := mType.In(1), mType.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}

		s.methodMap[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server:register .%s.%s \n", s.name, method.Name)

	}
}

// return true when either type's first char is upper or pkgpath is equal to ""
func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

// 通过反射值调用方法
func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	//调用次数+1
	atomic.AddUint64(&m.numCalls, 1)
	//获取反射类型的方法
	fuc := m.method.Func

	//将参数打包成[]reflect.Value传入并获取[]reflect.Value返回值
	returnValues := fuc.Call([]reflect.Value{s.rcvr, argv, replyv})
	//检查error
	if errInter := returnValues[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}

// ********Server Methods
var DefaultServer = NewServer()

// 给server的serviceMap注册对应的service,参数为任意类型，该类型可以实现对应调度方法
func (s *Server) Register(rcvr any) error {
	service := NewService(rcvr)
	if _, loaded := s.serviceMap.LoadOrStore(service.name, service); loaded {
		return fmt.Errorf("rpc: service already defined")
	}
	return nil
}

// 全局Register
func Register(rcvr any) error {
	s := NewService(rcvr)
	if _, loaded := DefaultServer.serviceMap.LoadOrStore(s.name, s); loaded {
		return fmt.Errorf("rpc: service already defined")
	}
	return nil
}

// 通过ServiceMethod如Foo.Sum在serviceMap找对应service和对应method
func (s *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = fmt.Errorf("rpc server: service/method required ill-formed: " + serviceMethod)
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	sv, ok := s.serviceMap.Load(serviceName)
	if !ok {
		err = fmt.Errorf("rpc server:cant find service")
		return
	}
	svc = sv.(*service)
	mtype = svc.methodMap[methodName]
	if mtype == nil {
		err = fmt.Errorf("rpc server:cant find method")
	}

	return
}

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

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Println("rpc hijacking ", req.RemoteAddr, ":", err)
		return
	}

	_, _ = io.WriteString(conn, "HTTP/1.0"+connected+"\n\n")
	s.Serve(conn)
}

func (s *Server) HandleHTTP() {
	http.Handle(dafaultRPCPath, s)
}

func HandleHTTP() {
	DefaultServer.HandleHTTP()
}

// Serve func need a instanced connection to serve
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
		go s.handleRequest(cc, req, sending, wg, time.Second)

	}
	wg.Wait()
	//处理完所有请求后关闭连接
	_ = cc.Close()
}

type request struct {
	h            *codec.Header //header of request
	argv, replyv reflect.Value //argv and replyv of request
	mtype        *methodType
	svc          *service
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
	req.svc, req.mtype, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	//初始化参数实例
	//确保argvi是个指针用来让readBody能够读取
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}

	//读取请求体
	if err := cc.ReadBody(argvi); err != nil {
		log.Println("rpc server:read body error:", err)
		return req, err
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

// 处理数据(并回应消息)
func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	//构建两个管道，类型使用空类型，达到不传值不占内存的效果
	called := make(chan struct{}) //called表示调用完方法
	sent := make(chan struct{})   //sent表示发送完数据
	defer func() {
		//对于发送完后不再利用的管道，结束后直接关闭，防止协程异常时内存泄漏
		close(called)
		close(sent)
	}()
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	//不限制时间则处理完退出
	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	//timeout只判断处理消息时是否超时，不会判断发送消息超时
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		s.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		//处理完消息后，还需等待回应消息
		<-sent
	}

}

func Accept(lis net.Listener) { DefaultServer.Accept(lis) }
