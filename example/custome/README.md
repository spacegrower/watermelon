# 如何实现业务自定义框架

业务自定义框架是指根据业务特性基于 watermelon 实现自定义注册/发现字段信息  
本文我们以一个定时任务系统的角度来设计基础框架

1. 复制整个根目录下 watermelon.go 文件到业务代码中，例如本示例的 firemelon.go(当然记得修改包名)

2. 声明业务自定义的 NodeMeta(注册信息) 与 ResolveMeta(发现信息)

### 自定义注册信息

需要实现

```go
interface[T any] {
    // 框架需要
    WithMeta(register.NodeMeta) T
    // watermelon自带的etcd注册器需要
    RegisterKey() string
	Value() string
}
```

`register.NodeMeta` 为框架能获取的一些关于节点的基本信息，用户可以根据需要自行通过 WithMeta 方法 merge 进 NodeMeta 中  
`WithMeta(register.NodeMeta) T` 在框架调用服务前时被调用, watermelon.registry.Append(s.CustomInfo.WithMeta(metaData))

假设我们当前场景需要按照组织及系统来划分不同的微服务，同时我们又要通过地区属性(region)来隔离不同网络环境下的服务，且希望每个服务拥有自己的一个权重属性，用来做负载均衡逻辑。  
那么我们的 NodeMeta 设计如下

```go
type NodeMeta struct {
	OrgID        string
	System       string
	Region       string
	Weight       int32
	RegisterTime int64
	register.NodeMeta
}
```

通过实现 `WithMeta(register.NodeMeta) T` 方法拿到框架提供给我们的一些服务基本信息

```go
func (n NodeMeta) WithMeta(meta register.NodeMeta) NodeMeta {
	n.NodeMeta = meta
	return n
}
```

通过实现 `RegisterKey() string` 与 `Value() string` 两个方法来告诉 etcd 注册器我们需要写入 etcd 的 key 和 value

```go

func init() {
	// 首先设置etcd key前缀，该前缀主要用来与其他共用etcd的业务进行隔离，其次可以方便的通过该前缀获取所有与之相关的服务注册信息
	infra.RegisterETCDRegisterPrefixKey("/gophercron/registry")
}

func (n NodeMeta) RegisterKey() string {
	// 实际写入时框架会在我们生成的key前面加上 etcdkeyprefix，这里我们只需要生成业务相关的key即可
	return fmt.Sprintf("%s/%s/%s/node/%s:%d", n.OrgID, n.System, n.ServiceName, n.Host, n.Port)
}

func (n NodeMeta) Value() string {
	// 这里我们自己实现一个节点权重获取的姿势
	n.Weight = utils.GetEnvWithDefault(definition.NodeWeightENVKey, n.Weight, func(val string) (int32, error) {
		res, err := strconv.Atoi(val)
		if err != nil {
			return 0, err
		}
		return int32(res), nil
	})

	n.RegisterTime = time.Now().Unix()

	raw, _ := json.Marshal(n)
	return string(raw)
}
```

当我们在设计 etcd key 时，是需要考虑到我们的节点如何被 resolver 发现，示例中我们设计的 key 为

{组织 ID}/{系统名称}/{服务名称}/node/IP:端口  
其中组织 ID 可以方便的使我们系统成为多租户系统，比如公司内部不同的部门进行隔离

这样在我们定时任务中心需要发现某一系统的 agent 时，就可以使用

```shell
etcdctl get {etcd-prefix-key}/{组织ID}/{系统名称} --prefix
```

实现代码见 service.meta.go 中的 `NodeMeta`

### 自定义发现信息

需要实现

```go
interface {
    FullServiceName(srvName string) string
}
```

返回服务发现时需要用到的 target，该 taget 不包含 scheme

实现代码见 client.meta.go 中的 `ResolveMeta`

3. 实现 server 与 client 的 options 配置方法

```go
func (s *Server[T]) WithSystem(ns string) infra.Option[NodeMeta] {
	return func(s *infra.SrvInfo[NodeMeta]) {
		s.CustomInfo.System = ns
	}
}

func (s *Server[T]) WithOrg(org string) infra.Option[NodeMeta] {
	return func(s *infra.SrvInfo[NodeMeta]) {
		s.CustomInfo.OrgID = org
	}
}
```

至此完成了业务框架的自定义封装
