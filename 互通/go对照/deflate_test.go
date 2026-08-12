package interop

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	pb "qigrpc/interop/greetpb"
)

// gRPC 的 deflate 指的是 **zlib 包装**的 deflate（RFC 1950），不是裸 deflate。
// 用错一个，解出来就是一堆 invalid header，而两边都觉得自己没错。
type deflateCompressor struct{}

func (deflateCompressor) Name() string { return "deflate" }

func (deflateCompressor) Compress(w io.Writer) (io.WriteCloser, error) {
	return zlib.NewWriter(w), nil
}

func (deflateCompressor) Decompress(r io.Reader) (io.Reader, error) {
	return zlib.NewReader(r)
}

func init() { encoding.RegisterCompressor(deflateCompressor{}) }

func TestDeflate(t *testing.T) {
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name := strings.Repeat("deflate测试", 300)
	reply, err := pb.NewGreeterClient(conn).SayHello(ctx,
		&pb.HelloRequest{Name: name, Times: 1},
		grpc.UseCompressor("deflate"))
	if err != nil {
		t.Fatalf("deflate 调用失败: %v", err)
	}
	if reply.Message != "你好，"+name+"！" {
		t.Fatalf("回的内容不对（长度 %d）", len(reply.Message))
	}
	t.Logf("deflate 请求 %d 字节，通过", len(name))
}

// 顺手确认 zlib 包装确实是我们认的那种
func TestZlibIsWhatWeThink(t *testing.T) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write([]byte("hello"))
	_ = w.Close()
	// zlib 头第一个字节的低四位是 8（deflate 方法）
	if buf.Bytes()[0]&0x0f != 8 {
		t.Fatalf("zlib 头不对: %x", buf.Bytes()[:2])
	}
	_ = binary.BigEndian
}
