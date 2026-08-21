#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONSUMER_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$CONSUMER_DIR"
}
trap cleanup EXIT

cd "$CONSUMER_DIR"
go mod init example.com/axiom-consumer
go mod edit -require=github.com/Homiakus/axiom@v0.0.0
go mod edit -replace=github.com/Homiakus/axiom="$REPO_ROOT"

cat > consumer_test.go <<'EOF'
package consumer

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom"
	"github.com/Homiakus/axiom/axm"
	"github.com/Homiakus/axiom/model"
	"github.com/Homiakus/axiom/table"
)

type state struct {
	Value int `json:"value"`
}

type event struct {
	Value int `json:"value"`
}

func TestPublicPackages(t *testing.T) {
	flow := axiom.NewFlow("consumer", state{})
	axiom.Handle(flow, func(_ context.Context, current state, incoming event) (axiom.FlowResult[state], error) {
		current.Value = incoming.Value
		return axiom.Next(current), nil
	})
	engine, err := axiom.OpenFlow(flow)
	if err != nil {
		t.Fatalf("OpenFlow failed: %v", err)
	}
	if err := engine.Execution("one").Dispatch(context.Background(), event{Value: 7}); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	definition := model.New("Consumer")
	current := model.State[state](definition, "Current")
	incoming := model.Event[event](definition, "SetValue")
	definition.Rule("set").On(incoming.Trigger()).Set(current.Field("Value"), incoming.Field("Value"))
	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if _, err := axm.Parse([]byte("domain Empty\n")); err != nil {
		t.Fatalf("AXM Parse failed: %v", err)
	}
	if _, err := table.Parse([]byte("[workflow]\nname = \"EmptyTable\"\n")); err != nil {
		t.Fatalf("Table Parse failed: %v", err)
	}
}
EOF

go mod tidy
go test -v ./...
echo "==> Isolated consumer test passed successfully."
