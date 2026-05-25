package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"axiom/pkg/axiom"
)

// main - вход в демо-тест программу.
//
// Эта программа не является частью библиотеки. Она показывает, как внешний
// Go-код может загрузить .axm файл, создать engine, зарегистрировать activity
// и выполнить типовые сценарии через публичный API axiom/pkg/axiom.
func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}

	command := os.Args[1]
	path := os.Args[2]
	module, err := loadModuleFile(path)
	if err != nil {
		exitErr("load", err)
	}

	switch command {
	case "validate":
		value := map[string]any{"summary": summary(module)}
		if trizSummary, err := normalizeSummary(path); err == nil && trizSummary != nil {
			value["triz"] = trizSummary
		}
		if err := printJSON(value); err != nil {
			exitErr("print", err)
		}
	case "run-welcome":
		if err := runWelcome(module); err != nil {
			exitErr("run-welcome", err)
		}
	case "run-checkout":
		if err := runCheckout(module); err != nil {
			exitErr("run-checkout", err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  axiom-demo validate examples/axiom-files/<file.axm>")
	fmt.Fprintln(os.Stderr, "  axiom-demo run-welcome examples/axiom-files/welcome.axm")
	fmt.Fprintln(os.Stderr, "  axiom-demo run-checkout examples/axiom-files/checkout.axm")
}

// loadModuleFile - пример загрузки DSL-модуля из файла приложения.
func loadModuleFile(path string) (*axiom.Module, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return axiom.CompileAny(source, axiom.WithSourceName(path))
}

// runWelcome показывает минимальный сценарий: старт execution, signal,
// выполнение activity и чтение state/history.
func runWelcome(module *axiom.Module) error {
	ctx := context.Background()
	engine, err := axiom.New(module, axiom.WithActivity("SendWelcomeEmail", func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
		return axiom.Output{
			"sent":  input["userId"] != "" && input["email"] != "",
			"email": input["email"],
		}, nil
	}))
	if err != nil {
		return err
	}

	const executionID = "demo-welcome-1"
	if err := engine.Start(ctx, executionID, nil); err != nil {
		return err
	}
	if err := engine.Signal(ctx, executionID, "UserRegistered", map[string]any{
		"userId": "u1",
		"email":  "user@example.com",
	}); err != nil {
		return err
	}
	if err := engine.RunUntilIdle(ctx, executionID); err != nil {
		return err
	}

	state, err := engine.Query(ctx, executionID, "state")
	if err != nil {
		return err
	}
	history, err := engine.Query(ctx, executionID, "history")
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"summary": summary(module),
		"state":   state,
		"history": history,
	})
}

// runCheckout показывает более плотный сценарий с несколькими activity
// и пошаговым продвижением runtime до idle-состояния.
func runCheckout(module *axiom.Module) error {
	ctx := context.Background()
	engine, err := axiom.New(module, axiom.WithActivities(axiom.ActivityRegistry{
		"CheckInventory": func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
			return axiom.Output{"status": "available", "unavailable": []any{}}, nil
		},
		"CalculateRisk": func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
			return axiom.Output{"status": "ok", "score": 0.1}, nil
		},
		"ChargeCard": func(ctx context.Context, input axiom.Input) (axiom.Output, error) {
			return axiom.Output{"paymentId": "pay-demo-1", "status": "paid"}, nil
		},
	}))
	if err != nil {
		return err
	}

	const executionID = "demo-checkout-1"
	initial := map[string]any{
		"User": map[string]any{"id": "u1", "country": "US"},
		"Cart": map[string]any{"items": []any{"sku-1"}, "total": 25.0},
		"Payment": map[string]any{
			"method":   map[string]any{"kind": "card", "token": "tok_demo"},
			"intentId": "intent-demo-1",
		},
	}
	if err := engine.Start(ctx, executionID, initial); err != nil {
		return err
	}
	if err := engine.Signal(ctx, executionID, "CheckoutRequested", nil); err != nil {
		return err
	}
	if err := engine.RunUntilIdle(ctx, executionID); err != nil {
		return err
	}
	if err := engine.Signal(ctx, executionID, "CheckoutConfirmed", nil); err != nil {
		return err
	}
	if err := engine.RunUntilIdle(ctx, executionID); err != nil {
		return err
	}

	state, err := engine.Query(ctx, executionID, "state")
	if err != nil {
		return err
	}
	history, err := engine.Query(ctx, executionID, "history")
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"summary": summary(module),
		"state":   state,
		"history": history,
	})
}

func summary(module *axiom.Module) map[string]any {
	return map[string]any{
		"domain":     module.Domain,
		"signals":    len(module.Signals),
		"contexts":   len(module.Contexts),
		"computed":   len(module.Computeds),
		"facts":      len(module.Facts),
		"activities": len(module.Activities),
		"rules":      len(module.Rules),
		"claims":     len(module.Claims),
		"queries":    len(module.Queries),
	}
}

func normalizeSummary(path string) (map[string]any, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !looksLikeTRIZ(source) {
		return nil, nil
	}
	result, err := axiom.NormalizeTRIZ(source, axiom.WithSourceName(path))
	if result == nil {
		return nil, err
	}
	diagnostics := make([]map[string]any, 0, len(result.Diagnostics))
	for _, d := range result.Diagnostics {
		diagnostics = append(diagnostics, map[string]any{
			"code":    d.Code,
			"kind":    d.Kind,
			"entity":  d.Entity,
			"line":    d.Line,
			"message": d.Message,
			"hint":    d.Hint,
		})
	}
	return map[string]any{
		"normalizedBytes": len(result.NormalizedSource),
		"diagnostics":     diagnostics,
		"sourceMap":       result.SourceMap,
	}, err
}

func looksLikeTRIZ(source []byte) bool {
	for _, raw := range strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return strings.HasPrefix(line, "system ")
	}
	return false
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func exitErr(label string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
	os.Exit(1)
}
