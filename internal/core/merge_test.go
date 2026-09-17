package core

import (
	"reflect"
	"testing"
)

func TestDeepMerge(t *testing.T) {
	dst := map[string]any{
		"a": 1,
		"nested": map[string]any{
			"x": 1,
			"y": 2,
		},
		"arr": []any{1, 2},
	}
	src := map[string]any{
		"a": 9, // 标量覆盖
		"nested": map[string]any{ // 对象递归合并
			"y": 20,
			"z": 30,
		},
		"arr": []any{3}, // 数组覆盖
		"new": "v",
	}
	got := DeepMerge(dst, src)
	want := map[string]any{
		"a": 9,
		"nested": map[string]any{
			"x": 1, "y": 20, "z": 30,
		},
		"arr": []any{3},
		"new": "v",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormPolicies(t *testing.T) {
	// warn：记录 warning
	n := NewNorm("warn")
	if v := n.ClampF("speed", 3.0, 0.5, 2.0); v != 2.0 {
		t.Errorf("clamp = %v", v)
	}
	if len(n.Warnings) != 1 || n.Err() != nil {
		t.Errorf("warn policy: warnings=%v err=%v", n.Warnings, n.Err())
	}
	// error：第一处违规即错
	n = NewNorm("error")
	n.ClampF("speed", 3.0, 0.5, 2.0)
	if n.Err() == nil {
		t.Error("error policy should produce error")
	}
	// drop：静默
	n = NewNorm("drop")
	n.ClampF("speed", 3.0, 0.5, 2.0)
	n.Drop("pitch", "unsupported")
	if len(n.Warnings) != 0 || n.Err() != nil {
		t.Errorf("drop policy should be silent: %v %v", n.Warnings, n.Err())
	}
}

func TestNearest(t *testing.T) {
	n := NewNorm("warn")
	if v := n.Nearest("sample_rate", 40000, []int{8000, 16000, 24000, 32000, 44100}); v != 44100 {
		t.Errorf("nearest = %d, want 44100", v)
	}
	if v := n.Nearest("sample_rate", 44100, []int{44100}); v != 44100 {
		t.Errorf("exact match should not warn")
	}
	if len(n.Warnings) != 1 {
		t.Errorf("warnings = %v", n.Warnings)
	}
}
