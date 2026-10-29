package seda

// Operator is the interface every business operator must implement.
// DoImpl returns (code, message): code=0 is normal; code<0 triggers a short-circuit jump to the last operator.
type Operator interface {
	Name() string
	DoImpl(ctx *Context) (int, string)
}

// OperatorBuilder is the factory function creating operator instances.
// Every flow execution calls the factory to obtain a brand-new operator instance.
type OperatorBuilder func() Operator

var registry = map[string]OperatorBuilder{}

// Register registers an operator's factory function so BuildFlow can look it up by name.
func Register(name string, builder OperatorBuilder) {
	registry[name] = builder
}
