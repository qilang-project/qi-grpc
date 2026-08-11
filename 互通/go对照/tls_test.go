package interop

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	pb "qigrpc/interop/greetpb"
)

// Go 客户端走 TLS 打 qi 服务端。ALPN 必须协商成 h2，否则 gRPC 根本起不来。
func TestTLS(t *testing.T) {
	addr := os.Getenv("QI_GRPC_TLS_ADDR")
	if addr == "" {
		t.Skip("没设 QI_GRPC_TLS_ADDR，跳过")
	}
	pem, err := os.ReadFile(os.Getenv("QI_GRPC_CA"))
	if err != nil {
		t.Fatalf("读 CA 失败: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("CA 加不进去")
	}
	creds := credentials.NewTLS(&tls.Config{RootCAs: pool})
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, err := pb.NewGreeterClient(conn).SayHello(ctx, &pb.HelloRequest{Name: "TLS"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if reply.Message != "你好，TLS！" {
		t.Errorf("回错了: %q", reply.Message)
	}
}
