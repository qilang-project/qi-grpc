// 一个真的 Go gRPC 服务端，专门给 qi 客户端当靶子。
//
// qi 客户端 → 这里，验的是我们的 h2 客户端说得对不对：请求头（te: trailers、
// content-type）、5 字节分帧、trailers 里的 grpc-status 读不读得对，以及
// **trailers-only 响应**（出错时状态在初始头里而不是 trailers 里）那条路。
//
//   go run ./服务端 -addr 127.0.0.1:47821
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "qigrpc/interop/greetpb"
)

type server struct {
	pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	if in.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name 不能为空")
	}
	if in.Name == "拒绝" {
		// 中文错误消息 —— 验 qi 客户端的 percent-decode
		return nil, status.Error(codes.PermissionDenied, "Go 服务端拒绝了：" + in.Name)
	}
	times := in.Times
	if times < 1 {
		times = 1
	}
	if times > 5 {
		times = 5
	}
	msg := strings.Repeat("Hello, "+in.Name+"! ", int(times))
	if in.Mood == pb.Mood_MOOD_TIRED {
		msg += "(get some rest)"
	}
	return &pb.HelloReply{
		Message:  strings.TrimSpace(msg),
		ServedAt: time.Now().Unix(),
		Detail:   &pb.Detail{Server: "go", Cached: len(in.Tags) > 0},
	}, nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:47821", "监听地址")
	flag.Parse()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterGreeterServer(s, &server{})
	log.Printf("Go gRPC 服务端: %s", *addr)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}
