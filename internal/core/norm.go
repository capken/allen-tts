package core

import (
	"fmt"
	"math"
)

// Norm 在 adapter 构建 payload 时执行降级策略（设计文档 4.3）：
// clamp / drop / 取最近值都经由它，保证 warning 文案一致，
// 且 --on-unsupported=error 时能在第一处违规停下。
type Norm struct {
	Policy   string // warn | error | drop
	Warnings []string
	err      *Error
}

func NewNorm(policy string) *Norm {
	if policy == "" {
		policy = "warn"
	}
	return &Norm{Policy: policy}
}

// note 按策略处理一条违规：warn 记录、error 置错、drop 静默。
func (n *Norm) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	switch n.Policy {
	case "error":
		if n.err == nil {
			n.err = NewError(ErrInvalidArgument, "%s", msg)
		}
	case "drop":
		// 静默
	default:
		n.Warnings = append(n.Warnings, msg)
	}
}

// Drop 记录一个被丢弃的不支持字段。
func (n *Norm) Drop(field, reason string) {
	n.note("%s dropped: %s", field, reason)
}

// ClampF 把浮点值裁剪进 [lo, hi]。
func (n *Norm) ClampF(field string, v, lo, hi float64) float64 {
	if v < lo {
		n.note("%s clamped from %g to %g", field, v, lo)
		return lo
	}
	if v > hi {
		n.note("%s clamped from %g to %g", field, v, hi)
		return hi
	}
	return v
}

// ClampI 把整数值裁剪进 [lo, hi]。
func (n *Norm) ClampI(field string, v, lo, hi int) int {
	if v < lo {
		n.note("%s clamped from %d to %d", field, v, lo)
		return lo
	}
	if v > hi {
		n.note("%s clamped from %d to %d", field, v, hi)
		return hi
	}
	return v
}

// Nearest 取 allowed 中距 v 最近的值（相等距离取较小者，因 allowed 升序先到先得）。
func (n *Norm) Nearest(field string, v int, allowed []int) int {
	if len(allowed) == 0 {
		return v
	}
	best := allowed[0]
	bestD := math.Abs(float64(v - allowed[0]))
	for _, a := range allowed[1:] {
		if d := math.Abs(float64(v - a)); d < bestD {
			best, bestD = a, d
		}
	}
	if best != v {
		n.note("%s adjusted from %d to nearest supported %d", field, v, best)
	}
	return best
}

// Err 返回 error 策略下积累的第一个错误（无则 nil）。
func (n *Norm) Err() error {
	if n.err != nil {
		return n.err
	}
	return nil
}
