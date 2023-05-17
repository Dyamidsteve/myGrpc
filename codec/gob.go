package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn    io.ReadWriteCloser //tcp/upd连接
	buf     *bufio.Writer      //防止阻塞的缓存writer
	decoder *gob.Decoder       //gob解码器
	encoder *gob.Encoder       //gob编码器
}

var _Codec = (*GobCodec)(nil) //全局变量

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn:    conn,
		buf:     buf,
		decoder: gob.NewDecoder(conn),
		encoder: gob.NewEncoder(buf),
	}
}

// 从连接中读取请求头，包含解码
func (gc *GobCodec) ReadHeader(h *Header) error {
	return gc.decoder.Decode(h)
}

// 从连接中读取请求体，包含解码
func (gc *GobCodec) ReadBody(body interface{}) error {
	return gc.decoder.Decode(body)
}

// 将这里返回的error也可做为判断是否要断开连接的依据
func (gc *GobCodec) Write(h *Header, body interface{}) (err error) {
	//结束时出现err则关闭连接
	defer func() {
		_ = gc.buf.Flush()
		if err != nil {
			_ = gc.Close()
		}
	}()
	//编码并发送给连接
	if err := gc.encoder.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}
	if err := gc.encoder.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}

	return nil
}

// 实现Close方法，实际是io.Closer.Close()
func (gc *GobCodec) Close() error {
	return gc.conn.Close()
}
