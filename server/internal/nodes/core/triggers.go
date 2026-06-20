package core

import "github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"

// The scheduler (internal/scheduler) fires this trigger on its interval/cron; the
// node itself just emits the scheduler-supplied payload, like the manual trigger.
func init() { Nodes = append(Nodes, scheduleTriggerNode) }

var scheduleTriggerNode = schema.NodeDefinition{
	Type:        "core.scheduleTrigger",
	Label:       "Schedule Trigger",
	Group:       "trigger",
	Icon:        "CalendarClock",
	Description:  "Start the workflow on an interval or cron schedule.",
	IsTrigger:   true,
	Inputs:      []schema.Port{},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "mode", Label: "Mode", Type: "select", Default: "interval", Options: []schema.ParamOption{
			{Label: "Interval", Value: "interval"}, {Label: "Cron", Value: "cron"},
		}},
		{Name: "amount", Label: "Every", Type: "number", Default: 5,
			ShowWhen: &schema.ShowWhen{Param: "mode", Equals: []any{"interval"}}},
		{Name: "unit", Label: "Unit", Type: "select", Default: "minutes", Options: []schema.ParamOption{
			{Label: "Seconds", Value: "seconds"}, {Label: "Minutes", Value: "minutes"},
			{Label: "Hours", Value: "hours"}, {Label: "Days", Value: "days"},
		}, ShowWhen: &schema.ShowWhen{Param: "mode", Equals: []any{"interval"}}},
		{Name: "cron", Label: "Cron expression (5-field)", Type: "string", Placeholder: "0 9 * * 1-5",
			ShowWhen: &schema.ShowWhen{Param: "mode", Equals: []any{"cron"}}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		out := ctx.Trigger
		if len(out) == 0 {
			out = []schema.Item{{JSON: map[string]any{}}}
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}
