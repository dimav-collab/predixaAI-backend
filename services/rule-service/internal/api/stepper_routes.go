package api

import "github.com/go-chi/chi/v5"

func (h *Handler) RegisterStepperRoutes(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		r.Route("/rules", func(r chi.Router) {
			// Static paths first — chi resolves these before dynamic /{ruleId}
			r.Get("/catalog", h.handleRuleCatalog)
			r.Post("/baseline/check", h.handleRuleBaselineCheck)
			r.Post("/preview", h.handleRulePreview)
			r.Post("/", h.handleStepperRuleCreate)
			r.Get("/", h.handleStepperRuleList)
			// Dynamic /{ruleId} — param name matches chi.URLParam(r, "ruleId") in stepper handlers
			r.Get("/{ruleId}", h.handleStepperRuleGetByID)
			r.Put("/{ruleId}", h.handleStepperRuleUpdate)
			r.Delete("/{ruleId}", h.handleStepperRuleDelete)
			r.Post("/{ruleId}/enable", h.handleStepperRuleEnable)
			r.Post("/{ruleId}/disable", h.handleStepperRuleDisable)
		})
		r.Get("/machine-units/{unitId}/parameters", h.handleUnitParameters)
		r.Get("/machine-units/{unitId}/rule-health", h.handleRuleHealth)
	})
}
