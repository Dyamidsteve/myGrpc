package codec

import "io"

/*
	请求体中包含请求参数args和return返回值，
	其余的请求方法名、error、请求ID放在Header里面
*/
type Header struct {
	ServiceMethod string // format “Service.Method”
	Seq           uint64 //sequence number chosen by client
	Error         string
}

//消息体编解码接口
type Codec interface {
	io.Closer //继承接口io.Closer,来实现Close
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodeFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	//默认两种编码
	GobType  Type = "application/gob"
	JsonType Type = "application/json" //not implemented
)

//初始化全局编码映射
var NewCodeFuncMap map[Type]NewCodeFunc

func init() {
	NewCodeFuncMap = make(map[Type]NewCodeFunc)

	//先添加gob编解码器
	NewCodeFuncMap[GobType] = NewGobCodec

}
