package pebble

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"sync"

	"axiom/internal/runtime"
)

type codecKind string

const (
	codecGob  codecKind = "gob"
	codecJSON codecKind = "json"
)

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func init() {
	gob.Register(map[string]any{})
	gob.Register(map[string]int{})
	gob.Register(map[string]string{})
	gob.Register([]any{})
	gob.Register([]string{})
	gob.Register(runtime.Execution{})
	gob.Register(runtime.ExecutionState{})
	gob.Register(runtime.Value{})
	gob.Register(runtime.FactValue{})
	gob.Register(runtime.HistoryEntry{})
	gob.Register(runtime.ActivityTask{})
}

func encodeValue(kind codecKind, value any) ([]byte, error) {
	if kind == codecJSON {
		return json.Marshal(value)
	}
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)
	if err := gob.NewEncoder(buf).Encode(value); err != nil {
		data, jsonErr := json.Marshal(value)
		if jsonErr != nil {
			return nil, err
		}
		return append([]byte("json:"), data...), nil
	}
	out := make([]byte, len("gob:")+buf.Len())
	copy(out, "gob:")
	copy(out[len("gob:"):], buf.Bytes())
	return out, nil
}

func decodeValue(kind codecKind, data []byte, out any) error {
	if kind == codecJSON {
		return json.Unmarshal(data, out)
	}
	if bytes.HasPrefix(data, []byte("json:")) {
		return json.Unmarshal(data[len("json:"):], out)
	}
	if bytes.HasPrefix(data, []byte("gob:")) {
		data = data[len("gob:"):]
	}
	return gob.NewDecoder(bytes.NewReader(data)).Decode(out)
}
