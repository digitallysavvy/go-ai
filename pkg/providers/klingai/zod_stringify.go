package klingai

import "github.com/digitallysavvy/go-ai/pkg/internal/zodjson"

// zodNode describes the shape of a zod object schema: JSON.stringify of a
// zod-parsed value has the schema's keys in schema order and none of the
// unknown keys the wire carried.
type zodNode = zodjson.Node

func zodObj(keys []string, child map[string]*zodNode) *zodNode { return zodjson.Obj(keys, child) }

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
			"videos": zodjson.Array(zodObj([]string{"id", "url", "watermark_url", "duration"}, nil)),
		}),
	}),
})

// zodStringify renders raw the way TS JSON.stringify(parsedValue) does.
func zodStringify(raw []byte, spec *zodNode) string { return zodjson.Stringify(raw, spec) }
