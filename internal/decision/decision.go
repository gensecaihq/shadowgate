package decision

import (
	"net/http"

	"shadowgate/internal/rules"
)

// Action represents the action to take for a request
type Action int

const (
	// AllowForward forwards the request to the backend
	AllowForward Action = iota
	// DenyDecoy serves decoy content
	DenyDecoy
	// Drop silently drops the connection
	Drop
	// Tarpit slows down the response
	Tarpit
	// Redirect sends a 3xx redirect
	Redirect
)

// String returns the string representation of an action
func (a Action) String() string {
	switch a {
	case AllowForward:
		return "allow_forward"
	case DenyDecoy:
		return "deny_decoy"
	case Drop:
		return "drop"
	case Tarpit:
		return "tarpit"
	case Redirect:
		return "redirect"
	default:
		return "unknown"
	}
}

// Decision represents the result of request evaluation
type Decision struct {
	Action      Action
	Reason      string
	Labels      []string
	RedirectURL string // for Redirect action
}

// Engine evaluates requests and returns decisions
type Engine struct {
	allowRules *rules.Group
	denyRules  *rules.Group
	evaluator  *rules.Evaluator
}

// NewEngine creates a new decision engine
func NewEngine(allowRules, denyRules *rules.Group) *Engine {
	return &Engine{
		allowRules: allowRules,
		denyRules:  denyRules,
		evaluator:  rules.NewEvaluator(),
	}
}

// Evaluate evaluates a request and returns a decision
func (e *Engine) Evaluate(req *http.Request, clientIP string) Decision {
	ctx := &rules.Context{
		Request:  req,
		ClientIP: clientIP,
	}

	// Extract TLS information if available
	if req.TLS != nil {
		ctx.TLSVersion = req.TLS.Version
		ctx.SNI = req.TLS.ServerName
	}

	// Check deny rules first (deny takes precedence)
	if e.denyRules != nil {
		result := e.evaluator.EvaluateGroup(e.denyRules, ctx)
		if result.Matched {
			return Decision{
				Action: DenyDecoy,
				Reason: result.Reason,
				Labels: result.Labels,
			}
		}
	}

	// Check allow rules
	if e.allowRules != nil {
		result := e.evaluator.EvaluateGroup(e.allowRules, ctx)
		if result.Matched {
			return Decision{
				Action: AllowForward,
				Reason: result.Reason,
				Labels: result.Labels,
			}
		}
		// Allow rules exist but didn't match - deny by default
		return Decision{
			Action: DenyDecoy,
			Reason: "no allow rules matched",
			Labels: []string{"default-deny"},
		}
	}

	// No rules configured - allow by default (permissive mode)
	return Decision{
		Action: AllowForward,
		Reason: "no rules configured",
		Labels: []string{"no-rules"},
	}
}
