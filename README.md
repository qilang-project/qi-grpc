# qi-grpc

用 qi 写 gRPC 服务。真的 gRPC —— HTTP/2 + protobuf 线格式 + trailers，
`grpcurl`、Go、Java 的客户端直接能打。

```qi
包 主程序;

导入 gRPC::{建服务, 注册, 跑, 出错, 参数不对};
导入 标准库.JSON 作为 J;

函数 说你好(请求JSON: 字符串) : 字符串 {
    变量 请求: 整数 = J.解码(请求JSON);
    变量 名: 字符串 = J.获取字符串(请求, "name");
    如果 (字符串::字节长度(名) == 0) {
        返回 出错(参数不对(), "name 不能为空");
    }
    变量 回: 整数 = J.创建对象();
    J.设置字符串(回, "message", "你好，" + 名);
    返回 J.转字符串(回);
}

函数 入口() {
    建服务("协议/greet.proto");
    注册("greet.Greeter/SayHello", 说你好);
    跑("127.0.0.1", 47811);
}
```

```
$ grpcurl -plaintext -import-path 协议 -proto greet.proto \
          -d '{"name":"世界"}' 127.0.0.1:47811 greet.Greeter/SayHello
{
  "message": "你好，世界"
}
```

## 现在有什么、没有什么

| | 状态 |
|---|---|
| 一元调用（服务端 + 客户端） | ✅ |
| 流式（服务端流 / 客户端流 / 双向） | ✅ 服务端；客户端只有一元 |
| protobuf 线格式 | ✅ 与 protoc 逐字节一致 |
| gRPC 状态码 + 错误消息（含中文） | ✅ 走 trailers，百分号编码 |
| 明文 h2c | ✅ |
| TLS（两端，ALPN h2） | ✅ 证书校验不可绕过 |
| gzip 压缩（收 + 发） | ✅ 双向实测 |
| 服务端反射（v1 + v1alpha） | ✅ grpcurl 不用带 `-proto` |
| 单连接多路复用 | ✅ 50 并发实测 |
| **客户端流式** | ❌ 客户端只做了一元调用 |
| 拦截器 / 超时传递（deadline 头） | ❌ |
| 负载均衡 / 重试 | ❌ 一个连接一个后端 |

## 流式

三种流在这一层是**同一件事** —— 收几条、发几条自己定，`.proto` 里的
`stream` 标记只影响客户端怎么用：

```qi
函数 唠嗑(调用: 整数) : 整数 {
    当 (真) {
        变量 一条: 字符串 = 收一条(调用, 10000);
        如果 (到头了(一条) == 1) { 跳出; }             // 客户端半关，该收尾
        如果 (字符串::字节长度(一条) == 0) { 继续; }    // 这轮没有，接着等
        发一条(调用, 想回什么(一条));
    }
    返回 收尾好了(调用);
}
注册流式("greet.Greeter/Chatter", 唠嗑);
```

「这轮没有」和「到头了」必须分开处理 —— 混成一个值，循环要么提前收摊
要么永远转下去。

## 写业务代码要知道的三件事

**1. JSON 字段名按 protobuf 官方映射，不是 .proto 里的写法。**
`.proto` 写 `served_at`，JSON 里是 `servedAt`；`int64` 是**字符串**（`"7"` 不是 `7`）；
枚举是**名字**（`"MOOD_HAPPY"` 不是 `1`）。这是 proto3 JSON mapping 规范，
对面的 Go/Java 服务也这么认。拿不准就把消息解出来打一眼。

**2. 出错用 `出错(状态码, 消息)` 返回，别自己拼字符串。**
框架据此发 `grpc-status`。中文消息会自动做百分号编码穿过 trailers ——
HTTP 头放不了非 ASCII，不编码的话整条 trailers 发不出去，
客户端只会看到「等到超时」，服务端这边一点痕迹没有。

**3. 方法名在 `注册` 时就查 `.proto`。**
拼错了当场报，不用等对面来调用才以 UNIMPLEMENTED 的形式发现。

**每条调用都必须收尾**（一元的 `回复`、流式的 `收尾好了`/`收尾出错`）。
不收尾的话客户端一直等到自己超时，而服务端这边一点痕迹都没有 ——
这是 gRPC 最难查的一类症状。

## 目录

```
gRPC.qi              qi 侧：注册、分发、流式、客户端、错误码
协议/greet.proto     互通验收用的最小服务定义
示例/问候服务端.qi   一元 + 双向流 + TLS（给了证书就走 TLS）
示例/问候客户端.qi   打任意 gRPC 后端
示例/压缩验.qi       双向 gzip
互通/go对照/         Go 客户端与服务端两侧的验收测试
互通/证书/           测试用的 CA + 叶子证书（自签，只在本机用）
```

运行时那半边（HTTP/2 分帧、HPACK、trailers、protobuf 编解码）在
`qi-runtime/src/io/grpc_ffi.rs` 和 `qi-runtime/src/stdlib/protobuf_ffi.rs` ——
FFI 符号必须在 `libqi_runtime.a` 里才链得上，所以没法放在这个仓。

## 为什么不是回调式

第一版把 qi 的处理函数当函数指针交给运行时，收到请求就回调。**那条路是死的**：
qi 把函数当值使用时会包一层闭包对象，FFI 参数拿到的是那个对象的栈地址而不是
裸代码地址，transmute 过去调用就是 SIGBUS。`h2_ffi.rs` 里那个回调是同样的写法，
所以 `运行应用_HTTP2` 那条路同样调不通 —— 只是至今没人走过。

现在是拉取式：运行时把解好的调用排进队列，qi 侧循环「接收调用 → 处理 → 回复」，
每条调用 `启动` 一个 goroutine。这也正是 qi-web 已经在生产上跑的形状。

## 跑验收

```bash
# 1. 起服务端
qi compile 示例/问候服务端.qi -o /tmp/问候服务端
GREET_PROTO=$PWD/协议/greet.proto PORT=47813 /tmp/问候服务端 &

# 2. grpcurl（动态读 .proto 那条路）
grpcurl -plaintext -import-path 协议 -proto greet.proto \
        -d '{"name":"世界","times":"3","mood":"MOOD_HAPPY"}' \
        127.0.0.1:47813 greet.Greeter/SayHello

# 3. Go 客户端（protoc 生成的静态桩 + 多路复用 + 流式 + gzip）
cd 互通/go对照 && QI_GRPC_ADDR=127.0.0.1:47813 go test -v

# 4. 反向：qi 客户端打真 Go 服务端
cd 互通/go对照 && go run ./server -addr 127.0.0.1:47821 &
GRPC_ADDR=127.0.0.1:47821 qi run 示例/问候客户端.qi

# 5. TLS（证书在 互通/证书）
GRPC_CERT=$PWD/互通/证书/cert.pem GRPC_KEY=$PWD/互通/证书/key.pem \
  PORT=47831 /tmp/问候服务端 &
grpcurl -cacert 互通/证书/ca.pem -d '{"name":"TLS"}' \
  localhost:47831 greet.Greeter/SayHello
```

两条路的编解码实现不一样，都过才算真互通。Go 那套还能验到 grpcurl 验不到的
多路复用 —— grpcurl 每次调用开一条新连接。
