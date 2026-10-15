package klingai

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// zodNode describes the shape of a zod object schema: JSON.stringify of a
// zod-parsed value has the schema's keys in schema order and none of the
// unknown keys the wire carried.
type zodNode struct {
	keys  []string
	child map[string]*zodNode
	item  *zodNode // element shape when the value is an array
}

func zodObj(keys []string, child map[string]*zodNode) *zodNode {
	return &zodNode{keys: keys, child: child}
}

var klingaiTaskInfoSpec = zodObj([]string{"external_task_id"}, nil)

// klingaiCreateSpec is TS klingaiCreateTaskSchema.
var klingaiCreateSpec = zodObj([]string{"code", "message", "request_id", "data"}, map[string]*zodNode{
	"data": zodObj([]string{"task_id", "task_status", "task_info", "created_at", "updated_at"}, map[string]*zodNode{
		"task_info": klingaiTaskInfoSpec,
	}),
})

// klingaiStatusSpec is TS klingaiTaskStatusSchema.
var klingaiStatusSpec = zodObj([]string{"code", "message", "request_id", "data"}, map[string]*zodNode{
	"data": zodObj([]string{"task_id", "task_status", "task_status_msg", "task_info", "watermark_info", "final_unit_deduction", "created_at", "updated_at", "task_result"}, map[string]*zodNode{
		"task_info":      klingaiTaskInfoSpec,
		"watermark_info": zodObj([]string{"enabled"}, nil),
		"task_result": zodObj([]string{"videos"}, map[string]*zodNode{
			"videos": {item: zodObj([]string{"id", "url", "watermark_url", "duration"}, nil)},
		}),
	}),
})

// zodStringify renders raw the way TS JSON.stringify(parsedValue) does.
func zodStringify(raw []byte, spec *zodNode) string {
	tree, ok := jsonorder.Parse(raw)
	if !ok {
		return string(raw)
	}
	return jsonorder.Stringify(zodPick(tree, spec))
}

func zodPick(v interface{}, spec *zodNode) interface{} {
	if spec == nil {
		return v
	}
	switch x := v.(type) {
	case *jsonorder.Object:
		if spec.keys == nil {
			return v
		}
		out := jsonorder.New()
		for _, k := range spec.keys {
			if val, ok := x.Get(k); ok {
				out.Set(k, zodPick(val, spec.child[k]))
			}
		}
		return out
	case []interface{}:
		if spec.item == nil {
			return v
		}
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = zodPick(e, spec.item)
		}
		return out
	}
	return v
}
