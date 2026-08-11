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
| 一元调用（服务端） | ✅ 已验证 |
| protobuf 线格式 | ✅ 与 protoc 逐字节一致 |
| gRPC 状态码 + 错误消息（含中文） | ✅ 走 trailers |
| 明文 h2c | ✅ |
| 单连接多路复用 | ✅ 50 并发实测 |
| **客户端** | ❌ 还没做 |
| **流式**（服务端流/客户端流/双向） | ❌ 还没做，调用会立刻回 UNIMPLEMENTED |
| TLS | ❌ 还没接（运行时有 rustls，接上不难） |
| 压缩 | ❌ 对面压了会回 UNIMPLEMENTED |
| 反射服务（server reflection） | ❌ 所以 grpcurl 要带 `-proto` |

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

## 目录

```
gRPC.qi          qi 侧：注册、分发、错误码
协议/greet.proto 互通验收用的最小服务定义
示例/问候服务端.qi
互通/go对照/     Go gRPC 客户端写的验收测试（跑法见文件头）
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

# 3. Go 客户端（protoc 生成的静态桩 + 多路复用）
cd 互通/go对照 && QI_GRPC_ADDR=127.0.0.1:47813 go test -v
```

两条路的编解码实现不一样，都过才算真互通。Go 那套还能验到 grpcurl 验不到的
多路复用 —— grpcurl 每次调用开一条新连接。
