package seda

// Operator 是每个业务算子必须实现的接口。
// DoImpl 返回 (code, message)：code=0 正常，code<0 触发短路跳到最后一个算子。
type Operator interface {
	Name() string
	DoImpl(ctx *Context) (int, string)
}

// OperatorBuilder 是创建算子实例的工厂函数。
// 每次 flow 执行都会调工厂来获取全新的算子实例。
type OperatorBuilder func() Operator

var registry = map[string]OperatorBuilder{}

// Register 注册算子的工厂函数，供 BuildFlow 按名字查找。
func Register(name string, builder OperatorBuilder) {
	registry[name] = builder
}
