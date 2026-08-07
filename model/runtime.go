package model

// runtimeNamespace exposes the stable runtime.* query projection namespace
// without requiring stringly-typed Ref calls in Go models.
type runtimeNamespace struct{}

// Runtime contains helpers for stable execution metadata that is valid inside
// model.Query expressions.
var Runtime runtimeNamespace

func (runtimeNamespace) ID() Expr              { return Ref("runtime.id") }
func (runtimeNamespace) Domain() Expr          { return Ref("runtime.domain") }
func (runtimeNamespace) Status() Expr          { return Ref("runtime.status") }
func (runtimeNamespace) Version() Expr         { return Ref("runtime.version") }
func (runtimeNamespace) CreatedAt() Expr       { return Ref("runtime.createdAt") }
func (runtimeNamespace) UpdatedAt() Expr       { return Ref("runtime.updatedAt") }
func (runtimeNamespace) ModuleHash() Expr      { return Ref("runtime.moduleHash") }
func (runtimeNamespace) CompilerVersion() Expr { return Ref("runtime.compilerVersion") }
func (runtimeNamespace) PlanVersion() Expr     { return Ref("runtime.planVersion") }
